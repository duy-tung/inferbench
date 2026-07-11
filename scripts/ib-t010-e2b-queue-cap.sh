#!/usr/bin/env bash
# IB-T010 E2b: queue-cap follow-up to E2's REFUTED G5 verdict (docs/evidence/
# ib-t010/benchmark-report-1.md Sec 3, +25.16% accepted-TTFT p95 degradation
# at 5x, root-caused to queue-transit under a perpetually-full cap=3 queue).
# Prescribed by the fresh-context G5 gate verifier: single changed variable
# is the queue cap (-admission-tenant-queue-cap AND -admission-global-queue-cap,
# paired, 3 -> 1); -admission-global-inflight-budget=6 and
# -admission-queue-deadline=500ms held fixed at E2's values. Reuses E2's own
# baseline/overload workload files VERBATIM (same seeds/rates/request-counts)
# so the ONLY thing that differs from E2's admission-sane-v1 numbers is the
# queue-cap value -- single-variable purity, not a second re-probed rate.
#
# Usage: scripts/ib-t010-e2b-queue-cap.sh
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
OUT=docs/evidence/ib-t010

MOCK_ADDR=127.0.0.1:19294
GW_ADDR=127.0.0.1:19295
MOCK_URL=http://$MOCK_ADDR
GW_URL=http://$GW_ADDR

echo "=== quiet-box check ==="
ps aux | grep -E 'llama-server|gateway-bin|mock-backend-bin' | grep -v grep || echo "no matching processes"
uptime

MOCK_PID=""; GW_PID=""
stop_all() {
  [ -n "$GW_PID" ] && kill "$GW_PID" 2>/dev/null && wait "$GW_PID" 2>/dev/null || true
  [ -n "$MOCK_PID" ] && kill "$MOCK_PID" 2>/dev/null && wait "$MOCK_PID" 2>/dev/null || true
  MOCK_PID=""; GW_PID=""
}
trap stop_all EXIT

"$OUT/mock-backend-bin" -addr "$MOCK_ADDR" -seed 43 -ttft 80ms -itl 10ms >"$OUT/e2b-mock.log" 2>&1 &
MOCK_PID=$!

# admission-sane-v1b: SAME as E2's admission-sane-v1 (budget 6, deadline
# 500ms) except queue caps (tenant AND global, paired) shrunk 3 -> 1 -- the
# single declared variable of this follow-up experiment.
"$OUT/gateway-bin" -addr "$GW_ADDR" -backend-url "$MOCK_URL" \
  -upstream-timeout 30s -stream-write-timeout 30s \
  -admission-tenant-queue-cap 1 -admission-global-inflight-budget 6 \
  -admission-global-queue-cap 1 -admission-queue-deadline 500ms \
  >"$OUT/e2b-gateway-sane.log" 2>&1 &
GW_PID=$!

for i in $(seq 1 50); do
  curl -fsS "$MOCK_URL/healthz" >/dev/null 2>&1 && curl -fsS "$GW_URL/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS "$GW_URL/healthz" >/dev/null 2>&1 || { echo "gateway did not become healthy"; exit 1; }
echo "mock + gateway healthy"

# --- 1. capacity probe against admission-sane-v1b (structural parity with
# E2, and a cross-check that capacity has not shifted -- capacity is
# budget-bound (inflight budget=6, unchanged) not queue-cap-bound, so this
# is expected to land close to E2's 37.8072 rps). NOT used to re-derive the
# measured-point rates -- E2's own baseline/overload workload files are
# reused verbatim below for single-variable purity (see header + hypothesis
# file 'workload' field).
echo "=== capacity probe (admission-sane-v1b, cross-check only) ==="
"$OUT/inferbench-bin" sweep \
  --workload "$OUT/e2-probe-base-workload.json" --manifest "$OUT/e2b-facts-sane.json" \
  --target "$GW_URL" --out "$OUT/e2b-probe" \
  --max-conns 1000 --repetitions 1 --points 1 --min-fraction 1.0 --max-fraction 1.0 \
  --probe-rate 200 --probe-requests 400 --probe-max-slip 60s \
  --max-slip 60s --request-timeout 15s --stream \
  2>&1 | tee "$OUT/e2b-probe.log" || true
