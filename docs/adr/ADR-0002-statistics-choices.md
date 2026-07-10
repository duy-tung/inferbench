# ADR-0002 — Statistics choices: pooled percentiles, bootstrap CIs, warm-up exclusion, knee detection

**Status:** Accepted (user review passed at the Wave-1 exit review, 2026-07-10); numeric
parameters finalized by IB-T005 — see the changelog at the bottom · **Date:** 2026-07-10 ·
**Owner task:** IB-T005 (parameters finalized there)

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

## Changelog

### 2026-07-10 — IB-T005 finalized parameters (implementation: `analysis/` package)

Fixed by the known-answer test suite (`analysis/tests/`, 70 tests green; evidence in
`implementation-notes.md`):

1. **Percentile definition:** linear interpolation between order statistics (Hyndman–Fan
   type 7, `numpy.quantile(..., method="linear")`), computed on the pooled raw samples.
   `p999` is reported only when the pool has ≥1000 samples (below that it restates the max
   with false precision). Structural pooling guard: `PercentileTable` is only constructible
   from raw pooled samples (`pooled_table()`); direct construction — the averaging path —
   raises `PoolingGuardError`, and the known-answer guard test (pooling ≠ averaging by
   construction) pins the pooled answer.
2. **Bootstrap CIs:** nonparametric percentile bootstrap, **B = 1000 resamples, 95% interval**
   (2.5th/97.5th empirical percentiles of the replicates), seeded generator (**default seed
   20260710**) so intervals are reproducible run-to-run. Coverage verified on Exp(1) with
   analytically known p50 (= ln 2) and p90 (= ln 10): measured coverage **0.913** (p50,
   150 trials, n=300) and **0.958** (p90, 120 trials, n=400) for the nominal 95% interval,
   inside the [0.85, 1.0] acceptance band (measured 2026-07-10; percentile bootstrap is known
   to undercover slightly at small n — the band catches regressions, not decoration). CIs are
   report-surface data — the pinned benchmark-result percentileTable has no CI fields.
3. **Warm-up:** as decided above; policy read ONLY from the run manifest (`discard-requests` /
   `discard-duration` / `none`), applied per repetition in `scheduled_send_ts` order; every
   exclusion counted into `validity.warm_up_handling`. A policy that excludes every event is a
   typed refusal. Policy `none` always produces a threats-to-validity entry.
4. **Knee detection (finalized method):** plateau-departure threshold — plateau = median of
   the signal over the lowest ⌊n/3⌋ (≥2) sweep rates; knee = first rate whose signal exceeds
   **1.5× plateau** and stays above it for every higher rate (sustained departure; a single
   noisy spike is not a knee). Kneedle-style max-curvature cross-check on the normalized curve;
   agreement within one sweep point → confidence 0.8, disagreement → 0.5. **Honest
   limitations, emitted in the method string:** resolution is bounded by sweep-point spacing
   (the true knee lies in (previous point, reported point]); assumes a plateau-then-degrade
   shape; the 1.5× factor is a declared judgment call, not a statistical test; a knee at the
   highest swept rate is reported as NOT bracketed with confidence capped at 0.5 and a
   mandatory threat entry. <6 sweep points is a typed refusal (rule 3). No extrapolation past
   the knee anywhere.
5. **Error/shed gating (CO re-review requirement, new in IB-T005):** latency percentiles are
   **withheld** when the measured window's combined error+shed rate exceeds the **declared**
   gate threshold (default **0.05**; always echoed in output so a permissive declaration is
   visible). A 100%-timeout run is a VALID run — its throughput, error/shed accounting and
   goodput are reported — but its latency table is structurally absent (`WithheldLatency`
   carries the reason into the validity block; there is no percentile surface to quote).
   Deliberate cancellations are workload features and do not count toward the gate. Because the
   pinned benchmark-result schema has no null form for the required percentile tables, emission
   of a withheld-latency result file is a typed refusal (`ResultNotExpressibleError`) — never a
   schema-invalid or number-fabricating artifact (contracts observation filed in
   `implementation-notes.md`).
6. **Goodput evaluation semantics:** DistServe-style per-request SLO attainment — a request
   meets the SLO iff status `ok` AND every objective holds on the request's own signal values;
   shed/errored/canceled requests count in the offered denominator and never meet. The SLO
   MUST declare a `max_stall_seconds` upper bound (otherwise typed refusal: stall rate needs a
   threshold to exist beside goodput). Stall rate = stalled / ITL-bearing requests; zero
   streaming requests → 0.0 with a mandatory vacuousness threat.
7. **Comparability guards on pooling:** repetitions pool only when their manifests agree on
   every comparability key (rule 10 set); duplicate run_ids refuse (double-counting/
   cherry-picking guard). Cross-run dispersion is a separate type (`RunDispersion`, median ±
   range of per-run summaries) that cannot occupy a pooled-table slot.
