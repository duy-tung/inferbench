#!/usr/bin/env bash
# IB-T004 calibration + measurement-correctness verification harness.
#
# Builds the gateway+mock pair READ-ONLY from the sibling infergate repo at
# its current HEAD (git archive; the commit is recorded in every manifest
# and in the summary), then runs against known configured latencies:
#
#   1. CALIBRATION (the core): two mock configs with known TTFT/ITL
#      (100ms/20ms and 300ms/5ms); streaming runs; measured client TTFT/ITL
#      must match configured within the declared tolerance:
#        ttft p50 - configured in [-2ms, +15ms]   ttft p95 <= configured + 50ms
#        itl  p50 - configured in [-2ms, +5ms]    itl  p95 <= configured + 15ms
#      (loopback + Go timer overshoot + scheduled-send basis overhead; see
#      docs/evidence/ib-t004/calibration.md).
#   2. CANCELLATION at the three points (queued / pre-first-token /
#      mid-stream), verified BOTH client-side (raw events) and target-side
#      (the mock's /debug/state abort observability).
#   3. SLOW-CLIENT bounded read rate: throttled runs vs a full-speed control.
#
# Usage: scripts/calibrate-mock.sh <out-dir> [infergate-repo-path] [pin]
# [pin] defaults to the infergate repo's current HEAD; pass a commit to hold
# the pin stable against a moving HEAD.
set -euo pipefail

OUT=${1:?out dir required}
INFERGATE=${2:-../infergate}
PIN_ARG=${3:-}

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)

MOCK_ADDR=127.0.0.1:18081
GW_ADDR=127.0.0.1:18080
GW_URL=http://$GW_ADDR
MOCK_URL=http://$MOCK_ADDR

PIN=${PIN_ARG:-$(git -C "$INFERGATE" rev-parse HEAD)}
echo "infergate pin: $PIN"
echo "$PIN" >"$OUT/infergate-pin.txt"

# --- build (read-only source consumption) -----------------------------------
SRC="$OUT/infergate-src"
rm -rf "$SRC" && mkdir -p "$SRC"
git -C "$INFERGATE" archive "$PIN" | tar -x -C "$SRC"
(cd "$SRC" && go build -o "$OUT/gateway" ./cmd/gateway && go build -o "$OUT/mock-backend" ./cmd/mock-backend)
go build -o "$OUT/inferbench" ./cmd/inferbench

MOCK_PID=""
GW_PID=""
stop_pair() {
  [ -n "$GW_PID" ] && kill "$GW_PID" 2>/dev/null && wait "$GW_PID" 2>/dev/null || true
  [ -n "$MOCK_PID" ] && kill "$MOCK_PID" 2>/dev/null && wait "$MOCK_PID" 2>/dev/null || true
  GW_PID=""; MOCK_PID=""
}
trap stop_pair EXIT

# start_pair <ttft> <itl> — fresh mock per scenario so /debug/state counters
# start clean.
start_pair() {
  stop_pair
  "$OUT/mock-backend" -addr "$MOCK_ADDR" -seed 42 -ttft "$1" -itl "$2" 2>>"$OUT/mock.log" &
  MOCK_PID=$!
  "$OUT/gateway" -addr "$GW_ADDR" -backend-url "$MOCK_URL" \
    -upstream-timeout 180s -stream-write-timeout 30s 2>>"$OUT/gateway.log" &
  GW_PID=$!
  for _ in $(seq 1 50); do
    curl -fsS "$GW_URL/healthz" >/dev/null 2>&1 && curl -fsS "$MOCK_URL/healthz" >/dev/null 2>&1 && return 0
    sleep 0.1
  done
  echo "pair did not become healthy"; exit 1
}

