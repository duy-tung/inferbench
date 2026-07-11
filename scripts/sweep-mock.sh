#!/usr/bin/env bash
# IB-T008 end-to-end evidence: sweep, replay, and A/B comparison mechanics
# verified against the pinned gateway+mock pair (infergate, built read-only
# via `git archive` — see docs/implementation-notes.md for the pin).
#
# Mock-verification technique (documented, not a production claim): the
# mock backend at the pinned build has NO admission control or concurrency
# limiter of its own (verified: cmd/mock-backend/main.go's flag set is
# addr/seed/ttft/itl/error-rate/created/stream-fail-after-chunks only), so
# a plain rate sweep against it would never saturate — every request is
# served in constant configured time regardless of concurrent load. To
# demonstrate the sweep mechanics AND the knee detector on a real HTTP
# round trip (not a synthetic-data unit test), this script holds a
# client-side connection-pool cap fixed across the probe and every point
# (`inferbench sweep --max-conns`, see internal/sweep package doc): Go's
# http.Transport blocks new requests once the cap is reached rather than
# failing them, producing genuine queueing delay that counts against
# latency via the scheduled-send basis (ADR-0001's "at-saturation
# caveat" — ADR text: "Raising --max-slip for overload studies is
# legitimate only because the slip is recorded per event either way").
# This is a verification harness parameter, never used for a published
# latency/goodput claim.
#
# Usage: scripts/sweep-mock.sh <out-dir> [infergate-repo-path] [pin]
set -euo pipefail

OUT=${1:?out dir required}
INFERGATE=${2:-../infergate}
PIN_ARG=${3:-}

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)

MOCK_ADDR=127.0.0.1:18281
GW_ADDR=127.0.0.1:18280
GW_URL=http://$GW_ADDR
MOCK_URL=http://$MOCK_ADDR

PIN=${PIN_ARG:-$(git -C "$INFERGATE" rev-parse HEAD)}
echo "infergate pin: $PIN"
echo "$PIN" >"$OUT/infergate-pin.txt"

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

start_pair() {
  stop_pair
  "$OUT/mock-backend" -addr "$MOCK_ADDR" -seed 42 -ttft 20ms -itl 5ms 2>>"$OUT/mock.log" &
  MOCK_PID=$!
  "$OUT/gateway" -addr "$GW_ADDR" -backend-url "$MOCK_URL" \
    -upstream-timeout 60s -stream-write-timeout 30s 2>>"$OUT/gateway.log" &
  GW_PID=$!
  for _ in $(seq 1 50); do
    curl -fsS "$GW_URL/healthz" >/dev/null 2>&1 && curl -fsS "$MOCK_URL/healthz" >/dev/null 2>&1 && return 0
    sleep 0.1
  done
  echo "pair did not become healthy"; exit 1
}

FAILED=0

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
  "repetitions": 3,
  "hypothesis": "IB-T008 sweep mechanics verification: a rate sweep at 10%-120% of an estimated client-modeled capacity produces a detectable saturation knee on the pinned mock pair."
}
EOF
python3 - "$OUT/facts-gateway.json" "$OUT/facts-direct.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["target_topology"] = "engine-direct"
d["gateway"] = None
json.dump(d, open(sys.argv[2], "w"), indent=2)
PY

cat >"$OUT/sweep-base.json" <<'EOF'
{
  "name": "ib-t008-sweep-base",
  "version": "1.0.0",
  "seed": 8008021,
  "description": "IB-T008 sweep mechanics verification base workload: moderate lengths, bounded, single-rate open-loop-poisson (rate overridden per sweep point).",
  "arrival_process": {"type": "open-loop-poisson", "rate_rps": 5},
  "input_length_distribution": {"type": "uniform", "min": 32, "max": 64},
  "output_length_distribution": {"type": "uniform", "min": 8, "max": 32},
  "prefix_sharing": {"ratio": 0},
  "cancellation": {"rate": 0},
  "slow_client": {"fraction": 0},
  "stop": {"request_count": 150}
}
EOF

# --- 1. rate sweep -----------------------------------------------------------
echo "=== 1. rate sweep (6 points, 10%-120% of estimated capacity, 3 reps/point) ==="
start_pair
"$OUT/inferbench" sweep \
  --workload "$OUT/sweep-base.json" --manifest "$OUT/facts-gateway.json" \
  --target "$GW_URL" --out "$OUT/sweep" \
  --max-conns 2 --repetitions 3 --points 6 \
  --min-fraction 0.10 --max-fraction 1.20 \
  --probe-rate 200 --probe-requests 150 --probe-max-slip 15s \
  --max-slip 15s --request-timeout 30s \
  2>&1 | tee "$OUT/sweep.log"

echo "=== knee detection on the pooled sweep points (analysis/knee.py) ==="
python3 - "$OUT" <<'PY' | tee "$OUT/knee-detection.log" || true
import json, sys, numpy as np
sys.path.insert(0, "analysis/src")
from inferbench_analysis.knee import detect_knee, SweepPoint

