"""Run-set assembly, comparability refusals, and schema-valid emission."""

import json

import pytest

from inferbench_analysis import (
    AnalysisConfig,
    ComparabilityError,
    CostInputs,
    CostUnavailable,
    ResultNotExpressibleError,
    analyze_run_set,
)
from conftest import make_event, make_manifest, make_run, make_slo


def _run(run_id="syn-A", n=50, **manifest_kw):
    events = [
        make_event(
            i,
            run_id=run_id,
            sched=i * 0.2,
            ttft=0.05 + (i % 10) * 0.01,
            e2e=0.3 + (i % 10) * 0.02,
            itl_series=(0.02, 0.03, 0.025),
        )
        for i in range(n)
    ]
    return make_run(events, make_manifest(run_id, **manifest_kw), run_id=run_id)


class TestAssembly:
    def test_full_result_shape(self):
        res = analyze_run_set(
            [_run()],
            AnalysisConfig(slo=make_slo(), bootstrap=None),
            result_id="asm",
        )
        assert res.pooled_event_count == 50
        assert res.run_count == 1
        assert res.latency.itl_seconds.n == 150  # 50 events x 3 gaps pooled
        assert res.latency.max_stall_seconds.n == 50
        assert res.knee is None
        assert isinstance(res.cost, CostUnavailable)
        # auto-threats: <3 runs, no sweep, no cost profile, warm-up none
        joined = " ".join(res.threats_to_validity)
        assert "run_count=1" in joined
        assert "knee_estimate is null" in joined
        assert "cost is null" in joined
        assert "warm-up policy 'none'" in joined
        # empty anomalies is the explicit "looked, found none" claim
        assert res.unexplained_anomalies == ()

    def test_throughput_known_answer(self):
        # 50 events at 0.2s spacing, each e2e 0.3..0.48; window =
        # (49*0.2 + last e2e) - 0
        res = analyze_run_set(
            [_run()],
            AnalysisConfig(slo=make_slo(), bootstrap=None),
            result_id="thr",
        )
        expected_window = 49 * 0.2 + (0.3 + 9 * 0.02)
        assert res.throughput.window_seconds == pytest.approx(expected_window)
        assert res.throughput.requests_per_second == pytest.approx(
            50 / expected_window
        )
        assert res.throughput.total_output_tokens == 500

    def test_dispersion_reported_alongside_for_multi_run(self):
        res = analyze_run_set(
            [_run("syn-A"), _run("syn-B")],
            AnalysisConfig(slo=make_slo(), bootstrap=None),
            result_id="disp",
        )
        assert res.run_count == 2
        assert "ttft_seconds_p50" in res.dispersion
        d = res.dispersion["ttft_seconds_p50"]
        assert d.min <= d.median <= d.max


class TestComparabilityGuard:
    def test_pooling_across_differing_engine_flags_refused(self):
        a = _run("syn-A")
        b = _run("syn-B", engine_flags={"max_num_seqs": 8})
        with pytest.raises(ComparabilityError, match="engine"):
            analyze_run_set(
                [a, b],
                AnalysisConfig(slo=make_slo(), bootstrap=None),
                result_id="cmp",
            )

    def test_duplicate_run_id_refused(self):
        with pytest.raises(ResultNotExpressibleError, match="duplicate run_id"):
            analyze_run_set(
                [_run("syn-A"), _run("syn-A")],
                AnalysisConfig(slo=make_slo(), bootstrap=None),
                result_id="dup",
            )


class TestEmission:
    def test_emitted_document_is_schema_valid(self, bundle, tmp_path):
        prov = {
            "basis": "source-reported",
            "as_of": "2026-07-01",
            "source": "vendor pricing page",
        }
        cost_inputs = CostInputs(
            profile={
                "profile_id": "test-cloud",
                "version": "1.0.0",
                "currency": "USD",
                "rates": [
                    {
                        "hardware_profile_ref": {"id": "cpu-dev"},
                        "pricing_model": "on-demand",
                        "usd_per_hour": {"value": 0.5, "provenance": prov},
                    }
                ],
            },
            hardware_profile_id="cpu-dev",
        )
        res = analyze_run_set(
            [_run()],
            AnalysisConfig(slo=make_slo(), bootstrap=None, cost_inputs=cost_inputs),
            result_id="emit",
        )
        doc = res.to_benchmark_result_dict(bundle)  # self-validates
        assert doc["pooled_percentiles"]["method"] == "pooled-raw-events"
        assert doc["goodput"]["shed_rate"] == 0.0
        assert doc["goodput"]["stall_rate"] == 0.0
        assert doc["knee_estimate"] is None
        assert doc["cost"]["per_successful_request_usd"] > 0
        # write + re-read + validate through the kit-equivalent validator
        out = tmp_path / "emit.benchmark-result.json"
        res.write_benchmark_result(out, bundle)
        bundle.validate("benchmark-result", json.loads(out.read_text()))

    def test_null_cost_and_null_knee_emitted_with_threats(self, bundle):
        res = analyze_run_set(
            [_run()],
            AnalysisConfig(slo=make_slo(), bootstrap=None),
            result_id="nulls",
        )
        doc = res.to_benchmark_result_dict(bundle)
        assert doc["cost"] is None
        assert doc["knee_estimate"] is None
        joined = " ".join(doc["validity"]["threats_to_validity"])
        assert "cost is null" in joined
        assert "knee_estimate is null" in joined
