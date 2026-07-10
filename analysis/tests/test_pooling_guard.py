"""Pooled-vs-averaged guard (experiments.md rule 5, ADR-0002 §1).

These tests FAIL if percentile averaging is ever reintroduced: the data is
constructed so that pooling and averaging give different answers, and the
only construction path for percentile tables is raw-sample pooling.
"""

import statistics

import pytest

import inferbench_analysis
import inferbench_analysis.percentiles as pctmod
from inferbench_analysis import (
    AnalysisConfig,
    PercentileTable,
    PoolingGuardError,
    analyze_run_set,
    pooled_table,
)
from conftest import make_event, make_manifest, make_run, make_slo


class TestPooledNotAveraged:
    def test_pooling_and_averaging_differ_and_pooled_wins(self):
        # run A: 100 samples of 1.0; run B: 100 samples of 3.0.
        # Averaging per-run p99s gives (1.0 + 3.0)/2 = 2.0 — a value NO
        # request ever experienced. Pooled p99 of the 200 samples is 3.0.
        by_run = {"A": [1.0] * 100, "B": [3.0] * 100}
        per_run_p99 = [1.0, 3.0]
        averaged = statistics.mean(per_run_p99)
        t = pooled_table(by_run, bootstrap=None)
        assert t.p99 == pytest.approx(3.0)
        assert t.p99 != pytest.approx(averaged)
        assert t.p50 == pytest.approx(2.0)  # pooled median genuinely straddles

    def test_analyzer_output_is_pooled(self):
        # same construction end-to-end through analyze_run_set: two
        # repetitions in one manifest, fast rep + slow rep
        events = [
            make_event(i, rep=1, sched=i * 0.1, ttft=0.010, e2e=0.5)
            for i in range(100)
        ] + [
            make_event(i, rep=2, sched=100 + i * 0.1, ttft=0.030, e2e=0.5)
            for i in range(100)
        ]
        run = make_run(events, make_manifest(repetitions=2))
        res = analyze_run_set(
            [run],
            AnalysisConfig(slo=make_slo(), bootstrap=None),
            result_id="guard",
        )
        # pooled p99 = 0.030 (the slow rep dominates the tail);
        # averaged per-rep p99 would be 0.020 — assert it is NOT that
        assert res.latency.ttft_seconds.p99 == pytest.approx(0.030)
        assert res.latency.ttft_seconds.p99 != pytest.approx(0.020)
        assert res.latency.ttft_seconds.n == 200

    def test_emitted_method_is_the_schema_const(self, bundle):
        events = [make_event(i, sched=i * 0.1) for i in range(20)]
        res = analyze_run_set(
            [make_run(events)],
            AnalysisConfig(slo=make_slo(), bootstrap=None),
            result_id="guard-method",
        )
        doc = res.to_benchmark_result_dict(bundle)
        assert doc["pooled_percentiles"]["method"] == "pooled-raw-events"


class TestStructuralGuard:
    def test_table_cannot_be_built_from_precomputed_percentiles(self):
        # the averaging path: someone computes per-run percentiles and tries
        # to construct a table from their means — must be impossible
        with pytest.raises(PoolingGuardError):
            PercentileTable(
                n=200, p50=2.0, p90=2.0, p95=2.0, p99=2.0, max=3.0, mean=2.0
            )

    def test_no_public_averaging_api(self):
        names = [n for n in dir(inferbench_analysis) if "average" in n.lower()]
        names += [n for n in dir(pctmod) if "average" in n.lower()]
        assert names == []

    def test_table_has_no_merge_or_combine(self):
        for attr in ("merge", "combine", "__add__"):
            assert getattr(PercentileTable, attr, None) is None

    def test_method_field_is_locked(self):
        with pytest.raises(PoolingGuardError):
            PercentileTable(
                n=1,
                p50=1.0,
                p90=1.0,
                p95=1.0,
                p99=1.0,
                max=1.0,
                mean=1.0,
                method="averaged-per-run",
                _guard=pctmod._GUARD,
            )