out = sys.argv[1]
man = json.load(open(f"{out}/sweep/sweep.json"))
points = []
for pt in man["points"]:
    vals = []
    for rep in range(1, pt["repetitions"] + 1):
        with open(f"{pt['run_dir']}/rep-{rep}/events.jsonl") as f:
            for line in f:
                ev = json.loads(line)
                if ev.get("ttft_seconds") is not None:
                    vals.append(ev["ttft_seconds"])
    p99 = float(np.quantile(vals, 0.99))
    points.append(SweepPoint(offered_rate_rps=pt["rate_rps"], value=p99))
    print(f"point {pt['index']}: rate={pt['rate_rps']:.4f} rps "
          f"({pt['fraction_of_capacity']*100:.0f}% of capacity) "
          f"pooled_ttft_p99={p99:.4f}s n={len(vals)}")

knee = detect_knee(points, signal="ttft_seconds_p99")
print()
if knee is None:
    print("KNEE: NOT FOUND")
    sys.exit(1)
print(f"KNEE FOUND: arrival_rate_rps={knee.arrival_rate_rps:.4f} "
      f"confidence={knee.confidence} bracketed={knee.bracketed}")
print(f"method: {knee.method}")
json.dump(
    {
        "arrival_rate_rps": knee.arrival_rate_rps,
        "signal": knee.signal,
        "method": knee.method,
        "confidence": knee.confidence,
        "bracketed": knee.bracketed,
        "capacity_estimate_rps": man["capacity_estimate_rps"],
    },
    open(f"{out}/knee-result.json", "w"),
    indent=2,
)
PY
knee_exit=${PIPESTATUS[0]}
if [ "$knee_exit" -ne 0 ]; then
  echo "FAIL: knee detector did not find a knee on the mock sweep"
  FAILED=1
else
  echo "PASS: knee detector found a bracketed saturation knee (see $OUT/knee-result.json)"
fi

# --- 2. replay determinism ---------------------------------------------------
echo
echo "=== 2. replay determinism ==="
cat >"$OUT/replay-workload.json" <<'EOF'
{
  "name": "ib-t008-replay",
  "version": "1.0.0",
  "seed": 8008030,
  "arrival_process": {"type": "open-loop-poisson", "rate_rps": 10},
  "input_length_distribution": {"type": "uniform", "min": 32, "max": 128},
  "output_length_distribution": {"type": "uniform", "min": 8, "max": 64},
  "prefix_sharing": {"ratio": 0},
  "cancellation": {"rate": 0},
  "slow_client": {"fraction": 0},
  "stop": {"request_count": 60}
}
EOF
"$OUT/inferbench" run --workload "$OUT/replay-workload.json" --manifest "$OUT/facts-gateway.json" \
  --target "$GW_URL" --out "$OUT/replay-original" --run-id ib-t008-replay-original --stream \
  2>&1 | tee "$OUT/replay-original.log"
"$OUT/inferbench" replay --workload "$OUT/replay-workload.json" --manifest "$OUT/facts-gateway.json" \
  --target "$GW_URL" --out "$OUT/replay-reissue" --run-id ib-t008-replay-reissue --stream \
  --reference-run "$OUT/replay-original" \
  2>&1 | tee "$OUT/replay-reissue.log"
grep -q "REPLAY DETERMINISTIC" "$OUT/replay-reissue.log" || { echo "FAIL: replay did not confirm determinism"; FAILED=1; }

# body-hash determinism cross-check (independent of the fingerprint check)
grep -o 'body_sha256=[a-f0-9]*' "$OUT/replay-original/run.log" | sort >"$OUT/replay-original-hashes.txt"
grep -o 'body_sha256=[a-f0-9]*' "$OUT/replay-reissue/run.log" | sort >"$OUT/replay-reissue-hashes.txt"
if diff -q "$OUT/replay-original-hashes.txt" "$OUT/replay-reissue-hashes.txt" >/dev/null; then
  echo "PASS: replay produced byte-identical response bodies ($(wc -l <"$OUT/replay-original-hashes.txt") hashes)"
else
  echo "FAIL: replay response bodies diverged from the original run"
  FAILED=1
fi

# negative control: a genuinely different seed must be REFUSED, not silently replayed
sed 's/8008030/8008031/' "$OUT/replay-workload.json" >"$OUT/replay-workload-diffseed.json"
set +e
"$OUT/inferbench" replay --workload "$OUT/replay-workload-diffseed.json" --manifest "$OUT/facts-gateway.json" \
  --target "$GW_URL" --out "$OUT/replay-mismatch" --run-id ib-t008-replay-mismatch \
  --reference-run "$OUT/replay-original" >"$OUT/replay-mismatch.log" 2>&1
mismatch_exit=$?
set -e
if [ "$mismatch_exit" -eq 0 ] || [ -d "$OUT/replay-mismatch" ]; then
  echo "FAIL: replay accepted a mismatched seed instead of refusing"
  FAILED=1
