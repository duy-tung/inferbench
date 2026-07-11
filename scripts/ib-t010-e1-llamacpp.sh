#!/usr/bin/env bash
# IB-T010 E1 llama.cpp sub-experiment: direct-vs-gateway TTFT overhead against
# a REAL engine (llama.cpp @ 8f114a9, qwen2.5-1.5b-instruct-q4_k_m). Reuses
# the gateway/inferbench binaries already built into docs/evidence/ib-t010/
# by the mock sub-experiment (same infergate pin).
#
# Usage: scripts/ib-t010-e1-llamacpp.sh
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
OUT=docs/evidence/ib-t010
LLAMA_BIN=/home/user/tools/llama.cpp/build/bin/llama-server
MODEL=/home/user/tools/models/qwen2.5-1.5b-instruct-q4_k_m.gguf

LLAMA_ADDR=127.0.0.1:19199
GW_ADDR=127.0.0.1:19198
GW_URL=http://$GW_ADDR
LLAMA_URL=http://$LLAMA_ADDR

echo "=== quiet-box check ==="
ps aux | grep -E 'llama-server|gateway-bin|mock-backend-bin' | grep -v grep || echo "no matching processes"
uptime

LLAMA_PID=""; GW_PID=""
stop_pair() {
  [ -n "$GW_PID" ] && kill "$GW_PID" 2>/dev/null && wait "$GW_PID" 2>/dev/null || true
  [ -n "$LLAMA_PID" ] && kill "$LLAMA_PID" 2>/dev/null && wait "$LLAMA_PID" 2>/dev/null || true
  GW_PID=""; LLAMA_PID=""
}
trap stop_pair EXIT

"$LLAMA_BIN" -m "$MODEL" --host 127.0.0.1 --port 19199 -np 1 -c 4096 -t 4 --metrics --log-disable --no-webui \
  >"$OUT/e1-llamacpp-server.log" 2>&1 &
LLAMA_PID=$!
for i in $(seq 1 180); do
  curl -fsS "$LLAMA_URL/health" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS "$LLAMA_URL/health" >/dev/null 2>&1 || { echo "llama-server did not become healthy"; exit 1; }
echo "llama-server healthy"

"$OUT/gateway-bin" -addr "$GW_ADDR" -backend-url "$LLAMA_URL" -backend-name llamacpp \
  -models qwen2.5-1.5b-instruct-q4_k_m -upstream-timeout 120s -stream-write-timeout 60s \
  >"$OUT/e1-llamacpp-gateway.log" 2>&1 &
GW_PID=$!
for i in $(seq 1 50); do
  curl -fsS "$GW_URL/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS "$GW_URL/healthz" >/dev/null 2>&1 || { echo "gateway did not become healthy"; exit 1; }
echo "gateway healthy"

"$OUT/inferbench-bin" experiment compare \
  --hypothesis hypotheses/EXP-ib-t010-e1-gateway-overhead.json \
  --workload "$OUT/e1-llamacpp-workload.json" --variable target_topology \
  --arm direct="$OUT/e1-llamacpp-facts-direct.json@$LLAMA_URL" \
  --arm gateway="$OUT/e1-llamacpp-facts-gateway.json@$GW_URL" \
  --out "$OUT/e1-llamacpp-compare" --model qwen2.5-1.5b-instruct-q4_k_m \
  --stream --repetitions 3 --max-slip 5s --request-timeout 60s \
  2>&1 | tee "$OUT/e1-llamacpp-compare.log"

# Gateway-side TTFT/queue-wait histograms (Contract 2 gateway series), scraped
# BEFORE teardown -- the client-side pooled percentiles are the primary basis
# (scheduled-send, ADR-0001); this is the mirror-series cross-check.
python3 "$OUT/scrape_ttft_hist.py" "$GW_URL/metrics" --diff "$OUT/empty-before.json" \
  | tee "$OUT/e1-llamacpp-gateway-side-percentiles.json"

echo "E1 llama.cpp sub-experiment complete: $OUT/e1-llamacpp-compare"