[ -f "$OUT/e2b-probe/sweep.json" ] || { echo "probe did not write sweep.json"; exit 1; }
CAP=$(python3 -c "import json; print(json.load(open('$OUT/e2b-probe/sweep.json'))['capacity_estimate_rps'])")
echo "capacity_estimate_rps(admission-sane-v1b, cross-check)=$CAP -- measured rates below are E2's own fixed 37.8072 / 189.0362"
echo "$CAP" > "$OUT/e2b-capacity-estimate-rps.txt"

# --- 2. baseline (~1x, E2's exact workload file reused verbatim), 3 reps ---
echo "=== baseline @ ~1x capacity (admission-sane-v1b, E2 workload reused) ==="
for rep in 1 2 3; do
  "$OUT/inferbench-bin" experiment run \
    --hypothesis hypotheses/EXP-ib-t010-e2b-queue-cap.json \
    --workload "$OUT/e2-baseline-workload.json" --manifest "$OUT/e2b-facts-sane.json" \
    --target "$GW_URL" --out "$OUT/e2b-baseline/rep-$rep" \
    --run-id "ib-t010-e2b-baseline-r$rep" --repetition "$rep" --stream \
    2>&1 | tee -a "$OUT/e2b-baseline.log"
done

# --- 3. spot-check: raw HTTP shed response at the new, shallower cap ---
echo "=== spot-check: raw HTTP shed response (admission-sane-v1b, burst beyond budget) ==="
{
  echo "burst of 20 concurrent requests against admission-sane-v1b (budget=6, queue-cap=1); expect ~7 200s (6 in-flight + 1 queued) and ~13 503s with Retry-After"
  for i in $(seq 1 20); do
    curl -sS -D - -o /dev/null --max-time 10 "$GW_URL/v1/chat/completions" \
      -H 'Content-Type: application/json' \
      -d '{"model":"mock-8b","messages":[{"role":"user","content":"spot check"}],"max_tokens":8,"stream":false}' \
      2>&1 | grep -iE '^HTTP|^Retry-After' &
  done
  wait
} | tee "$OUT/e2b-shed-spotcheck.log"

# --- 4. overload (~5x, E2's exact workload file reused verbatim), 3 reps ---
# Single arm (no on/off comparison): the variable under test is the
# queue-cap VALUE, not admission on/off, so `experiment run` (not `compare`)
# is used here, exactly as E2 used `run` for its own baseline. E2's own
# admission-off-v1 5x point (0% shed, p95 83.2ms, the no-queueing floor this
# hypothesis's mechanism argument references) remains the relevant
# off-control reference on file; nothing about the off-config changed, so
# it is not re-run.
echo "=== overload @ ~5x offered rate (admission-sane-v1b, E2 workload reused) ==="
for rep in 1 2 3; do
  "$OUT/inferbench-bin" experiment run \
    --hypothesis hypotheses/EXP-ib-t010-e2b-queue-cap.json \
    --workload "$OUT/e2-overload-workload.json" --manifest "$OUT/e2b-facts-sane.json" \
    --target "$GW_URL" --out "$OUT/e2b-overload/rep-$rep" \
    --run-id "ib-t010-e2b-overload-r$rep" --repetition "$rep" --stream \
    2>&1 | tee -a "$OUT/e2b-overload.log"
done

# --- 5. gateway-side evidence scraped BEFORE teardown: shed reasons +
# TTFT/queue-wait histograms. Process-cumulative over BOTH points (baseline
# + overload) since this is one long-lived gateway process -- same
# limitation as E2, disclosed the same way (cross-check only, never the
# primary basis).
python3 "$OUT/scrape_ttft_hist.py" "$GW_URL/metrics" --diff "$OUT/empty-before.json" \
  | tee "$OUT/e2b-gateway-side-percentiles.json"
curl -sS "$GW_URL/metrics" | grep -E "^inference_sheds_total|^inference_requests_total" \
  | tee "$OUT/e2b-gateway-metrics.txt"

echo "E2b queue-cap experiment complete: $OUT/e2b-overload (baseline: $OUT/e2b-baseline)"
