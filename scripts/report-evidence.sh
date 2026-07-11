#!/usr/bin/env bash
# IB-T006 end-to-end evidence: render honest reports from the real
# IB-T004/IB-T005 evidence artifacts.
#
# Usage: scripts/report-evidence.sh /path/to/serving-contracts-bundle
#
# Run from the repo root. Emits into docs/evidence/ib-t006/ and logs to
# docs/evidence/ib-t006/report.log. Three reports:
#
#   1. ib-t006-calib-A.report.md      — regenerated from raw events (the
#      primary G4 sample: full manifest, interpretation rules, pooled tables
#      + bootstrap CIs, goodput with shed+stall adjacent, validity block,
#      one-command repro)
#   2. ib-t005-calib-A.report.md      — rendered FROM the emitted
#      benchmark-result file (the consumption path; no CI/dispersion
#      surfaces — stated in the report)
#   3. ib-t006-cancel-queued.report.md — WITHHELD-latency case (kind
#      no-samples: every request deliberately canceled before a first
#      byte). No benchmark-result file exists for this run (IB-T005 typed
#      exit 3); the report is the publishable artifact and must show WHY.
#
# No benchmark-result files are (re)emitted by this script — the report
# generator consumes them (IB-T005 owns emission). The kit sweep at the end
# re-validates the consumed result files at the pin.
set -u
BUNDLE="${1:?usage: report-evidence.sh /path/to/contracts-bundle}"
cd "$(dirname "$0")/.."

OUT=docs/evidence/ib-t006
LOG="$OUT/report.log"
mkdir -p "$OUT"
: > "$LOG"

note() { echo "$@" | tee -a "$LOG"; }

note "# IB-T006 report-generator evidence"
note "# bundle: $BUNDLE ($(git -C "$BUNDLE" rev-parse --short HEAD 2>/dev/null || echo unpinned-dir))"
note "# date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
note ""

fail=0

run_report() { # run_report <label> <expected-exit> args...
  local label="$1" want="$2"; shift 2
  note "## report: $label (expected exit $want)"
  note "\$ python3 -m inferbench_analysis report $*"
  python3 -m inferbench_analysis report "$@" >>"$LOG" 2>&1
  local got=$?
  if [ "$got" -ne "$want" ]; then
    note "!! $label: exit $got, expected $want"
    fail=1
  else
    note "ok: $label exit $got"
  fi
  note ""
}

# 1. primary G4 sample — regenerated from raw events
run_report "calib-A from raw events" 0 \
  --bundle "$BUNDLE" \
  --run docs/evidence/ib-t004/calib-A \
  --slo docs/evidence/ib-t005/mock-loopback.slo.json \
  --result-id ib-t006-calib-A \
  --out "$OUT"

# 2. consumption path — from the emitted benchmark-result file
run_report "ib-t005-calib-A from result file" 0 \
  --bundle "$BUNDLE" \
  --result docs/evidence/ib-t005/results/ib-t005-calib-A.benchmark-result.json \
  --out "$OUT"

# 3. withheld-latency case (no-samples): valid run, no result file exists;
#    the report must render the WHY, never a blank table.
run_report "cancel-queued (latency withheld)" 0 \
  --bundle "$BUNDLE" \
  --run docs/evidence/ib-t004/cancel-queued \
  --slo docs/evidence/ib-t005/mock-loopback.slo.json \
  --result-id ib-t006-cancel-queued \
  --out "$OUT"

note "## kit re-validation of the CONSUMED result files (none re-emitted here)"
python3 "$BUNDLE/kit/contracts-validate.py" --bundle "$BUNDLE" \
  check docs/evidence/ib-t005/results > "$OUT/kit-validate.log" 2>&1 || fail=1
tail -n 3 "$OUT/kit-validate.log" | tee -a "$LOG"

if [ "$fail" -ne 0 ]; then
  note "RESULT: FAIL (see $LOG)"
  exit 1
fi
note "RESULT: GREEN — 3 reports rendered (incl. 1 withheld-latency), consumed results kit-valid"
