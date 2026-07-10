"""Warm-up exclusion known-answer tests (experiments.md rule 2)."""

import pytest

from inferbench_analysis import (
    AnalysisConfig,
    WarmupError,
    analyze_run_set,
    apply_warmup,
)
from conftest import make_event, make_manifest, make_run, make_slo


def _two_rep_run(warm_up, warm_e2e=10.0, steady_e2e=1.0, per_rep=100):
    """2 repetitions; the first half of each repetition is 'warm-up shaped'
    (slow), the second half steady, so exclusion is verifiable by value."""
    events = []
    for rep in (1, 2):
        base = (rep - 1) * 1000.0
        for i in range(per_rep):
            slow = i < per_rep // 2
            events.append(
                make_event(
                    i,
                    rep=rep,
                    sched=base + i * 1.0,
                    ttft=0.5 if slow else 0.05,
                    e2e=warm_e2e if slow else steady_e2e,
                )
            )
    return make_run(events, make_manifest(repetitions=2, warm_up=warm_up))


class TestDiscardRequests:
    def test_counts_and_kept_window(self):
        run = _two_rep_run({"policy": "discard-requests", "value": 50})
        kept, report = apply_warmup([run])
        assert report.excluded_total == 100  # 50 per repetition
        assert report.kept_total == 100
        assert report.excluded_per_repetition == {
            "syn-A/rep1": 50,
            "syn-A/rep2": 50,
        }
        # everything kept is steady-state by construction
        assert all(e.e2e_seconds == pytest.approx(1.0) for e in kept)

    def test_statistics_computed_after_exclusion(self):
        run = _two_rep_run({"policy": "discard-requests", "value": 50})
        res = analyze_run_set(
            [run],
            AnalysisConfig(slo=make_slo(), bootstrap=None),
            result_id="warmup",
        )
        # warm events (e2e=10) are gone: pooled e2e p99 is exactly 1.0
        assert res.latency.e2e_duration_seconds.p99 == pytest.approx(1.0)
        assert res.pooled_event_count == 100
        assert res.warmup.excluded_total == 100
        assert "50" in res.warmup.handling_statement()

    def test_ordering_is_scheduled_send(self):
        # shuffled event order on disk must not change what gets excluded
        run = _two_rep_run({"policy": "discard-requests", "value": 50})
        shuffled = make_run(tuple(reversed(run.events)), run.manifest)
        kept, report = apply_warmup([shuffled])
        assert report.excluded_total == 100
        assert all(e.e2e_seconds == pytest.approx(1.0) for e in kept)


class TestDiscardDuration:
    def test_first_seconds_of_each_repetition_excluded(self):
        run = _two_rep_run({"policy": "discard-duration", "value": 50.0})
        kept, report = apply_warmup([run])
        # events at 1s spacing: scheduled offsets 0..49 fall inside the
        # 50-second warm-up window per repetition
        assert report.excluded_total == 100
        assert report.kept_total == 100
        assert all(e.e2e_seconds == pytest.approx(1.0) for e in kept)


class TestNoneAndRefusals:
    def test_policy_none_excludes_nothing(self):
        run = _two_rep_run({"policy": "none"})
        kept, report = apply_warmup([run])
        assert report.excluded_total == 0
        assert report.kept_total == 200
        assert "none" in report.handling_statement()

    def test_missing_value_refused(self):
        run = _two_rep_run({"policy": "discard-requests"})
        with pytest.raises(WarmupError):
            apply_warmup([run])

    def test_everything_excluded_refused(self):
        run = _two_rep_run({"policy": "discard-requests", "value": 100})
        with pytest.raises(WarmupError, match="excluded every event"):
            apply_warmup([run])

    def test_missing_policy_refused(self):
        run = _two_rep_run(None)
        object.__setattr__(run, "manifest", {**run.manifest, "warm_up": {}})
        with pytest.raises(WarmupError):
            apply_warmup([run])
