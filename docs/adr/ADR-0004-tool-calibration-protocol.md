# ADR-0004 — Calibration protocol vs reference tooling

**Status:** Accepted (user review passed at the Wave-1 exit review, 2026-07-10) · **Date:** 2026-07-10 · **Owner task:** IB-T007

## Context

If `inferbench` is the only tool that ever produced its numbers, every published claim is exposed
to "your generator is broken" — the tool-calibration risk in `risks.md`. IB-T007 cross-checks the
generator against independent reference tooling: `vllm bench serve` on GPU (behind gate G6), or a
llama.cpp-based reference on CPU as the fallback. Naive cross-tool comparison, however, is itself
an anti-pattern (methodology rule 10 forbids cross-tool comparisons *as claims*) — calibration is
the one sanctioned, carefully fenced exception, and only ever about the *tools*, never about the
target.

## Decision

1. **Two calibration tiers.**
   - *Tier 1 — mock calibration (IB-T004, CI-runnable):* the mock backend's configured TTFT/ITL/
     error rates are ground truth; measured ≈ configured within a declared tolerance. This
     validates the measurement path absolutely, without a second tool.
   - *Tier 2 — reference-tool calibration (IB-T007):* identical target, identical model/config,
     same workload shape, inferbench vs the reference tool; deltas tabulated.
2. **Enumerate differences BEFORE comparing.** The calibration report starts with a
   differences table filled in before any run: measurement points (client TTFT definition — first
   byte vs first token vs first non-empty delta), warm-up handling, arrival process (open-loop vs
   the reference tool's loop model), timer sources, token-counting method (usage chunk vs
   tokenizer estimate), connection handling. Any delta must be attributed to an enumerated
   difference or it counts as unexplained.
3. **Tolerance declared up front** in the calibration plan (per-metric, e.g. TTFT p50/p95), not
   chosen after seeing the deltas.
4. **Outcome rule:** comparison table within tolerance, or every delta explained by an enumerated
   difference. Unexplained deltas block publication of comparative claims (risk register) and are
   candidate upstream findings (`oss-opportunities.md`).
5. **Volatility guard:** reference-tool behavior is volatile — `vllm bench serve` name, flags,
   and measurement semantics are "as of 2026-07, re-verify at use time"; the report records the
   exact tool version/commit and flags used.
6. **Scope guard:** calibration results never appear as target-performance claims; they live in
   the calibration report under `docs/` and speak only to generator trustworthiness.

## Alternatives considered

- **Skip cross-tool calibration; trust mock calibration alone**: rejected — the mock validates
  timing capture but not behavior against a real engine's streaming cadence and error surfaces.
- **Compare against published third-party benchmark numbers**: rejected — uncontrolled hardware,
  workload, and versions; exactly what rule 10 forbids.

## Consequences

- IB-T007 produces comparison scripts plus a calibration report under `docs/`; the report is
  referenced by every subsequent published benchmark report as the generator's credibility
  anchor.
- The GPU variant consumes G6-gated budget; the CPU (llama.cpp-based) variant keeps M6 reachable
  without GPU spend.
- The differences table doubles as documentation of inferbench's own measurement-point
  definitions, kept aligned with Contract 2's mirror rule.

## Executed protocol (CPU variant, 2026-07-11)

The CPU fallback was executed; the GPU variant (`vllm bench serve`) stays deferred behind G6 (no
GPU). Evidence: `docs/evidence/ib-t007/calibration-reference.md` (**PASSED** — deltas within the
declared tolerance or explained). What was actually run, and how the decision points above were
realized:

1. **Reference tooling (Tier 2) = three independent surfaces**, exploiting that llama.cpp ships its
   own measurement points:
   - **llama-server `timings`** — the engine's own per-request self-reported prompt/decode timings,
     attached to the final SSE chunk under `stream_options.include_usage=true`
     (`tools/server/server-task.cpp` `to_json_oaicompat_chat_stream`). The infergate adapter strips
     these client-facing, so calibration runs **direct-to-engine** to read them. This is the primary
     reference: a *server-side* measurement of the *same request* inferbench measured client-side.
   - an **independent Python reference client** (`refclient.py`) sharing no code with inferbench's Go
     client — so agreement is not a shared-bug artifact.
   - **llama-bench** as a model-level tokens/sec anchor (different measurement point; comparability
     caveat applied per rule 10).
2. **Differences enumerated before comparing** (§1 of the report): the dominant one is the
   **arrival process** — inferbench is open-loop Poisson, the reference client is sequential; on a
   single-slot (`-np 1`) engine, open-loop arrivals queue and the sequential client never does.
3. **Tolerances declared before comparing**, per metric (client-ITL ±10/±20 ms p50/p95; non-queued
   client-TTFT ±150 ms p50; paired client-TTFT − server `prompt_ms` positive and < 100 ms; paired
   client-ITL vs server decode/token ±10 ms). Whole-distribution TTFT was declared *not directly
   comparable* (arrival-process row) — its delta is explained, not bounded.
4. **Outcome:** same-basis comparisons agree to single-digit ms — client ITL p50 46.1 ms vs the
   server's own decode/token p50 46.1 ms; paired client TTFT = server prompt-eval + 11 ms (p50)
   bounded-positive transport delta; inferbench non-queued TTFT p50 0.541 s vs the reference client's
   0.438 s (within ±150 ms). The one large delta (TTFT tail 3.3 s) is the correct consequence of
   inferbench capturing single-slot queue delay that a sequential tool cannot see — evidence *for*
   coordinated-omission safety. Decode throughput converges across all three tools (llama-bench
   21.08, server 21.69, inferbench-client 21.71 t/s; ~3%).
5. **CPU-contention threat measured** (report §4): client/server core contention inflates the TTFT
   *tail* ~120–230 ms (shared cores vs `taskset` separation); the median is dominated by engine
   compute. Confound disclosed: separating the client onto its own core on a 4-core box costs the
   server one thread.
6. **Scope guard honored:** no number is published as a target-performance claim; the report speaks
   only to generator trustworthiness. No unexplained anomalies → nothing filed to
   `oss-opportunities.md`.