# facts <ttft> <itl> <hypothesis> — derive the manifest facts file. The
# infergate pin is injected from the commit actually built above, so a
# moving infergate HEAD can never desync the manifest from the binaries.
facts() {
  jq --arg ttft "$1" --arg itl "$2" --arg hyp "$3" --arg pin "$PIN" \
    '.engine.flags.ttft = $ttft | .engine.flags.itl = $itl | .hypothesis = $hyp
     | .engine.commit = $pin
     | .model.revision = "mockengine@" + ($pin[0:7])
     | .gateway.version = "dev@" + ($pin[0:7])' \
    testdata/live/ib-t004-pair.manifest.json
}

# workload <name> <jq-mutation> — derive from the base calibration workload.
BASE_WORKLOAD='{
  "name": "calib-base",
  "version": "1.0.0",
  "seed": 1004001,
  "description": "IB-T004 calibration base: moderate open-loop rate, bounded lengths; scenario scripts derive variants (name/seed kept, mutation recorded in the derived file).",
  "arrival_process": {"type": "open-loop-poisson", "rate_rps": 4},
  "input_length_distribution": {"type": "uniform", "min": 64, "max": 256},
  "output_length_distribution": {"type": "constant", "value": 128},
  "prefix_sharing": {"ratio": 0},
  "cancellation": {"rate": 0},
  "slow_client": {"fraction": 0},
  "stop": {"request_count": 120}
}'
workload() {
  echo "$BASE_WORKLOAD" | jq --arg name "$1" '.name = $name | '"$2"
}

# run_scenario <name> <workload-json> <facts-json> [extra flags...]
run_scenario() {
  local name=$1 wl=$2 fx=$3; shift 3
  mkdir -p "$OUT/$name"
  echo "$wl" >"$OUT/$name/workload.json"
  echo "$fx" >"$OUT/$name/facts.json"
  "$OUT/inferbench" run --workload "$OUT/$name/workload.json" \
    --manifest "$OUT/$name/facts.json" --target "$GW_URL" \
    --out "$OUT/$name" --run-id "ib-t004-$name" --stream "$@" \
    2>&1 | tee "$OUT/$name/cli.log"
  python3 scripts/eventstats.py "$OUT/$name/events.jsonl" >"$OUT/$name/stats.json"
  curl -fsS "$MOCK_URL/debug/state" >"$OUT/$name/debug-state.json"
}

# check <python-expr over s(cenario stats)/d(ebug state)> <scenario> <label>
check() {
  python3 - "$OUT/$2/stats.json" "$OUT/$2/debug-state.json" "$1" "$2: $3" <<'PY'
import json, sys
s = json.load(open(sys.argv[1]))
d = json.load(open(sys.argv[2]))
ok = eval(sys.argv[3], {"s": s, "d": d})
print(("PASS  " if ok else "FAIL  ") + sys.argv[4])
sys.exit(0 if ok else 1)
PY
}

FAILED=0
ck() { check "$@" || FAILED=1; }

# --- 1. calibration ----------------------------------------------------------
echo "=== calibration point A: ttft=100ms itl=20ms ==="
start_pair 100ms 20ms
run_scenario calib-A \
  "$(workload calib-a '.')" \
  "$(facts 100ms 20ms 'Measured client TTFT p50 lands within [configured-2ms, configured+15ms] and pooled client ITL p50 within [configured-2ms, configured+5ms] of the mock-configured 100ms/20ms, with p95 within +50ms/+15ms (tolerance statement in calibration.md).')"
ck 's["status_counts"].get("ok",0)==120' calib-A "all 120 streams ok"
ck '0.098 <= s["client_ttft_seconds"]["p50"] <= 0.115' calib-A "ttft p50 ~ 100ms (+15ms tol)"
ck 's["client_ttft_seconds"]["p95"] <= 0.150' calib-A "ttft p95 <= 150ms"
ck '0.018 <= s["client_itl_seconds_pooled"]["p50"] <= 0.025' calib-A "itl p50 ~ 20ms (+5ms tol)"
ck 's["client_itl_seconds_pooled"]["p95"] <= 0.035' calib-A "itl p95 <= 35ms"

