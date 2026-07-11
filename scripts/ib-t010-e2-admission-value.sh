#!/usr/bin/env bash
# IB-T010 E2: admission value at ~5x estimated capacity (G5 evidence).
# mock backend, two gateway processes differing ONLY in admission
# configuration (gateway.config_version: admission-sane-v1 vs
# admission-off-v1). Capacity is estimated once via `inferbench sweep`'s
# open-loop overload probe against the admission-sane gateway (ADR-0003:
# sanctioned for throughput-ceiling discovery). Reuses the gateway/mock/
# inferbench binaries already built by the E1 mock sub-experiment.
#
# Usage: scripts/ib-t010-e2-admission-value.sh
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
OUT=docs/evidence/ib-t010

MOCK_ADDR=127.0.0.1:19291
GW_SANE_ADDR=127.0.0.1:19292
GW_OFF_ADDR=127.0.0.1:19293
MOCK_URL=http://$MOCK_ADDR
GW_SANE_URL=http://$GW_SANE_ADDR
GW_OFF_URL=http://$GW_OFF_ADDR

echo "=== quiet-box check ==="
ps aux | grep -E 'llama-server|gateway-bin|mock-backend-bin' | grep -v grep || echo "no matching processes"
uptime

MOCK_PID=""; GWS_PID=""; GWO_PID=""
stop_all() {
  [ -n "$GWS_PID" ] && kill "$GWS_PID" 2>/dev/null && wait "$GWS_PID" 2>/dev/null || true
  [ -n "$GWO_PID" ] && kill "$GWO_PID" 2>/dev/null && wait "$GWO_PID" 2>/dev/null || true
  [ -n "$MOCK_PID" ] && kill "$MOCK_PID" 2>/dev/null && wait "$MOCK_PID" 2>/dev/null || true
  MOCK_PID=""; GWS_PID=""; GWO_PID=""
}
trap stop_all EXIT

"$OUT/mock-backend-bin" -addr "$MOCK_ADDR" -seed 43 -ttft 80ms -itl 10ms >"$OUT/e2-mock.log" 2>&1 &
MOCK_PID=$!

# admission-sane-v1: latency-protective admission (declared BEFORE running,
# per the hypothesis file): in-flight budget 6 (the modeled service
# capacity), queue depth deliberately SHALLOW (cap 3 -- a fraction of the
# budget, i.e. max queue transit ~ queue_cap/capacity, a small multiple of
# one service time) plus a short 500ms deadline backstop. Shedding early
# instead of queueing deep is the entire G5 design goal: accepted-request
# latency is protected BECAUSE excess demand is refused typed, not parked.
"$OUT/gateway-bin" -addr "$GW_SANE_ADDR" -backend-url "$MOCK_URL" \
  -upstream-timeout 30s -stream-write-timeout 30s \
  -admission-tenant-queue-cap 3 -admission-global-inflight-budget 6 \
  -admission-global-queue-cap 3 -admission-queue-deadline 500ms \
  >"$OUT/e2-gateway-sane.log" 2>&1 &
GWS_PID=$!

# admission-off-v1: huge budget/queues/deadline -- effectively no admission
# protection (single varying variable vs admission-sane-v1: gateway.config_version).
"$OUT/gateway-bin" -addr "$GW_OFF_ADDR" -backend-url "$MOCK_URL" \
  -upstream-timeout 30s -stream-write-timeout 30s \
  -admission-tenant-queue-cap 1000000 -admission-global-inflight-budget 1000000 \
  -admission-global-queue-cap 1000000 -admission-queue-deadline 300s \
  >"$OUT/e2-gateway-off.log" 2>&1 &
GWO_PID=$!

for i in $(seq 1 50); do
  curl -fsS "$MOCK_URL/healthz" >/dev/null 2>&1 && curl -fsS "$GW_SANE_URL/healthz" >/dev/null 2>&1 && curl -fsS "$GW_OFF_URL/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS "$GW_SANE_URL/healthz" >/dev/null 2>&1 || { echo "sane gateway did not become healthy"; exit 1; }
curl -fsS "$GW_OFF_URL/healthz" >/dev/null 2>&1 || { echo "off gateway did not become healthy"; exit 1; }
echo "mock + both gateways healthy"

# --- 1. capacity probe against admission-sane-v1 (ADR-0003 sanctioned overload probe) ---
# `sweep` is used PROBE-ONLY here: it writes sweep.json (with the probe block
# and capacity_estimate_rps) before refusing the <6-point run, and E2 needs
# exactly two hand-placed load points (1x baseline, 5x overload), not a
# 6-point sweep -- so the typed points refusal after the probe is expected
# and tolerated, and we assert the probe result exists instead.
echo "=== capacity probe (admission-sane-v1) ==="
"$OUT/inferbench-bin" sweep \
  --workload "$OUT/e2-probe-base-workload.json" --manifest "$OUT/e2-facts-sane.json" \
  --target "$GW_SANE_URL" --out "$OUT/e2-probe" \
  --max-conns 1000 --repetitions 1 --points 1 --min-fraction 1.0 --max-fraction 1.0 \
  --probe-rate 200 --probe-requests 400 --probe-max-slip 60s \
  --max-slip 60s --request-timeout 15s --stream \
  2>&1 | tee "$OUT/e2-probe.log" || true
[ -f "$OUT/e2-probe/sweep.json" ] || { echo "probe did not write sweep.json"; exit 1; }

