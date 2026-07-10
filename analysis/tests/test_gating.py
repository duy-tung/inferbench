"""Error/shed-rate gating (CO re-review requirement): latency percentiles
must not be quotable when the declared error+shed threshold is exceeded.
The run stays VALID; its latency table is structurally absent and the
reason lands in the validity block; contract-file emission is refused."""

import pytest

from inferbench_analysis import (
    AnalysisConfig,
    LatencyTables,
    ResultNotExpressibleError,
    WithheldLatency,
    analyze_run_set,
)
from conftest import make_event, make_manifest, make_run, make_slo


def _mixed_run(n_ok, n_error=0, n_shed=0, n_canceled=0):
    events = []
    i = 0
    for _ in range(n_ok):
        events.append(make_event(i, sched=i * 0.5, ttft=0.1, e2e=0.5))
        i += 1
    for _ in range(n_error):
        # timeout shape: no first byte ever arrived
        events.append(
            make_event(
                i, sched=i * 0.5, status="error", ttft=None, e2e=30.0,
                output_tokens=0,
            )
        )
        i += 1
    for _ in range(n_shed):
        events.append(
            make_event(i, sched=i * 0.5, status="shed", e2e=0.01, output_tokens=0)
        )
        i += 1
    for _ in range(n_canceled):
        # mid-stream cancel: a real TTFT was measured before the cancel
        events.append(
            make_event(i, sched=i * 0.5, status="canceled", ttft=0.1, e2e=0.3)
        )
        i += 1
    return make_run(events)


def _analyze(run, threshold=0.05):
    return analyze_run_set(
        [run],
        AnalysisConfig(slo=make_slo(), gate_threshold=threshold, bootstrap=None),
        result_id="gate-test",
    )


class TestGateTrips:
    def test_errors_above_threshold_withhold_latency(self):
        res = _analyze(_mixed_run(n_ok=90, n_error=10), threshold=0.05)
        assert isinstance(res.latency, WithheldLatency)
        assert res.latency.kind == "error-shed-gate"
        assert res.latency.error_rate == pytest.approx(0.10)
        # reason is in the validity block
        assert any("withheld" in t for t in res.threats_to_validity)
        assert any("0.05" in t for t in res.threats_to_validity)

    def test_gated_result_has_no_percentile_surface(self):
        res = _analyze(_mixed_run(n_ok=90, n_error=10))
        assert not hasattr(res.latency, "ttft_seconds")
        assert not isinstance(res.latency, LatencyTables)

    def test_contract_emission_refused_when_gated(self, bundle):
        res = _analyze(_mixed_run(n_ok=90, n_error=10))
        with pytest.raises(ResultNotExpressibleError, match="withheld"):
            res.to_benchmark_result_dict(bundle)

    def test_shed_counts_toward_the_gate(self):
        res = _analyze(_mixed_run(n_ok=90, n_shed=10, n_error=0))
        assert isinstance(res.latency, WithheldLatency)
        assert res.latency.shed_rate == pytest.approx(0.10)

    def test_hundred_percent_timeout_run_is_valid_but_unquotable(self):
        res = _analyze(_mixed_run(n_ok=0, n_error=50))
        # the RUN is valid: throughput/error accounting/goodput all real
        assert res.throughput.requests_per_second == 0.0
        assert res.throughput.total_requests == 50
        assert res.error_rate == 1.0
        assert res.goodput.ratio == 0.0
        assert res.goodput.shed_rate == 0.0  # errors, not sheds — visible
        # but its latency table is meaningless and absent
        assert isinstance(res.latency, WithheldLatency)


class TestGatePasses:
    def test_below_threshold_latency_present_and_errors_visible(self):
        res = _analyze(_mixed_run(n_ok=98, n_error=2), threshold=0.05)
        assert isinstance(res.latency, LatencyTables)
        assert res.error_rate == pytest.approx(0.02)
        # error events never pollute the ok-only e2e pool
        assert res.latency.e2e_duration_seconds.n == 98
        assert res.latency.e2e_duration_seconds.max == pytest.approx(0.5)

    def test_declared_threshold_is_respected_not_hardcoded(self):
        run = _mixed_run(n_ok=80, n_error=20)
        assert isinstance(_analyze(run, threshold=0.05).latency, WithheldLatency)
        permissive = _analyze(run, threshold=0.25)
        assert isinstance(permissive.latency, LatencyTables)
        assert permissive.gate_threshold == 0.25  # declared value echoed

    def test_deliberate_cancels_do_not_trip_the_gate(self):
        # cancel-storm workloads are features, not failures
        res = _analyze(_mixed_run(n_ok=50, n_canceled=50), threshold=0.05)
        assert isinstance(res.latency, LatencyTables)
        # canceled events with a real TTFT still pool into the TTFT table
        assert res.latency.ttft_seconds.n == 100
        # but e2e (completion latency) pools ok events only
        assert res.latency.e2e_duration_seconds.n == 50


class TestNoSamples:
    def test_zero_ttft_samples_withheld_as_no_samples(self):
        # all requests canceled pre-first-token: gate does not trip
        # (cancels are deliberate) but there is no TTFT to pool
        events = [
            make_event(i, sched=i * 0.5, status="canceled", ttft=None, e2e=0.15)
            for i in range(30)
        ]
        res = _analyze(make_run(events))
        assert isinstance(res.latency, WithheldLatency)
        assert res.latency.kind == "no-samples"
        assert any("zero pooled samples" in t for t in res.threats_to_validity)

    def test_no_samples_emission_refused(self, bundle):
        events = [
            make_event(i, sched=i * 0.5, status="canceled", ttft=None, e2e=0.15)
            for i in range(30)
        ]
        res = _analyze(make_run(events))
        with pytest.raises(ResultNotExpressibleError):
            res.to_benchmark_result_dict(bundle)
