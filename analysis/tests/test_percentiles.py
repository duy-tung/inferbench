"""Known-answer tests: exact percentiles on constructed data (ADR-0002 §1)."""

import pytest

from inferbench_analysis import (
    EmptyPoolError,
    PercentileTable,
    PoolingGuardError,
    per_run_dispersion,
    pooled_table,
)


class TestKnownAnswers:
    def test_percentiles_of_1_to_100(self):
        # linear interpolation (Hyndman-Fan 7): q-th percentile of 1..100
        # is 1 + q/100 * 99
        t = pooled_table({"r1": list(range(1, 101))}, bootstrap=None)
        assert t.n == 100
        assert t.p50 == pytest.approx(50.5)
        assert t.p90 == pytest.approx(90.1)
        assert t.p95 == pytest.approx(95.05)
        assert t.p99 == pytest.approx(99.01)
        assert t.max == 100.0
        assert t.mean == pytest.approx(50.5)
        assert t.p999 is None  # n < 1000: p999 would be false precision

    def test_p999_reported_at_1000_samples(self):
        t = pooled_table({"r1": list(range(1, 1001))}, bootstrap=None)
        assert t.p999 == pytest.approx(1 + 0.999 * 999)

    def test_single_sample(self):
        t = pooled_table({"r1": [0.25]}, bootstrap=None)
        assert t.n == 1
        assert t.p50 == t.p99 == t.max == t.mean == 0.25

    def test_pool_is_concatenation_of_runs(self):
        # identical data split differently across runs => identical table
        a = pooled_table({"r1": [1, 2, 3, 4], "r2": [5, 6, 7, 8]}, bootstrap=None)
        b = pooled_table({"x": [1, 2, 3, 4, 5, 6, 7, 8]}, bootstrap=None)
        assert (a.n, a.p50, a.p90, a.p99) == (b.n, b.p50, b.p90, b.p99)


class TestRefusals:
    def test_empty_pool_refused(self):
        with pytest.raises(EmptyPoolError):
            pooled_table({}, bootstrap=None)
        with pytest.raises(EmptyPoolError):
            pooled_table({"r1": []}, bootstrap=None)

    def test_negative_and_nonfinite_refused(self):
        with pytest.raises(EmptyPoolError):
            pooled_table({"r1": [1.0, -0.1]}, bootstrap=None)
        with pytest.raises(EmptyPoolError):
            pooled_table({"r1": [1.0, float("nan")]}, bootstrap=None)


class TestDispersion:
    def test_median_and_range_of_per_run_summaries(self):
        d = per_run_dispersion(
            {"r1": list(range(1, 11)), "r2": list(range(11, 21))}, "p50"
        )
        assert d.per_run["r1"] == pytest.approx(5.5)
        assert d.per_run["r2"] == pytest.approx(15.5)
        assert d.median == pytest.approx(10.5)
        assert (d.min, d.max) == (pytest.approx(5.5), pytest.approx(15.5))

    def test_dispersion_is_not_a_percentile_table(self):
        d = per_run_dispersion({"r1": [1.0, 2.0]}, "p50")
        assert not isinstance(d, PercentileTable)