echo "=== calibration point B: ttft=300ms itl=5ms ==="
start_pair 300ms 5ms
run_scenario calib-B \
  "$(workload calib-b '.')" \
  "$(facts 300ms 5ms 'Measured client TTFT/ITL track a changed mock configuration (300ms/5ms) within the same declared tolerances, showing the measurement follows the configured latency rather than a coincidence of one config.')"
ck 's["status_counts"].get("ok",0)==120' calib-B "all 120 streams ok"
ck '0.298 <= s["client_ttft_seconds"]["p50"] <= 0.315' calib-B "ttft p50 ~ 300ms (+15ms tol)"
ck 's["client_ttft_seconds"]["p95"] <= 0.350' calib-B "ttft p95 <= 350ms"
ck '0.003 <= s["client_itl_seconds_pooled"]["p50"] <= 0.010' calib-B "itl p50 ~ 5ms (+5ms tol)"
ck 's["client_itl_seconds_pooled"]["p95"] <= 0.020' calib-B "itl p95 <= 20ms"

# --- 2. cancellation: three points ------------------------------------------
# Mock at ttft=300ms itl=30ms so the points are unambiguous relative to the
# stream timeline. rate 1.0: every request carries the profiled cancel.
CANCEL_STOP='.stop = {"request_count": 30} | .output_length_distribution = {"type": "constant", "value": 256}'

echo "=== cancellation point 1: queued (cancel at 0s, before the send) ==="
start_pair 300ms 30ms
run_scenario cancel-queued \
  "$(workload cancel-queued "$CANCEL_STOP"' | .cancellation = {"rate": 1.0, "point": {"trigger": "elapsed-seconds", "distribution": {"type": "constant", "value": 0}}}')" \
  "$(facts 300ms 30ms 'A cancel profiled at 0s after the scheduled send is issued at dispatch: the client records status=canceled with 0 tokens and no TTFT; requests that never completed their send emit send_slip_seconds ABSENT.')"
ck 's["status_counts"].get("canceled",0)==30' cancel-queued "all 30 canceled"
ck 's["client_ttft_seconds"] is None' cancel-queued "no body byte -> no ttft"
ck 's["cancellation_tokens_at_cancel"]["max"]==0' cancel-queued "0 tokens at cancel"
ck 's["cancellation_elapsed_seconds"]["p50"] <= 0.05' cancel-queued "cancel issued ~immediately"

echo "=== cancellation point 2: pre-first-token (cancel at 150ms < ttft 300ms) ==="
start_pair 300ms 30ms
run_scenario cancel-pre-first-token \
  "$(workload cancel-pre-first-token "$CANCEL_STOP"' | .cancellation = {"rate": 1.0, "point": {"trigger": "elapsed-seconds", "distribution": {"type": "constant", "value": 0.15}}}')" \
  "$(facts 300ms 30ms 'A cancel profiled at 150ms (mock TTFT 300ms) aborts every stream pre-first-token: client events carry status=canceled with 0 tokens and no TTFT; the mock observes the aborts with phase=pre_first_token and chunks_sent=0 (/debug/state).')"
ck 's["status_counts"].get("canceled",0)==30' cancel-pre-first-token "all 30 canceled"
ck 's["client_ttft_seconds"] is None' cancel-pre-first-token "no first token before cancel"
ck 's["cancellation_tokens_at_cancel"]["max"]==0' cancel-pre-first-token "0 tokens at cancel"
ck '0.10 <= s["cancellation_elapsed_seconds"]["p50"] <= 0.20' cancel-pre-first-token "cancel at ~150ms"
ck 'd["aborts_total"]==30 and all(a["phase"]=="pre_first_token" and a["chunks_sent"]==0 for a in d["aborts"])' \
  cancel-pre-first-token "mock observed 30 pre_first_token aborts"

