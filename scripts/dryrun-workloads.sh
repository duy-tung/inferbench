#!/usr/bin/env bash
# Workload-suite dry run (IB-T003 evidence; regenerated at IB-T004).
#
# Derives short, low-volume variants of the canonical workloads (stop
# condition shortened; bursty phases compressed 5x -- everything else,
# including seeds and length distributions, unchanged) and runs each against
# an already-running Contract 1 target as a STREAMING (SSE) run. Since
# IB-T004 all eight workloads execute end-to-end: prefix-sharing prompt
# construction, cancellation issuance, and slow-client read throttling are
# implemented (closed-loop arrival remains the only typed refusal, IB-T008).
#
# Derived variants are smoke-only. Published results use workloads/*.json
# as-is (docs/experiments.md rule 10: comparability keys on version + seed).
#
# NOTE for slow-client: the run's wall time is dominated by the slowest
# throttled stream (bytes / read_bytes_per_second), so the target gateway
# should run with an upstream/stream timeout above that bound and the
# per-request client timeout is raised accordingly below.
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
# stays under ~1 min against the mock pair, except slow-client whose wall
# time is bounded by the throttled read of the longest stream).
derive chat-short    '.stop = {"request_count": 120}'
derive rag-long-in   '.stop = {"request_count": 60}'
derive gen-long-out  '.stop = {"request_count": 45}'
derive mixed         '.stop = {"request_count": 100}'
# bursty: compress phase durations 5x (12s base + 3s burst, period 15s) and
# stop after three cycles so the dry run still crosses burst boundaries.
derive bursty        '.arrival_process.phases |= map(.duration_seconds /= 5) | .stop = {"duration_seconds": 45}'
derive shared-prefix '.stop = {"request_count": 60}'
derive cancel-storm  '.stop = {"request_count": 90}'
derive slow-client   '.stop = {"request_count": 40}'

# Per-workload request timeout: slow-client streams are deliberately slow
# readers (up to ~256 mock tokens x ~170 B at 1024 B/s ~= 43s + delays).
timeout_for() {
  case "$1" in
    slow-client) echo 120s ;;
    *) echo 60s ;;
  esac
}

fail=0
for w in chat-short rag-long-in gen-long-out shared-prefix mixed bursty cancel-storm slow-client; do
  echo "=== dry-run $w (streaming) ==="
  if "$BIN" run --workload "$OUT/derived/$w.json" --manifest "$MANIFEST" \
      --target "$TARGET" --out "$OUT/$w" --run-id "dryrun-$w" --stream \
      --request-timeout "$(timeout_for "$w")" \
      2>&1 | tee "$OUT/$w.dryrun.log"; then
    echo "OK: $w"
  else
    echo "FAIL: $w did not complete"; fail=1
  fi
done

exit $fail
