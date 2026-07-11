#!/usr/bin/env bash
# IB-T007 CPU calibration: inferbench vs a llama.cpp-based reference tool.
#
# Runs, against ONE long-lived llama-server process (llama.cpp @ 8f114a9,
# qwen2.5-1.5b-instruct-q4_k_m):
#   1. `inferbench run` direct-to-engine (the generator under test).
#   2. The independent Python reference client (refclient.py), "shared"
#      arm: no CPU pinning, same core pool as the server.
#   3. The independent Python reference client, "pinned" arm: server
#      taskset to cores 0-2 (-t 3), client taskset to core 3.
#   4. llama-bench: a model-level tokens/sec anchor (different measurement
#      point -- documented, not directly comparable).
#
# Usage: scripts/calibrate-llamacpp-reference.sh
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
OUT=docs/evidence/ib-t007
LLAMA_BIN=/home/user/tools/llama.cpp/build/bin/llama-server
LLAMA_BENCH_BIN=/home/user/tools/llama.cpp/build/bin/llama-bench
MODEL=/home/user/tools/models/qwen2.5-1.5b-instruct-q4_k_m.gguf
INFERBENCH_BIN=/tmp/inferbench-bin
LLAMA_ADDR=127.0.0.1:19299
LLAMA_URL=http://$LLAMA_ADDR

mkdir -p "$OUT"

echo "=== quiet-box check (before) ===" | tee "$OUT/quiet-box.txt"
date -u | tee -a "$OUT/quiet-box.txt"
uptime | tee -a "$OUT/quiet-box.txt"
ps aux | grep -Ei 'llama-server|llama-bench|inferbench-bin|refclient' | grep -v grep | tee -a "$OUT/quiet-box.txt" || echo "no matching processes" | tee -a "$OUT/quiet-box.txt"
nproc | tee -a "$OUT/quiet-box.txt"

LLAMA_PID=""
stop_server() {
  if [ -n "$LLAMA_PID" ]; then
    kill "$LLAMA_PID" 2>/dev/null || true
    wait "$LLAMA_PID" 2>/dev/null || true
    LLAMA_PID=""
  fi
}
trap stop_server EXIT

echo "=== starting llama-server (unpinned, -t 4) ==="
"$LLAMA_BIN" -m "$MODEL" --host 127.0.0.1 --port 19299 -np 1 -c 4096 -t 4 \
  --metrics --log-disable --no-webui \
  >"$OUT/llama-server.log" 2>&1 &
LLAMA_PID=$!
for i in $(seq 1 180); do
  curl -fsS "$LLAMA_URL/health" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS "$LLAMA_URL/health" >/dev/null 2>&1 || { echo "llama-server did not become healthy"; exit 1; }
echo "llama-server healthy (unpinned)"

echo "=== 1) inferbench run (direct-to-engine) ==="
"$INFERBENCH_BIN" run \
  --workload "$OUT/inferbench-workload.json" \
  --manifest "$OUT/inferbench-facts.json" \
  --target "$LLAMA_URL" \
  --out "$OUT/inferbench-run" \
  --model qwen2.5-1.5b-instruct-q4_k_m \
  --stream --max-slip 10s --request-timeout 60s \
  2>&1 | tee "$OUT/inferbench-run.log"

echo "=== 2) reference client: shared (unpinned) arm ==="
python3 "$OUT/refclient.py" \
  --base-url "$LLAMA_URL" --model qwen2.5-1.5b-instruct-q4_k_m \
  --n 30 --warmup 5 --seed 1007002 \
  --input-min-words 18 --input-max-words 56 \
  --output-min-tokens 16 --output-max-tokens 40 \
  --timeout 60 --label shared --out "$OUT/refclient-shared.json" \
  2>&1 | tee "$OUT/refclient-shared.log"

echo "=== stopping unpinned llama-server, starting pinned pair ==="
stop_server

"$LLAMA_BIN" -m "$MODEL" --host 127.0.0.1 --port 19299 -np 1 -c 4096 -t 3 \
  --metrics --log-disable --no-webui \
  >"$OUT/llama-server-pinned.log" 2>&1 &
LLAMA_PID=$!
if command -v taskset >/dev/null 2>&1; then
  taskset -cp 0-2 "$LLAMA_PID" >>"$OUT/llama-server-pinned.log" 2>&1 || echo "taskset -cp failed (non-fatal)" | tee -a "$OUT/llama-server-pinned.log"
else
  echo "taskset not available -- pinned arm will run WITHOUT real CPU pinning (recorded as a threat to validity)" | tee -a "$OUT/llama-server-pinned.log"
fi
for i in $(seq 1 180); do
  curl -fsS "$LLAMA_URL/health" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS "$LLAMA_URL/health" >/dev/null 2>&1 || { echo "pinned llama-server did not become healthy"; exit 1; }
echo "llama-server healthy (pinned, -t 3, cores 0-2)"

echo "=== 3) reference client: taskset-pinned arm (core 3) ==="
if command -v taskset >/dev/null 2>&1; then
  taskset -c 3 python3 "$OUT/refclient.py" \
    --base-url "$LLAMA_URL" --model qwen2.5-1.5b-instruct-q4_k_m \
    --n 30 --warmup 5 --seed 1007002 \
    --input-min-words 18 --input-max-words 56 \
    --output-min-tokens 16 --output-max-tokens 40 \
    --timeout 60 --label taskset-pinned --out "$OUT/refclient-pinned.json" \
    2>&1 | tee "$OUT/refclient-pinned.log"
else
  python3 "$OUT/refclient.py" \
    --base-url "$LLAMA_URL" --model qwen2.5-1.5b-instruct-q4_k_m \
    --n 30 --warmup 5 --seed 1007002 \
    --input-min-words 18 --input-max-words 56 \
    --output-min-tokens 16 --output-max-tokens 40 \
    --timeout 60 --label taskset-pinned-NOTPINNED --out "$OUT/refclient-pinned.json" \
    2>&1 | tee "$OUT/refclient-pinned.log"
fi

echo "=== 4) llama-bench model-level anchor ==="
"$LLAMA_BENCH_BIN" -m "$MODEL" -p 64 -n 32 -t 4 -r 5 -o json \
  >"$OUT/llama-bench.json" 2>"$OUT/llama-bench.err.log" || echo "llama-bench exited non-zero (see llama-bench.err.log)"

stop_server

echo "=== quiet-box check (after) ===" | tee -a "$OUT/quiet-box.txt"
uptime | tee -a "$OUT/quiet-box.txt"

echo "IB-T007 calibration runs complete: $OUT"
