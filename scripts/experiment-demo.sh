#!/usr/bin/env bash
# IB-T009 governance-demo evidence: the `experiment` subcommand refuses
# hypothesis-less invocations and combinatorial/multi-variable arm sets
# (both BEFORE any request is sent), and a compliant hypothesis-gated
# comparison runs end to end against the pinned mock pair.
#
# Usage: scripts/experiment-demo.sh <out-dir> [infergate-repo-path] [pin]
set -euo pipefail

OUT=${1:?out dir required}
INFERGATE=${2:-../infergate}
PIN_ARG=${3:-}

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)

MOCK_ADDR=127.0.0.1:18381
GW_ADDR=127.0.0.1:18380
GW_URL=http://$GW_ADDR
MOCK_URL=http://$MOCK_ADDR

PIN=${PIN_ARG:-$(git -C "$INFERGATE" rev-parse HEAD)}
echo "infergate pin: $PIN"
echo "$PIN" >"$OUT/infergate-pin.txt"

SRC="$OUT/infergate-src"
rm -rf "$SRC" && mkdir -p "$SRC"
git -C "$INFERGATE" archive "$PIN" | tar -x -C "$SRC"
(cd "$SRC" && go build -o "$OUT/gateway-bin" ./cmd/gateway && go build -o "$OUT/mock-backend-bin" ./cmd/mock-backend)
go build -o "$OUT/inferbench-bin" ./cmd/inferbench
GATEWAY="$OUT/gateway-bin"
MOCKBIN="$OUT/mock-backend-bin"
IB="$OUT/inferbench-bin"

MOCK_PID=""
GW_PID=""
stop_pair() {
  [ -n "$GW_PID" ] && kill "$GW_PID" 2>/dev/null && wait "$GW_PID" 2>/dev/null || true
  [ -n "$MOCK_PID" ] && kill "$MOCK_PID" 2>/dev/null && wait "$MOCK_PID" 2>/dev/null || true
  GW_PID=""; MOCK_PID=""
}
trap stop_pair EXIT
start_pair() {
  stop_pair
  "$MOCKBIN" -addr "$MOCK_ADDR" -seed 42 -ttft 20ms -itl 5ms 2>>"$OUT/mock.log" &
  MOCK_PID=$!
  "$GATEWAY" -addr "$GW_ADDR" -backend-url "$MOCK_URL" \
    -upstream-timeout 60s -stream-write-timeout 30s 2>>"$OUT/gateway.log" &
  GW_PID=$!
  for _ in $(seq 1 50); do
    curl -fsS "$GW_URL/healthz" >/dev/null 2>&1 && curl -fsS "$MOCK_URL/healthz" >/dev/null 2>&1 && return 0
    sleep 0.1
  done
  echo "pair did not become healthy"; exit 1
}

FAILED=0
TRANSCRIPT="$OUT/refusal-demo-transcript.md"
: >"$TRANSCRIPT"
note() { echo "$@" | tee -a "$TRANSCRIPT"; }
section() { note ""; note "## $*"; note '```text'; }
endsection() { note '```'; }

note "# IB-T009 refusal-demo transcript"
note ""
note "Generated $(date -u +%Y-%m-%dT%H:%M:%SZ) against inferbench built from this checkout,"
note "infergate pin \`$PIN\` (mock/gateway, built read-only via \`git archive\`)."

cat >"$OUT/facts-gateway.json" <<EOF
{
  "run_id": "",
  "target_topology": "gateway-mock",
  "workload_ref": {"name": "", "version": "", "seed": 0},
  "engine": {"name": "mock", "version": "dev", "commit": "$PIN", "flags": {"ttft": "20ms", "itl": "5ms", "seed": 42, "error_rate": 0}},
  "model": {"checkpoint": "mock-8b", "revision": "mockengine@${PIN:0:7}", "tokenizer": "mockengine-estimator"},
  "hardware": {"gpu_model": null, "gpu_count": 0, "vram_gb": null, "driver_version": null, "cuda_version": null, "instance_type": "local-dev-container (linux/amd64, CPU-only)"},
  "gateway": {"version": "dev@${PIN:0:7}", "config_version": "flags-v1"},
  "client": {"location": "same-host (loopback)", "rtt_ms": null},
  "warm_up": {"policy": "none"},
  "repetitions": 1,
  "hypothesis": "Routing chat-short traffic through infergate (via-gateway) instead of directly at the engine (engine-direct) increases end-to-end latency by a small, bounded amount rather than an unbounded or wildly variable one."
}
EOF
python3 - "$OUT/facts-gateway.json" "$OUT/facts-direct.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["target_topology"] = "engine-direct"
d["gateway"] = None
# The manifest's hypothesis field (methodology rule 6) restates the SAME
# experiment-level falsifiable statement on every arm -- it is not a
# per-arm description, so it must NOT differ (manifest.Diff correctly
# flags it as a second variable if the two arms' text diverges, which is
# exactly the single-variable guard working as intended, caught while
# authoring this script's fixture data).
json.dump(d, open(sys.argv[2], "w"), indent=2)
PY

