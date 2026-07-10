# ADR-0004 — Calibration protocol vs reference tooling

**Status:** Accepted · **Date:** 2026-07-10 · **Owner task:** IB-T007

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