echo "=== cancellation point 3: mid-stream (cancel after 8 output tokens) ==="
start_pair 300ms 30ms
run_scenario cancel-mid-stream \
  "$(workload cancel-mid-stream "$CANCEL_STOP"' | .cancellation = {"rate": 1.0, "point": {"trigger": "output-tokens", "distribution": {"type": "constant", "value": 8}}}')" \
  "$(facts 300ms 30ms 'A cancel profiled at 8 output tokens aborts streams mid-generation: client events carry status=canceled with exactly 8 tokens at cancel, a measured TTFT and an ITL series; the mock observes the aborts with phase=mid_stream and chunks_sent>=9 (/debug/state). Streams the mock deterministically ends before 8 tokens complete ok (honest realized-cancel accounting).')"
ck 's["status_counts"].get("canceled",0) >= 25' cancel-mid-stream ">=25/30 canceled (short streams complete ok)"
ck 's["cancellation_tokens_at_cancel"]["p50"]==8 and s["cancellation_tokens_at_cancel"]["max"]==8' \
  cancel-mid-stream "exactly 8 tokens at cancel"
ck 's["client_ttft_seconds"] is not None and 0.298 <= s["client_ttft_seconds"]["p50"] <= 0.32' \
  cancel-mid-stream "canceled streams keep measured ttft (~300ms)"
ck 's["client_itl_seconds_pooled"] is not None and 0.028 <= s["client_itl_seconds_pooled"]["p50"] <= 0.04' \
  cancel-mid-stream "canceled streams keep ITL series (~30ms)"
ck 'd["aborts_total"]>=25 and all(a["phase"]=="mid_stream" and a["chunks_sent"]>=9 for a in d["aborts"])' \
  cancel-mid-stream "mock observed mid_stream aborts with >=9 chunks written"

# --- 3. slow-client bounded read rate ---------------------------------------
SLOW_STOP='.stop = {"request_count": 30} | .arrival_process.rate_rps = 3 | .output_length_distribution = {"type": "constant", "value": 64}'

echo "=== slow-client control: full-speed readers ==="
start_pair 20ms 5ms
run_scenario slow-control \
  "$(workload slow-control "$SLOW_STOP")" \
  "$(facts 20ms 5ms 'Control for the slow-client comparison: identical traffic with full-speed readers establishes the unthrottled e2e baseline.')"
ck 's["status_counts"].get("ok",0)==30' slow-control "all 30 ok"

echo "=== slow-client: every reader throttled to 4096 B/s (+200ms first-read delay) ==="
start_pair 20ms 5ms
run_scenario slow-on \
  "$(workload slow-on "$SLOW_STOP"' | .slow_client = {"fraction": 1.0, "read_bytes_per_second": 4096, "initial_read_delay_seconds": 0.2}')" \
  "$(facts 20ms 5ms 'With every reader throttled to 4096 B/s and a 200ms first-read delay, client-side e2e p50 is dominated by the bounded read rate (>=2x the full-speed control) and TTFT includes the self-imposed initial delay - the client-side mirror series honestly reflects the slow client.')" \
  --request-timeout 120s
ck 's["status_counts"].get("ok",0)==30' slow-on "all 30 ok"

python3 - "$OUT/slow-control/stats.json" "$OUT/slow-on/stats.json" <<'PY' || FAILED=1
import json, sys
ctrl = json.load(open(sys.argv[1]))
slow = json.load(open(sys.argv[2]))
c50 = ctrl["client_e2e_seconds"]["p50"]; s50 = slow["client_e2e_seconds"]["p50"]
sttft = slow["client_ttft_seconds"]["p50"]
ok = s50 >= 2*c50 and s50 >= 0.5 and sttft >= 0.2
print(("PASS  " if ok else "FAIL  ") +
      f"slow-on: e2e p50 {s50:.3f}s >= 2x control {c50:.3f}s and ttft p50 {sttft:.3f}s includes the 0.2s read delay")
sys.exit(0 if ok else 1)
PY

stop_pair
echo
if [ "$FAILED" -ne 0 ]; then
  echo "CALIBRATION FAILED (see FAIL lines above); artifacts in $OUT"
  exit 1
fi
echo "CALIBRATION PASSED; artifacts in $OUT (infergate pin $PIN)"