cat >"$OUT/demo-workload.json" <<'EOF'
{
  "name": "ib-t009-experiment-demo",
  "version": "1.0.0",
  "seed": 9009001,
  "arrival_process": {"type": "open-loop-poisson", "rate_rps": 10},
  "input_length_distribution": {"type": "uniform", "min": 32, "max": 128},
  "output_length_distribution": {"type": "uniform", "min": 8, "max": 64},
  "prefix_sharing": {"ratio": 0},
  "cancellation": {"rate": 0},
  "slow_client": {"fraction": 0},
  "stop": {"request_count": 40}
}
EOF

GOOD_HYP="hypotheses/EXP-ib-t009-gateway-overhead-demo.json"

run_and_capture() { # run_and_capture <label> <expect-substring> <cmd...>
  local label="$1" expect="$2"; shift 2
  section "$label"
  set +e
  out=$("$@" 2>&1)
  code=$?
  set -e
  echo "\$ $*" >>"$TRANSCRIPT"
  echo "$out" >>"$TRANSCRIPT"
  echo "(exit $code)" >>"$TRANSCRIPT"
  endsection
  if [ "$code" -eq 0 ]; then
    echo "!! FAIL: $label expected a refusal (nonzero exit), got 0"
    FAILED=1
    return
  fi
  if ! echo "$out" | grep -qF "$expect"; then
    echo "!! FAIL: $label refusal did not carry the expected typed reason ($expect)"
    FAILED=1
    return
  fi
  echo "PASS: $label refused as expected (exit $code): $expect"
}

echo "=== demo 1: hypothesis-less refusals (no --hypothesis at all) ==="
run_and_capture "experiment run, no --hypothesis" "experiment: --hypothesis is required" \
  "$IB" experiment run --workload "$OUT/demo-workload.json" --manifest "$OUT/facts-gateway.json" \
  --target "$GW_URL" --out "$OUT/refused-run-nohyp"
run_and_capture "experiment sweep, no --hypothesis" "experiment: --hypothesis is required" \
  "$IB" experiment sweep --workload "$OUT/demo-workload.json" --manifest "$OUT/facts-gateway.json" \
  --target "$GW_URL" --out "$OUT/refused-sweep-nohyp"
run_and_capture "experiment compare, no --hypothesis" "experiment: --hypothesis is required" \
  "$IB" experiment compare --workload "$OUT/demo-workload.json" --variable target_topology \
  --arm direct="$OUT/facts-direct.json@$MOCK_URL" --arm gateway="$OUT/facts-gateway.json@$GW_URL" \
  --out "$OUT/refused-compare-nohyp"

echo
echo "=== demo 2: incomplete hypothesis file refusal ==="
python3 - "$GOOD_HYP" "$OUT/hyp-incomplete.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
del d["stop_condition"]
json.dump(d, open(sys.argv[2], "w"), indent=2)
PY
run_and_capture "experiment run, incomplete hypothesis (missing stop_condition)" "experiment: incomplete hypothesis file" \
  "$IB" experiment run --hypothesis "$OUT/hyp-incomplete.json" \
  --workload "$OUT/demo-workload.json" --manifest "$OUT/facts-gateway.json" \
  --target "$GW_URL" --out "$OUT/refused-run-incomplete"

echo
echo "=== demo 3: combinatorial arm set refused (variable mismatch already caught structurally) ==="
python3 - "$OUT/facts-gateway.json" "$OUT/facts-gateway-diffmodel.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["model"]["checkpoint"] = "different-model-8b"
json.dump(d, open(sys.argv[2], "w"), indent=2)
PY
run_and_capture "experiment compare, arms differ in 2 fields (target_topology declared, model.checkpoint also differs)" \
  "arms vary in more than the single declared variable" \
  "$IB" experiment compare --hypothesis "$GOOD_HYP" \
  --workload "$OUT/demo-workload.json" --variable target_topology \
  --arm direct="$OUT/facts-direct.json@$MOCK_URL" --arm gateway="$OUT/facts-gateway-diffmodel.json@$GW_URL" \
  --out "$OUT/refused-compare-combinatorial"

echo
echo "=== demo 4: --variable flag disagrees with the hypothesis's declared variable ==="
run_and_capture "experiment compare, --variable != hypothesis.variable" \
  "hypothesis declares variable" \
  "$IB" experiment compare --hypothesis "$GOOD_HYP" \
  --workload "$OUT/demo-workload.json" --variable "engine.flags.max_num_seqs" \
  --arm direct="$OUT/facts-direct.json@$MOCK_URL" --arm gateway="$OUT/facts-gateway.json@$GW_URL" \
  --out "$OUT/refused-compare-variable-mismatch"

