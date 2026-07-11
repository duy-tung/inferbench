# IB-T007 — Calibration vs reference tooling (CPU variant)

**Date:** 2026-07-11 · **Status:** PASSED — deltas within the declared tolerance **or** explained
by an enumerated difference (ADR-0004 outcome rule; the IB-T007 stop condition is "deltas
explained", not "deltas tiny").
**Task:** IB-T007 — prove inferbench's numbers are not an artifact of the generator.
**Variant:** CPU fallback (llama.cpp-based). The GPU variant (vs `vllm bench serve`) stays deferred
behind gate G6 (no GPU in this environment — program rule; recorded in `docs/tasks.md` IB-T007 and
`docs/milestones.md` M6).

This is **calibration evidence for generator trustworthiness — NOT a benchmark and NOT a
target-performance claim** (ADR-0004 scope guard). No number here may be cited as a performance
result for llama.cpp, qwen2.5, or this host.

## 0. Reference tooling used (volatility guard, ADR-0004 §5)

llama.cpp ships two independent measurement surfaces; this calibration uses both, plus a third
independent client of my own:

1. **llama-server `timings`** — the server's own per-request self-reported prompt/decode timings
   (`prompt_n`/`prompt_ms`/`prompt_per_second`, `predicted_n`/`predicted_ms`/`predicted_per_second`),
   attached to the final SSE chunk of a streaming chat completion when
   `stream_options.include_usage=true`. Verified present in the pinned source at
   `tools/server/server-task.cpp:540` (`to_json_oaicompat_chat_stream`) and computed in
   `tools/server/server-context.cpp:504` (`get_timings`). The infergate adapter strips these
   client-facing, but a **direct-to-engine** run sees them on the wire. This is the primary
   reference: a server-side measurement of the *same request* inferbench measured client-side.
2. **`llama-bench`** — model-level tokens/sec (no HTTP server, no SSE, no scheduler). A different
   measurement point; used only as an order-of-magnitude throughput anchor, with the comparability
   caveat stated in §5.
3. **An independent Python reference client** (`refclient.py`) — a deliberately separate
   implementation of client-side TTFT/ITL capture that shares **no code** with inferbench's Go
   client (`internal/client/client.go`). Agreement between the two therefore cannot be a shared-bug
   artifact — which is the whole point of IB-T007.

**Pins (re-verified at use time, 2026-07-11):**
- llama.cpp `8f114a9b573b69035299f9b924047f53c1e22c7e` (built read-only; `llama-server`,
  `llama-bench`).
- Model `qwen2.5-1.5b-instruct-q4_k_m.gguf`, sha256
  `6a1a2eb6d15622bf3c96857206351ba97e1af16c30d7a74ee38970e434e9407e`.
- llama-server flags: `-np 1 -c 4096 -t 4 --metrics --log-disable --no-webui` (single slot — this
  matters; see §2). Loopback, 4 vCPU Xeon @ 2.80 GHz, CPU-only.
- inferbench built from this repo (working tree at task start, `cc404a6`).
- Contracts bundle `serving-contracts` v0.2.0 (`484b449`) — the emitted inferbench run artifacts
  kit-validate (`kit-validate.log`).

**Quiet box:** `quiet-box.txt` — before each measurement pass: no competing `llama`/`gateway`/
`inferbench` processes, load average 1.31 at start. Passes ran strictly sequentially (server + one
client at a time); no benchmark load competed *during* a measurement pass. (`llama-bench` ran last,
alone, after all latency passes finished — the load-average 3.38 in the "after" snapshot is
llama-bench itself.)

**Reproduce (one command, server included):**
```
scripts/calibrate-llamacpp-reference.sh
```

---

## 1. Differences enumerated BEFORE comparing (ADR-0004 §2 — mandatory)

Filled in from the run design, which was fixed in the scripts before any request was sent. Any
delta must attribute to a row here or it counts as unexplained.

| Dimension | inferbench (under test) | reference client (`refclient.py`) | llama-server `timings` | Comparable? |
|---|---|---|---|---|
| **Measurement point (TTFT)** | `scheduled_send_ts` → first response **body byte** of a content delta (CO-safe basis, `internal/client/client.go`) | dispatch instant → first content-bearing SSE delta | `prompt_ms` = prompt-eval wall time (server-internal; excludes transport, dispatch, first-token decode) | client-vs-client: **yes**; client-vs-server: **related, not identical** (server excludes transport + first decode) |
| **Measurement point (ITL)** | gap between content SSE chunks, stamped at client arrival | gap between content SSE chunks, stamped at client arrival | `predicted_ms / predicted_n` = mean server decode time per token | **yes** (client ITL ≈ server per-token decode + jitter) |
| **Arrival process** | **open-loop Poisson**, 0.2 rps, send schedule precomputed from seed (ADR-0001) | **sequential/closed-loop** (request i+1 only after i completes) | n/a (per-request) | **NO — the key enumerated difference.** On a single-slot engine, open-loop arrivals can cluster and **queue**; the sequential client never queues. Isolated in §2. |
| **Warm-up** | 5 requests discarded (manifest `discard-requests`, value 5) | 5 requests discarded (`--warmup 5`) | n/a | yes (same policy) |
| **Workload / prompt construction** | inferbench synthetic vocab, ~4 B/token sizing; input 24–80 tok uniform, output 16–40 tok | own vocab, word-count prompts 18–56 words; output 16–40 tok | n/a | **partial** — different prompts → different prompt-token counts → different `prompt_ms`; a secondary ~100 ms p50 TTFT contributor (§2). |
| **Decoding** | model default sampler | temperature 0, fixed sampler seed (greedy, reproducible) | reflects whatever the request asked | affects `predicted_n` spread, not per-token rate materially |
| **Timer source** | Go monotonic `time.Now()` pairs | Python `time.perf_counter()` (monotonic) | server `ggml_time_us()` | yes (all monotonic) |
| **Token counting** | usage chunk (`stream_options.include_usage`) | usage chunk | server-internal counters | yes |
| **Connection handling** | new/keep-alive per transport, no client retries (ADR-0001) | `requests.Session` keep-alive, no retries | n/a | yes |

**Declared tolerances** (declared here from the IB-T004 mock-calibration precedent + the physics of
a CPU engine; **not** chosen after seeing deltas — the same-basis rows are what a correct generator
must satisfy):
- **Client ITL vs client ITL** (inferbench vs reference): p50 Δ within **±10 ms**, p95 Δ within
  **±20 ms** (CPU decode ≈ 46 ms/token, noisier than the mock's 20 ms).
- **Non-queued client TTFT** (inferbench vs reference, arrival-process difference removed): p50 Δ
  within **±150 ms** (the two tools use different prompts; server `prompt_ms` alone spans
  267–996 ms across requests, so ±150 ms is the prompt-length envelope, not slack).
- **Client TTFT vs server `prompt_ms`** (paired, same request): expected **positive and < 100 ms**
  on loopback (transport + dispatch + first-token decode; the server basis excludes all three).
- **Client ITL vs server decode/token** (paired): within **±10 ms**.
- **Whole-distribution TTFT** (inferbench open-loop vs sequential reference): **no tolerance
  declared — not directly comparable** by the enumerated arrival-process row; the delta is
  *explained*, not bounded (ADR-0004 outcome rule, "or every delta explained").

---

## 2. Comparison A — inferbench vs the independent reference client (client-side TTFT/ITL)

Same llama-server process, same warm-up policy, same-shape short workload. Warm-up 5 excluded;
n = 25 kept each. Evidence-grade nearest-rank percentiles (`compare_calibration.py`; the IB-T005
bootstrap engine is for published benchmark claims, not tool-calibration diagnostics).

### 2a. Whole distribution (NOT directly comparable — arrival-process difference)

| Series | inferbench (open-loop 0.2 rps) | reference (sequential) | Δ p50 | Δ p95 |
|---|---|---|---|---|
| client TTFT p50 | 0.665 s | 0.438 s | +0.227 s | — |
| client TTFT p95 | 4.162 s | 0.884 s | — | +3.278 s |
| client TTFT max | 5.754 s | 1.003 s | — | — |
| client ITL pooled p50 | 0.0450 s | 0.0461 s | **−0.0010 s** | — |
| client ITL pooled p95 | 0.0642 s | 0.0745 s | — | **−0.0103 s** |

**ITL is within tolerance on the nose** (p50 Δ −1.0 ms ≪ ±10 ms; p95 Δ −10.3 ms within ±20 ms).
The **TTFT tail is 3.3 s apart** — this is the enumerated arrival-process difference, and it is
fully explained by queueing, proven below.

### 2b. Root cause of the TTFT tail: Poisson clustering on a single slot (`-np 1`)

The engine has one slot. Under open-loop Poisson arrivals, requests occasionally arrive while the
previous one is still being served → they **queue**, and inferbench's `scheduled_send_ts` basis
**correctly counts that queue wait against TTFT** (this is coordinated-omission safety working as
designed — the exact behavior IB-T002's CO review hardened). The sequential reference client
structurally **cannot** queue: it holds the next send until the current response completes, so it
never observes the queue and its tail stays flat.

Per-request evidence (`inferbench-run/events.jsonl`, reconstructed in §-analysis): every TTFT spike
coincides with the previous request still being in service at this request's scheduled send time
("busy overlap" > 0). Requests 20–23 form a Poisson burst (send gaps 0.45/0.39/0.54 s) that
cascaded a queue → TTFT 2.08 / 3.84 / 5.75 / 4.16 s. Splitting inferbench's own requests by whether
the engine was idle or busy at schedule time:

| inferbench subset | n | TTFT p50 | TTFT p95 | TTFT max |
|---|---|---|---|---|
| **non-queued** (engine idle at scheduled send) | 16 | **0.541 s** | **0.744 s** | 1.068 s |
| **queued** (engine busy at scheduled send) | 9 | 1.870 s | 5.754 s | 5.754 s |
| reference client (sequential — never queues) | 25 | 0.438 s | 0.884 s | 1.003 s |

**The non-queued inferbench distribution matches the sequential reference** (p50 0.541 vs 0.438 s,
Δ +103 ms within the ±150 ms prompt-length tolerance; p95 0.744 vs 0.884 s). The entire tail lives
in the queued subset, which the reference tool cannot produce by construction. **Delta explained.**

The residual +103 ms non-queued p50 gap is the secondary enumerated difference (row 5): the two
tools send different prompts, so their prompt-token counts and hence `prompt_ms` differ; the server
`prompt_ms` itself ranges 267–996 ms across requests, so a 103 ms shift between two different prompt
sets is well inside prompt-length variance, not a measurement discrepancy.

---

## 3. Comparison B — client-measured vs server-reported timings (paired, same request)

The strongest cross-check: for each reference-client request, its client-measured TTFT/ITL against
the `timings` object llama-server attached to *that same response*. n = 25 (warm-up excluded);
all 30 records carried `timings`.

| Paired statistic | p50 | p95 | range | Interpretation |
|---|---|---|---|---|
| client TTFT − server `prompt_ms` | **+11.4 ms** | +33.4 ms | [+7.1, +41.0] ms | client ≈ server prompt-eval + **small positive** transport/dispatch/first-decode delta — exactly the expected relationship (client ≈ server + overhead) |
| client TTFT − (`prompt_ms` + 1 avg-decode-token) | −35.7 ms | −15.1 ms | [−57, −11] ms | negative → first-token decode is *faster* than the average decode token; the true overhead sits between this and the row above (first decode ≈ 11–52 ms, transport single-digit ms) |
| client ITL pooled p50 | **46.1 ms** | 74.5 ms | — | inferbench/refclient inter-token gap |
| server decode/token p50 (`predicted_ms/predicted_n`) | **46.1 ms** | 73.6 ms | — | server's own per-token decode rate |

**Client ITL and server-reported decode/token are near-identical** (p50 46.1 ms vs 46.1 ms; p95
74.5 vs 73.6 ms) — the client-side ITL measurement reproduces the engine's own self-reported decode
cadence to within ~1 ms. **Client TTFT is server prompt-eval time plus a bounded +11 ms** (p50)
transport/first-decode overhead — the client-measured latency is always ≥ the server-internal
compute time, never below it, and the gap is small and positive as a correct client measurement
must be. Both paired deltas are **within the declared tolerance.**

---

## 4. Comparison C — CPU contention: shared cores vs taskset separation

The single most important threat to a single-host calibration is client/server CPU contention. I
measured it. Two reference-client passes, identical workload/seed:
- **shared:** llama-server `-t 4` (all 4 cores), client unpinned (competes for the same cores).
- **taskset-pinned:** llama-server `-t 3` pinned to cores 0–2, client pinned to core 3 (isolated).

| Pass | TTFT p50 | TTFT p95 | TTFT max | wall |
|---|---|---|---|---|
| shared (unpinned, server -t 4) | 0.438 s | 0.884 s | 1.003 s | 51.4 s |
| taskset-pinned (server -t 3 / client core 3) | 0.530 s | 0.766 s | 0.769 s | 54.7 s |
| Δ (pinned − shared) | **+0.092 s** | **−0.118 s** | **−0.234 s** | — |

**Two effects, honestly separated (this comparison changes two variables — thread count AND
pinning — a documented confound):**
- **Median went *up* +92 ms** — dominated by the **thread-count** change (the server lost a compute
  thread, 4→3), not by contention. This is a confound: to give the client a dedicated core on a
  4-core box, the server must drop to 3 threads. So the +92 ms is mostly lost server throughput,
  not contention relief.
- **Tail *tightened*** — p95 −118 ms, **max −234 ms**. Isolating the client onto its own core
  removed the worst-case client/server core-contention stalls. This is the genuine contention
  signal: **on this host, client/server CPU contention inflates the TTFT tail by roughly
  120–230 ms**, while leaving the median dominated by raw engine compute.

**Consequence for the calibration:** the shared-core numbers in §2–§3 carry a tail inflation of
~120–230 ms from contention. That does not change any conclusion — §2's tail is dominated by
*queueing* (seconds), and §3's paired deltas are p50/loopback-transport scale (ms) where the
contention effect is a small additive tail term, not a systematic bias. The median-level agreements
hold under both configurations.

---

## 5. llama-bench model-level anchor (different measurement point — comparability caveat)

`llama-bench -m qwen2.5-1.5b-instruct-q4_k_m -p 64 -n 32 -t 4 -r 5`:

| llama-bench metric | value | — matched against — | value |
|---|---|---|---|
| token generation (tg32) | **21.08 t/s** ±1.50 | server `predicted_per_second` p50 | 21.69 t/s |
| — | — | inferbench client-derived decode (1 / median ITL) | 21.71 t/s |
| prompt processing (pp64) | 122.15 t/s ±5.36 | server `prompt_per_second` p50 | 107.5 t/s |

**Comparability rule (methodology rule 10):** llama-bench is a **different measurement point** —
pure model compute with **no HTTP server, no SSE framing, no scheduler, no prompt caching**. It is
**not** directly comparable to a client-measured serving latency and no equality is claimed. It is
used only as an independent **order-of-magnitude anchor**:
- **Decode throughput agrees across three fully independent measurement points** — llama-bench
  (21.08 t/s), the server's own `timings` (21.69 t/s), and inferbench's client-side ITL
  (21.71 t/s) — all within ~3%. Three tools that share no measurement code converge.
- **Prompt throughput differs ~12%** (122 vs 107 t/s) — **expected and explained**: the serving
  runs hit prompt-cache reuse (`cache_n > 0` on the shared system-prompt prefix) and process short,
  variable non-cached suffixes at higher per-token overhead, whereas llama-bench processes a fixed
  uncached 64-token prompt in a tight loop. Different measurement point, same order of magnitude —
  the comparability caveat, not a discrepancy.

---

## 6. Verdict

| Comparison | Basis | Result | Within declared tolerance? |
|---|---|---|---|
| A — ITL, inferbench vs reference client | client vs client, same engine | p50 Δ −1.0 ms, p95 Δ −10.3 ms | **Yes** |
| A — TTFT non-queued, inferbench vs reference | client vs client, arrival diff removed | p50 0.541 vs 0.438 s (Δ +103 ms) | **Yes** (±150 ms) |
| A — TTFT whole distribution | open-loop vs sequential | tail +3.3 s | **N/A — explained** (queueing on single slot; CO-safety working as designed) |
| B — client TTFT vs server `prompt_ms` | paired, same request | +11.4 ms p50, bounded positive | **Yes** (< 100 ms, positive) |
| B — client ITL vs server decode/token | paired, same request | p50 46.1 vs 46.1 ms | **Yes** (±10 ms) |
| C — CPU contention (shared vs pinned) | measured | tail +120–230 ms from contention; median confounded by thread count | Measured & disclosed |
| Anchor — llama-bench decode t/s | model-level | 21.08 vs 21.69 vs 21.71 t/s (3 independent points, ~3%) | Order-of-magnitude anchor |

**IB-T007 stop condition (ADR-0004 §4, `docs/tasks.md`): met.** Every delta is either within the
tolerance declared before comparing, or attributed to an enumerated difference (§1) whose effect is
isolated and quantified. The same-basis comparisons (client ITL vs client ITL; client vs
server-reported decode; paired client TTFT vs server prompt-eval) agree to single-digit ms. The one
large delta (the TTFT tail) is the expected, correct consequence of inferbench measuring queue delay
that a sequential reference tool cannot see — evidence *for* the generator's coordinated-omission
safety, not against its accuracy. No comparative claim is published (ADR-0004 scope guard); this
speaks only to generator trustworthiness.

---

## 7. Threats to validity

- **Single host, CPU-only, loopback.** Network RTT ≈ 0, so §3's +11 ms client−server delta is
  timer/scheduler/SSE-framing cost on this box, not a network model. Cross-host behavior is not
  characterized here.
- **Client/server CPU contention (measured, §4).** On this 4-core box the shared-core config
  inflates the TTFT tail ~120–230 ms vs an isolated client. All §2–§3 numbers use the shared
  config, so they carry that tail term; it does not affect the median-level agreements or the
  queueing conclusion. The taskset separation cannot be done without also dropping a server thread
  on a 4-core box (confound, disclosed) — a machine with ≥ 5 cores would separate the two variables.
- **Small n (25 kept/arm, 1 repetition).** This is calibration diagnostics, not a published
  benchmark; nearest-rank percentiles, no bootstrap CIs. The conclusions rest on mechanism
  (queueing, paired server timings) and cross-tool convergence, not on tight tail estimates. A
  larger n would sharpen the tail percentiles but cannot change the mechanism.
- **Different workloads between inferbench and the reference client.** Deliberate — an independent
  tool with its own prompt generator is a stronger independence check than replaying identical
  bytes, but it introduces the ~100 ms non-queued p50 prompt-length gap (§2b), which is inside the
  declared prompt-length tolerance and inside the server's own `prompt_ms` spread.
- **Prompt caching in the serving path.** The shared system-prompt prefix is cached (`cache_n > 0`),
  lowering `prompt_ms` for the cached portion vs llama-bench's uncached prompt — the §5 prompt-rate
  gap. It does not affect decode-rate agreement.
- **`refclient.py` "dispatch instant" vs inferbench "scheduled send".** Because the reference client
  is sequential, its dispatch instant and a scheduled-send instant coincide (there is no schedule to
  slip against). The comparison is therefore against inferbench's *non-queued* requests (§2b), where
  scheduled-send ≈ dispatch, keeping the measurement points aligned.

## 8. Unexplained anomalies

**None.** Every delta observed is accounted for by an enumerated difference in §1: the TTFT tail by
the arrival-process row (queueing on a single slot), the non-queued p50 gap by the
prompt-construction row, the client−server positive delta by the measurement-point row (server basis
excludes transport + first decode), and the llama-bench prompt-rate gap by the different-measurement-
point comparability caveat. Searched for and found no delta without an enumerated cause.

## 9. Files

- `refclient.py` — independent reference client (shares no code with inferbench's Go client).
- `inferbench-workload.json`, `inferbench-facts.json` — inferbench run inputs.
- `inferbench-run/` — inferbench raw events, manifest, run log, reference.json.
- `refclient-shared.json`, `refclient-pinned.json` — reference-client pass outputs (per-request
  records incl. server `timings`).
- `llama-bench.json` — model-level anchor.
- `compare_calibration.py`, `comparison-summary.json` — the numeric basis for this report.
- `quiet-box.txt`, `*.log` — quiet-box snapshots and per-pass logs.
- `kit-validate.log` — contracts kit validation of the emitted inferbench artifacts.
