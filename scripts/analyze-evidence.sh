#!/usr/bin/env bash
# IB-T005 end-to-end evidence: analyze the real IB-T002/IB-T004 raw-event runs
# into benchmark-result files and kit-validate everything emitted.
#
# Usage: scripts/analyze-evidence.sh /path/to/serving-contracts-bundle
#
# Run from the repo root. Emits into docs/evidence/ib-t005/results/ and logs
# to docs/evidence/ib-t005/analyze.log. Two runs (cancel-queued,
# cancel-pre-first-token) are EXPECTED to exit 3: they are valid runs whose
# latency tables are not expressible (no TTFT samples — every request was
# deliberately canceled before a first byte); the typed refusal is the
# evidence there.
set -u
BUNDLE="${1:?usage: analyze-evidence.sh /path/to/contracts-bundle}"
cd "$(dirname "$0")/.."

OUT=docs/evidence/ib-t005
SLO="$OUT/mock-loopback.slo.json"
LOG="$OUT/analyze.log"
mkdir -p "$OUT/results"
: > "$LOG"

note() { echo "$@" | tee -a "$LOG"; }

note "# IB-T005 end-to-end analysis of ib-t002/ib-t004 evidence runs"
note "# bundle: $BUNDLE ($(git -C "$BUNDLE" rev-parse --short HEAD 2>/dev/null || echo unpinned-dir))"
note "# date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
note ""

fail=0

analyze() { # analyze <run-dir> <result-id> <expected-exit> [extra args...]
  local dir="$1" id="$2" want="$3"; shift 3
  note "## analyze $dir -> $id (expected exit $want)"
  python3 -m inferbench_analysis analyze \
    --bundle "$BUNDLE" \
    --run "$dir" \
    --slo "$SLO" \
    --result-id "$id" \
    --out "$OUT/results/$id.benchmark-result.json" \
    "$@" >>"$LOG" 2>&1
  local got=$?
  if [ "$got" -ne "$want" ]; then
    note "!! $id: exit $got, expected $want"
    fail=1
  else
    note "ok: $id exit $got"
  fi
  note ""
}

# IB-T002 generator-core runs (200 events each)
analyze docs/evidence/ib-t002/smoke-A  ib-t005-smoke-A  0 \
  --threat "non-streaming run: client TTFT equals full-response arrival (first body byte of a non-streamed JSON response), not a streaming first-token time"
analyze docs/evidence/ib-t002/stream-SA ib-t005-stream-SA 0

# IB-T004 calibration + scenario runs
analyze docs/evidence/ib-t004/calib-A ib-t005-calib-A 0
analyze docs/evidence/ib-t004/calib-B ib-t005-calib-B 0
analyze docs/evidence/ib-t004/slow-control ib-t005-slow-control 0
analyze docs/evidence/ib-t004/slow-on ib-t005-slow-on 0 \
  --threat "slow-client run: the client deliberately throttles its own reads (4096 B/s + 0.2 s initial delay); latency figures measure that self-imposed backpressure by design"
analyze docs/evidence/ib-t004/cancel-mid-stream ib-t005-cancel-mid-stream 0

# valid runs whose latency table is NOT expressible (no TTFT samples):
# typed exit 3, no result file — that refusal is the honest output.
analyze docs/evidence/ib-t004/cancel-queued ib-t005-cancel-queued 3
analyze docs/evidence/ib-t004/cancel-pre-first-token ib-t005-cancel-pre-first-token 3

note "## kit validation of everything under $OUT"
python3 "$BUNDLE/kit/contracts-validate.py" --bundle "$BUNDLE" \
  validate --schema slo "$SLO" >>"$LOG" 2>&1 || fail=1
python3 "$BUNDLE/kit/contracts-validate.py" --bundle "$BUNDLE" \
  check "$OUT/results" >>"$LOG" 2>&1 || fail=1
tail -n 4 "$LOG"

if [ "$fail" -ne 0 ]; then
  note "RESULT: FAIL (see $LOG)"
  exit 1
fi
note "RESULT: GREEN — 7 results emitted + kit-valid, 2 typed latency-withheld refusals as expected"
