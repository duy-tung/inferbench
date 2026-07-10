#!/usr/bin/env bash
# IB-T003 workload-suite dry run.
#
# Derives short, low-volume variants of the canonical workloads (stop
# condition shortened; bursty phases compressed 5x -- everything else,
# including seeds and length distributions, unchanged) and runs each against
# an already-running Contract 1 target. The three workloads whose defining
# feature is deferred to IB-T004 (shared-prefix, cancel-storm, slow-client)
# are expected to REFUSE with the typed ErrNotImplemented -- that refusal is
# asserted, not worked around.
#
# Derived variants are smoke-only. Published results use workloads/*.json
# as-is (docs/experiments.md rule 10: comparability keys on version + seed).
#
# Usage: scripts/dryrun-workloads.sh <target-url> <out-dir> [manifest-facts]
set -euo pipefail

TARGET=${1:?target url required}
OUT=${2:?out dir required}
MANIFEST=${3:-testdata/live/ib-t003-dryrun.manifest.json}

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
mkdir -p "$OUT/derived"

BIN="$OUT/inferbench"
go build -o "$BIN" ./cmd/inferbench

# derive <name> <jq-filter>
derive() {
  jq "$2" "workloads/$1.json" >"$OUT/derived/$1.json"
}

# Shortened stop conditions (canonical rates already low; wall time per run
# stays under ~1 min against the mock pair).
derive chat-short   '.stop = {"request_count": 120}'
derive rag-long-in  '.stop = {"request_count": 60}'
derive gen-long-out '.stop = {"request_count": 45}'
derive mixed        '.stop = {"request_count": 100}'
# bursty: compress phase durations 5x (12s base + 3s burst, period 15s) and
# stop after three cycles so the dry run still crosses burst boundaries.
derive bursty       '.arrival_process.phases |= map(.duration_seconds /= 5) | .stop = {"duration_seconds": 45}'
# deferred-feature workloads run unmodified; they refuse before any send.
derive shared-prefix '.'
derive cancel-storm  '.'
derive slow-client   '.'

RUNNABLE="chat-short rag-long-in gen-long-out mixed bursty"
DEFERRED="shared-prefix cancel-storm slow-client"

fail=0
for w in $RUNNABLE; do
  echo "=== dry-run $w ==="
  if "$BIN" run --workload "$OUT/derived/$w.json" --manifest "$MANIFEST" \
      --target "$TARGET" --out "$OUT/$w" --run-id "dryrun-$w" \
      2>&1 | tee "$OUT/$w.dryrun.log"; then
    echo "OK: $w"
  else
    echo "FAIL: $w did not complete"; fail=1
  fi
done

for w in $DEFERRED; do
  echo "=== typed-refusal check $w ==="
  if "$BIN" run --workload "$OUT/derived/$w.json" --manifest "$MANIFEST" \
      --target "$TARGET" --out "$OUT/$w" --run-id "dryrun-$w" \
      >"$OUT/$w.dryrun.log" 2>&1; then
    echo "FAIL: $w ran but its feature is deferred (expected typed refusal)"; fail=1
  elif grep -q "workload feature not implemented in this build" "$OUT/$w.dryrun.log"; then
    echo "OK: $w refused with typed ErrNotImplemented:"
    cat "$OUT/$w.dryrun.log"
  else
    echo "FAIL: $w failed for an unexpected reason:"; cat "$OUT/$w.dryrun.log"; fail=1
  fi
done

exit $fail