CAP=$(python3 -c "import json; print(json.load(open('$OUT/e2-probe/sweep.json'))['capacity_estimate_rps'])")
echo "capacity_estimate_rps=$CAP"
echo "$CAP" > "$OUT/e2-capacity-estimate-rps.txt"

RATE_5X=$(python3 -c "print(round($CAP*5, 4))")
RATE_1X=$(python3 -c "print(round($CAP*1.0, 4))")
echo "RATE_5X=$RATE_5X RATE_1X=$RATE_1X"

# --- 2. derive the 1x baseline and 5x overload workloads from the probe base ---
python3 - "$OUT/e2-probe-base-workload.json" "$OUT/e2-baseline-workload.json" "$RATE_1X" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["name"] = "ib-t010-e2-baseline"
d["seed"] = 10010202
d["description"] = "IB-T010 E2 capacity-boundary (~1x estimated capacity) baseline, admission-sane-v1 only. Reference point for the G5 TTFT-degradation-at-5x criterion."
d["arrival_process"]["rate_rps"] = float(sys.argv[3])
d["stop"] = {"request_count": 350}
json.dump(d, open(sys.argv[2], "w"), indent=2)
PY
python3 - "$OUT/e2-probe-base-workload.json" "$OUT/e2-overload-workload.json" "$RATE_5X" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["name"] = "ib-t010-e2-overload"
d["seed"] = 10010201
d["description"] = "IB-T010 E2 ~5x estimated-capacity overload point, admission-sane-v1 vs admission-off-v1 (single declared variable gateway.config_version)."
d["arrival_process"]["rate_rps"] = float(sys.argv[3])
d["stop"] = {"request_count": 900}
json.dump(d, open(sys.argv[2], "w"), indent=2)
PY

# --- 3. baseline (~1x capacity, admission-sane-v1 only), 3 repetitions ---
echo "=== baseline @ ~1x capacity (admission-sane-v1) ==="
for rep in 1 2 3; do
  "$OUT/inferbench-bin" experiment run \
    --hypothesis hypotheses/EXP-ib-t010-e2-admission-value.json \
    --workload "$OUT/e2-baseline-workload.json" --manifest "$OUT/e2-facts-sane.json" \
    --target "$GW_SANE_URL" --out "$OUT/e2-baseline/rep-$rep" \
    --run-id "ib-t010-e2-baseline-r$rep" --repetition "$rep" --stream \
    2>&1 | tee -a "$OUT/e2-baseline.log"
done

# --- 4. spot-check: direct curl at raw HTTP level showing a real 503 + Retry-After ---
echo "=== spot-check: raw HTTP shed response (admission-sane-v1, burst beyond budget) ==="
{
  echo "burst of 20 concurrent requests against admission-sane-v1 (budget=6, tenant-queue-cap=20); expect some 200s and some 503s with Retry-After"
  for i in $(seq 1 20); do
    curl -sS -D - -o /dev/null --max-time 10 "$GW_SANE_URL/v1/chat/completions" \
      -H 'Content-Type: application/json' \
      -d '{"model":"mock-8b","messages":[{"role":"user","content":"spot check"}],"max_tokens":8,"stream":false}' \
      2>&1 | grep -iE '^HTTP|^Retry-After' &
  done
  wait
} | tee "$OUT/e2-shed-spotcheck.log"

# --- 5. governed 5x comparison: admission-sane-v1 vs admission-off-v1 ---
echo "=== 5x overload: admission-sane-v1 vs admission-off-v1 ==="
"$OUT/inferbench-bin" experiment compare \
  --hypothesis hypotheses/EXP-ib-t010-e2-admission-value.json \
  --workload "$OUT/e2-overload-workload.json" --variable gateway.config_version \
  --arm sane="$OUT/e2-facts-sane.json@$GW_SANE_URL" \
  --arm off="$OUT/e2-facts-off.json@$GW_OFF_URL" \
  --out "$OUT/e2-overload-compare" --stream --repetitions 3 --max-slip 60s --request-timeout 15s \
  2>&1 | tee "$OUT/e2-overload-compare.log"

# --- 6. gateway-side evidence scraped BEFORE teardown: shed reasons + TTFT/queue-wait histograms ---
for pair in "sane $GW_SANE_URL" "off $GW_OFF_URL"; do
  set -- $pair
  name=$1; url=$2
  python3 "$OUT/scrape_ttft_hist.py" "$url/metrics" --diff "$OUT/empty-before.json" \
    | tee "$OUT/e2-gateway-side-percentiles-$name.json"
  curl -sS "$url/metrics" | grep -E "^inference_sheds_total|^inference_requests_total" \
    | tee "$OUT/e2-gateway-metrics-$name.txt"
done

# --- 7. spot-check the admission-off arm too: same burst, expect NO 503s ---
{
  echo "burst of 20 concurrent requests against admission-off-v1 (all caps ~1e6); expect all 200s, no Retry-After"
  for i in $(seq 1 20); do
    curl -sS -D - -o /dev/null --max-time 10 "$GW_OFF_URL/v1/chat/completions" \
      -H 'Content-Type: application/json' \
      -d '{"model":"mock-8b","messages":[{"role":"user","content":"spot check"}],"max_tokens":8,"stream":false}' \
      2>&1 | grep -iE '^HTTP|^Retry-After' &
  done
  wait
} | tee "$OUT/e2-shed-spotcheck-off.log"

echo "E2 admission-value experiment complete: $OUT/e2-overload-compare (baseline: $OUT/e2-baseline)"
