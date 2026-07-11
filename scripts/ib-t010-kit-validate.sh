#!/usr/bin/env bash
# IB-T010 kit validation: validate every artifact emitted by experiment set 1
# against the pinned serving-contracts bundle (v0.2.0 = 484b449).
#
# Usage: scripts/ib-t010-kit-validate.sh /path/to/serving-contracts-bundle
set -u
BUNDLE="${1:?usage: ib-t010-kit-validate.sh /path/to/contracts-bundle}"
cd "$(dirname "$0")/.."
OUT=docs/evidence/ib-t010
LOG="$OUT/kit-validate.log"
: > "$LOG"
fail=0

echo "# IB-T010 kit validation ($(date -u +%Y-%m-%dT%H:%M:%SZ))" | tee -a "$LOG"
echo "# bundle: $BUNDLE ($(git -C "$BUNDLE" rev-parse --short HEAD 2>/dev/null || echo unpinned-dir))" | tee -a "$LOG"

run_kit() { # run_kit <schema> <files...>
  local schema="$1"; shift
  [ "$#" -eq 0 ] && return 0
  python3 "$BUNDLE/kit/contracts-validate.py" --bundle "$BUNDLE" \
    validate --schema "$schema" "$@" >>"$LOG" 2>&1 || fail=1
}

mapfile -t manifests < <(find "$OUT" -name 'manifest.json' | sort)
run_kit benchmark-run "${manifests[@]}"

mapfile -t events < <(find "$OUT" -name 'events.jsonl' | sort)
run_kit raw-event "${events[@]}"

mapfile -t wls < <(find "$OUT" -maxdepth 1 -name '*workload*.json' | sort)
# sweep-derived workloads live inside e2-probe/
mapfile -t wls2 < <(find "$OUT/e2-probe" -name '*workload*.json' 2>/dev/null | sort)
run_kit workload "${wls[@]}" "${wls2[@]}"

mapfile -t slos < <(find "$OUT" -maxdepth 1 -name '*.slo.json' | sort)
run_kit slo "${slos[@]}"

mapfile -t results < <(find "$OUT/results" -name '*.benchmark-result.json' 2>/dev/null | sort)
run_kit benchmark-result "${results[@]}"

{
  echo
  echo "counts: ${#manifests[@]} manifests, ${#events[@]} raw-event files, $(( ${#wls[@]} + ${#wls2[@]} )) workloads, ${#slos[@]} slos, ${#results[@]} results"
  grep -c "PASS\|ok" "$LOG" >/dev/null 2>&1 || true
} | tee -a "$LOG"
tail -n 6 "$LOG"

if [ "$fail" -ne 0 ]; then
  echo "IB-T010 KIT VALIDATION FAILED (see $LOG)"
  exit 1
fi
echo "IB-T010 KIT VALIDATION GREEN (see $LOG)"