echo
echo "=== demo 5: GPU session block required when hardware declares a GPU (G6), no live GPU needed ==="
python3 - "$OUT/facts-gateway.json" "$OUT/facts-gpu-no-session.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["hardware"]["gpu_model"] = "H100-80GB"
d["hardware"]["gpu_count"] = 1
d["hardware"]["vram_gb"] = 80
d["hardware"]["driver_version"] = "550.90"
d["hardware"]["cuda_version"] = "12.4"
d["hardware"]["instance_type"] = "gpu-demo-instance (not actually rented -- structural refusal only)"
json.dump(d, open(sys.argv[2], "w"), indent=2)
PY
run_and_capture "experiment run against a GPU-declaring manifest, hypothesis has no gpu_session block" \
  "gpu_session block" \
  "$IB" experiment run --hypothesis "$GOOD_HYP" \
  --workload "$OUT/demo-workload.json" --manifest "$OUT/facts-gpu-no-session.json" \
  --target "$GW_URL" --out "$OUT/refused-run-gpu-no-session"

echo
echo "=== demo 6: compliant hypothesis-gated experiment runs END TO END vs the mock ==="
start_pair
section "compliant experiment compare (hypothesis + single-variable arms, executes)"
set +e
out=$("$IB" experiment compare --hypothesis "$GOOD_HYP" \
  --workload "$OUT/demo-workload.json" --variable target_topology \
  --arm direct="$OUT/facts-direct.json@$MOCK_URL" --arm gateway="$OUT/facts-gateway.json@$GW_URL" \
  --out "$OUT/compliant-compare" --stream --repetitions 1 2>&1)
code=$?
set -e
echo "\$ $IB experiment compare --hypothesis $GOOD_HYP ..." >>"$TRANSCRIPT"
echo "$out" >>"$TRANSCRIPT"
echo "(exit $code)" >>"$TRANSCRIPT"
endsection
echo "$out"
if [ "$code" -ne 0 ]; then
  echo "!! FAIL: compliant experiment compare should have succeeded"
  FAILED=1
elif [ ! -f "$OUT/compliant-compare/comparison.json" ]; then
  echo "!! FAIL: compliant experiment compare produced no comparison.json"
  FAILED=1
else
  echo "PASS: compliant experiment compare ran end to end vs the mock pair"
fi
stop_pair

echo
echo "=== kit validation of the compliant run's emitted artifacts ==="
CONTRACTS_BUNDLE=${CONTRACTS_BUNDLE:-}
if [ -n "$CONTRACTS_BUNDLE" ] && [ -d "$OUT/compliant-compare" ]; then
  : >"$OUT/kit-validate.log"
  set +e
  kit_exit=0
  mapfile -t manifests < <(find "$OUT/compliant-compare" -name 'manifest.json')
  [ "${#manifests[@]}" -gt 0 ] && python3 "$CONTRACTS_BUNDLE/kit/contracts-validate.py" --bundle "$CONTRACTS_BUNDLE" \
    validate --schema benchmark-run "${manifests[@]}" >>"$OUT/kit-validate.log" 2>&1 || kit_exit=1
  mapfile -t events < <(find "$OUT/compliant-compare" -name 'events.jsonl')
  [ "${#events[@]}" -gt 0 ] && python3 "$CONTRACTS_BUNDLE/kit/contracts-validate.py" --bundle "$CONTRACTS_BUNDLE" \
    validate --schema raw-event "${events[@]}" >>"$OUT/kit-validate.log" 2>&1 || kit_exit=1
  python3 "$CONTRACTS_BUNDLE/kit/contracts-validate.py" --bundle "$CONTRACTS_BUNDLE" \
    validate --schema workload "$OUT/demo-workload.json" >>"$OUT/kit-validate.log" 2>&1 || kit_exit=1
  set -e
  tail -n 10 "$OUT/kit-validate.log"
  [ "$kit_exit" -eq 0 ] || FAILED=1
else
  echo "CONTRACTS_BUNDLE not set or compliant run missing; skipping kit validation"
fi

# clean up build byproducts (kept out of git; only the evidence artifacts are committed)
rm -rf "$SRC" "$GATEWAY" "$MOCKBIN" "$IB"

echo
if [ "$FAILED" -ne 0 ]; then
  echo "IB-T009 VERIFICATION FAILED (see FAIL lines above); artifacts in $OUT"
  exit 1
fi
echo "IB-T009 VERIFICATION PASSED; artifacts in $OUT (infergate pin $PIN)"
echo "transcript: $TRANSCRIPT"