else
  echo "PASS: replay refused a mismatched seed before sending any request (exit $mismatch_exit)"
fi

# --- 3. A/B comparison smoke -------------------------------------------------
echo
echo "=== 3. A/B comparison smoke (direct vs via-gateway, single declared variable) ==="
cat >"$OUT/compare-workload.json" <<'EOF'
{
  "name": "ib-t008-compare",
  "version": "1.0.0",
  "seed": 8008040,
  "arrival_process": {"type": "open-loop-poisson", "rate_rps": 10},
  "input_length_distribution": {"type": "uniform", "min": 32, "max": 128},
  "output_length_distribution": {"type": "uniform", "min": 8, "max": 64},
  "prefix_sharing": {"ratio": 0},
  "cancellation": {"rate": 0},
  "slow_client": {"fraction": 0},
  "stop": {"request_count": 60}
}
EOF
"$OUT/inferbench" compare --workload "$OUT/compare-workload.json" --variable target_topology \
  --arm direct="$OUT/facts-direct.json@$MOCK_URL" \
  --arm gateway="$OUT/facts-gateway.json@$GW_URL" \
  --out "$OUT/compare" --stream --repetitions 1 \
  2>&1 | tee "$OUT/compare.log"
grep -q '"declared_variable": "target_topology"' "$OUT/compare/comparison.json" || { echo "FAIL: comparison.json missing declared_variable"; FAILED=1; }

echo
echo "=== negative control: compare refuses a second undeclared variable, before any traffic ==="
python3 - "$OUT/facts-gateway.json" "$OUT/facts-gateway-diffmodel.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["model"]["checkpoint"] = "different-model-8b"
json.dump(d, open(sys.argv[2], "w"), indent=2)
PY
set +e
"$OUT/inferbench" compare --workload "$OUT/compare-workload.json" --variable target_topology \
  --arm direct="$OUT/facts-direct.json@$MOCK_URL" \
  --arm gateway="$OUT/facts-gateway-diffmodel.json@$GW_URL" \
  --out "$OUT/compare-refused" >"$OUT/compare-refused.log" 2>&1
refused_exit=$?
set -e
if [ "$refused_exit" -eq 0 ] || [ -d "$OUT/compare-refused" ]; then
  echo "FAIL: compare accepted a multi-variable arm set instead of refusing"
  FAILED=1
else
  echo "PASS: compare refused a second undeclared variable before sending any request (exit $refused_exit)"
fi

stop_pair
echo
echo "=== kit validation of everything emitted ==="
CONTRACTS_BUNDLE=${CONTRACTS_BUNDLE:-}
if [ -n "$CONTRACTS_BUNDLE" ]; then
  : >"$OUT/kit-validate.log"
  set +e
  kit_exit=0
  # This tree mixes schema kinds under plain filenames (manifest.json,
  # events.jsonl, *-workload.json) rather than the check-subcommand's
  # <name>.<schema>.json convention (matching ib-t002/ib-t003/ib-t004
  # evidence), so each kind is validated explicitly.
  mapfile -t manifests < <(find "$OUT" -name 'manifest.json')
  if [ "${#manifests[@]}" -gt 0 ]; then
    python3 "$CONTRACTS_BUNDLE/kit/contracts-validate.py" --bundle "$CONTRACTS_BUNDLE" \
      validate --schema benchmark-run "${manifests[@]}" >>"$OUT/kit-validate.log" 2>&1 || kit_exit=1
  fi
  mapfile -t events < <(find "$OUT" -name 'events.jsonl')
  if [ "${#events[@]}" -gt 0 ]; then
    python3 "$CONTRACTS_BUNDLE/kit/contracts-validate.py" --bundle "$CONTRACTS_BUNDLE" \
      validate --schema raw-event "${events[@]}" >>"$OUT/kit-validate.log" 2>&1 || kit_exit=1
  fi
  mapfile -t wls < <(find "$OUT" -name '*workload*.json')
  if [ "${#wls[@]}" -gt 0 ]; then
    python3 "$CONTRACTS_BUNDLE/kit/contracts-validate.py" --bundle "$CONTRACTS_BUNDLE" \
      validate --schema workload "${wls[@]}" >>"$OUT/kit-validate.log" 2>&1 || kit_exit=1
  fi
  set -e
  tail -n 20 "$OUT/kit-validate.log"
  [ "$kit_exit" -eq 0 ] || FAILED=1
else
  echo "CONTRACTS_BUNDLE not set; skipping kit validation (run separately, see docs/evidence/ib-t008/)"
fi

echo
if [ "$FAILED" -ne 0 ]; then
  echo "IB-T008 VERIFICATION FAILED (see FAIL lines above); artifacts in $OUT"
  exit 1
fi
echo "IB-T008 VERIFICATION PASSED; artifacts in $OUT (infergate pin $PIN)"
