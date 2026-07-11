#!/usr/bin/env python3
"""IB-T010 E2b: G5 degradation stat (accepted-request TTFT p95 at ~5x vs the
SAME-config ~1x capacity-boundary baseline), bootstrapped exactly like E2's
e2-degradation.json (B=1000, seed 20260710, numpy default_rng, percentile
bootstrap over pooled post-warm-up accepted-request TTFT samples; ratio CI
from paired resample draws -- i.e. each of the B iterations independently
resamples BOTH pools with replacement and recomputes the ratio for that
iteration, so the reported CI is a joint CI over both pools' sampling
variability, not a CI of one pool alone).

Uses the analysis package's own Run loader + apply_warmup so the pooled TTFT
samples are byte-identical to what `inferbench_analysis analyze` used for
the published p95 point estimates (docs/evidence/ib-t010/e2b-analyze-*.log).

Usage: compute_degradation_e2b.py
"""
import json
import sys

import numpy as np

sys.path.insert(0, "/home/user/inferbench/analysis/src")
from inferbench_analysis.contracts import Bundle
from inferbench_analysis.events import load_run
from inferbench_analysis.warmup import apply_warmup

BUNDLE = Bundle("/home/user/serving-contracts")
OUT = "docs/evidence/ib-t010"

def pooled_ttft(run_dirs):
    runs = [load_run(d, BUNDLE) for d in run_dirs]
    measured, _ = apply_warmup(runs)
    return np.asarray(
        [e.ttft_seconds for e in measured if e.ttft_seconds is not None],
        dtype=float,
    )

baseline = pooled_ttft([f"{OUT}/e2b-baseline/rep-{i}" for i in (1, 2, 3)])
overload = pooled_ttft([f"{OUT}/e2b-overload/rep-{i}" for i in (1, 2, 3)])

baseline_p95 = float(np.quantile(baseline, 0.95, method="linear"))
overload_p95 = float(np.quantile(overload, 0.95, method="linear"))
degradation_pct = (overload_p95 - baseline_p95) / baseline_p95 * 100.0

B = 1000
rng = np.random.default_rng(20260710)
reps = np.empty(B, dtype=float)
base_p95_reps = np.empty(B, dtype=float)
over_p95_reps = np.empty(B, dtype=float)
for b in range(B):
    bs = baseline[rng.integers(0, baseline.size, size=baseline.size)]
    ov = overload[rng.integers(0, overload.size, size=overload.size)]
    bp = np.quantile(bs, 0.95, method="linear")
    op = np.quantile(ov, 0.95, method="linear")
    base_p95_reps[b] = bp
    over_p95_reps[b] = op
    reps[b] = (op - bp) / bp * 100.0

ci_lo, ci_hi = np.quantile(reps, [0.025, 0.975])
base_ci = np.quantile(base_p95_reps, [0.025, 0.975])
over_ci = np.quantile(over_p95_reps, [0.025, 0.975])
p_le_20 = float(np.mean(reps <= 20.0))

result = {
    "baseline_n": int(baseline.size),
    "overload_n": int(overload.size),
    "baseline_p95_s": baseline_p95,
    "baseline_p95_ci95": [float(base_ci[0]), float(base_ci[1])],
    "sane5x_p95_s": overload_p95,
    "sane5x_p95_ci95": [float(over_ci[0]), float(over_ci[1])],
    "degradation_pct": degradation_pct,
    "degradation_ci95_pct": [float(ci_lo), float(ci_hi)],
    "p_degradation_le_20pct": p_le_20,
    "method": "bootstrap (B=1000, seed 20260710, numpy default_rng) over pooled "
    "post-warm-up accepted-request TTFT samples; ratio CI from paired resample "
    "draws (each of the B iterations resamples BOTH pools independently and "
    "recomputes the ratio for that iteration) -- identical method to E2's "
    "e2-degradation.json",
}
print(json.dumps(result, indent=2))
with open(f"{OUT}/e2b-degradation.json", "w") as f:
    json.dump(result, f, indent=2)
    f.write("\n")
