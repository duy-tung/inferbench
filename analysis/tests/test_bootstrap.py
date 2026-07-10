"""Bootstrap CI checks (ADR-0002 §2): reproducibility, ordering, and
coverage sanity on a distribution with a known percentile."""

import math

import numpy as np
import pytest

from inferbench_analysis import BootstrapParams, bootstrap_ci, pooled_table


class TestMechanics:
    def test_reproducible_with_seed(self):
        rng = np.random.default_rng(7)
        samples = rng.lognormal(mean=-2.0, sigma=0.5, size=500)
        p = BootstrapParams(resamples=200, seed=123)
        a = bootstrap_ci(samples, [50, 99], p)
        b = bootstrap_ci(samples, [50, 99], p)
        assert a == b
        c = bootstrap_ci(samples, [50, 99], BootstrapParams(resamples=200, seed=124))
        assert a != c

    def test_interval_brackets_point_estimate(self):
        rng = np.random.default_rng(11)
        samples = rng.lognormal(mean=-2.0, sigma=0.5, size=800)
        t = pooled_table({"r": samples}, bootstrap=BootstrapParams(resamples=400))
        for label, point in (("p50", t.p50), ("p90", t.p90), ("p99", t.p99)):
            lo, hi = t.ci[label]
            assert lo <= point <= hi, f"{label}: [{lo}, {hi}] vs {point}"
            assert lo < hi

    def test_table_without_bootstrap_has_no_ci(self):
        t = pooled_table({"r": [1.0, 2.0, 3.0]}, bootstrap=None)
        assert t.ci is None


class TestCoverage:
    """95% percentile-bootstrap CI for the MEDIAN of Exp(1) (true value
    ln 2) should cover the truth in roughly 95% of trials. The percentile
    bootstrap is known to undercover slightly on small samples, so the
    acceptance band is [0.85, 1.0] — a regression to e.g. averaged or
    mis-ordered intervals lands far below it."""

    def test_p50_coverage_on_exponential(self):
        true_median = math.log(2.0)
        rng = np.random.default_rng(20260710)
        trials, n = 150, 300
        params = BootstrapParams(resamples=300, seed=99)
        covered = 0
        for _ in range(trials):
            samples = rng.exponential(scale=1.0, size=n)
            (lo, hi), = bootstrap_ci(samples, [50], params)
            if lo <= true_median <= hi:
                covered += 1
        coverage = covered / trials
        assert 0.85 <= coverage <= 1.0, f"coverage {coverage}"
        # and it must not be trivially wide: median CI half-width for
        # Exp(1), n=300 is well under 0.25
        samples = rng.exponential(scale=1.0, size=n)
        (lo, hi), = bootstrap_ci(samples, [50], params)
        assert (hi - lo) < 0.5

    def test_p90_coverage_on_exponential(self):
        true_p90 = -math.log(0.1)  # inverse CDF of Exp(1) at 0.9
        rng = np.random.default_rng(42)
        trials, n = 120, 400
        params = BootstrapParams(resamples=300, seed=7)
        covered = 0
        for _ in range(trials):
            samples = rng.exponential(scale=1.0, size=n)
            (lo, hi), = bootstrap_ci(samples, [90], params)
            if lo <= true_p90 <= hi:
                covered += 1
        assert covered / trials >= 0.85
