# ADR-0002 — Statistics choices: pooled percentiles, bootstrap CIs, warm-up exclusion, knee detection

**Status:** Accepted (user review passed at the Wave-1 exit review, 2026-07-10) · **Date:** 2026-07-10 · **Owner task:** IB-T005 (parameters finalized there)

## Context

The analysis half turns raw per-request events into published numbers. The statistics must be
(a) correct for heavy-tailed latency distributions, (b) honest across repetitions, and
(c) resistant to the anti-patterns in `experiments.md` rule 12. Contract 3's
`benchmark-result.schema.json` already mandates pooled-percentile tables and a validity block;
this ADR fixes how we compute them.

## Decision

1. **Pooled percentiles.** For a sweep point with ≥3 repetitions, all post-warm-up per-request
   samples from all repetitions are pooled into one dataset; percentiles (p50/p90/p95/p99, plus
   max where meaningful) are computed on the pool. **Averaging percentiles across runs is
   forbidden and enforced in code**: the public API exposes no per-run-percentile aggregation
   path, and a known-answer test (constructed so pooling and averaging differ) fails if averaging
   is reintroduced. Cross-run *dispersion* is reported as median ± range of per-run summaries —
   alongside, never instead of, the pooled table.
2. **Bootstrap confidence intervals.** Percentile CIs via nonparametric bootstrap (resample the
   pooled dataset with replacement, recompute the percentile, take the percentile interval).
   Working defaults: ~1000 resamples, 95% interval; exact parameters are finalized in IB-T005
   with known-answer tests against synthetic distributions and recorded in code + this ADR's
   changelog. Rationale: latency distributions are heavy-tailed and multi-modal; no parametric
   form is defensible.
3. **Warm-up exclusion.** The first ≥50 requests or 60–120 s (whichever the run's declared policy
   states) are excluded before any statistic is computed. The exclusion policy and the run's
   cache state (cold/warm) come from the manifest; the analysis refuses events without a
   declared policy.
4. **Knee detection.** The saturation knee is estimated from sweep data (≥6 points, 10%→120% of
   estimated capacity) as the offered-rate point where measured latency departs from its
   low-rate plateau — implemented as a deviation-threshold method (e.g. p99 exceeding k× the
   plateau median), with a curvature-based method (kneedle-style) as a cross-check. Exact method
   and parameters are finalized in IB-T005 against synthetic sweeps with analytically placed
   knees. The knee is reported as an estimate with its uncertainty; **no extrapolation past it**.
5. **Goodput coupling.** Goodput@SLO, shed rate, and stall rate are computed in a single pass and
   returned as one value object, so no caller can obtain goodput without the adjacent rates.
6. **Failure semantics.** The loader refuses manifest-less or schema-invalid events; statistics
   never silently drop records — every exclusion (warm-up, invalid) is counted and reported in
   the validity block.

## Alternatives considered

- **Averaging per-run percentiles**: forbidden — it is statistically meaningless for tails and is
  the exact anti-pattern rule 5 targets.
- **Parametric CIs (normal/log-normal assumptions)**: rejected — indefensible for heavy-tailed
  TTFT/ITL data.
- **t-digest/HDR sketches for percentiles**: unnecessary — per-run datasets are small enough for
  exact percentiles on pooled raw data; exactness beats sketch error at our scale.
- **ML-based knee detection**: overkill; a threshold + curvature cross-check is explainable in a
  report.

## Consequences

- Raw per-request events must be retained per run (they are, as JSONL — Contract 3), since
  pooling needs raw data, not summaries.
- Known-answer tests (synthetic distributions with analytic percentiles/CIs/knees) become the
  acceptance basis for IB-T005 (see `testing.md` layer 2).
- Any future change to bootstrap parameters or knee method is a documented amendment here, since
  it affects comparability of results across report versions.
