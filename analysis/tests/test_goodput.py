"""Goodput@SLO with shed and stall structurally adjacent (rule 7)."""

import dataclasses

import pytest

from inferbench_analysis import Goodput, SLOError, evaluate_goodput
from conftest import make_event, make_slo


class TestKnownAnswers:
    def test_ratio_shed_stall_in_one_object(self):
        # 10 ok meeting; 5 ok failing the TTFT target; 5 shed.
        # SLO: ttft <= 0.3, e2e <= 2.0, stall <= 0.2
        events = (
            [
                make_event(i, sched=i, ttft=0.1, e2e=0.5, itl_series=(0.05, 0.06))
                for i in range(10)
            ]
            + [
                make_event(10 + i, sched=10 + i, ttft=0.4, e2e=0.5)
                for i in range(5)
            ]
            + [
                make_event(15 + i, sched=15 + i, status="shed", e2e=0.01)
                for i in range(5)
            ]
        )
        g = evaluate_goodput(events, make_slo(), window_seconds=20.0)
        assert g.ratio == pytest.approx(10 / 20)
        assert g.requests_per_second_meeting_slo == pytest.approx(10 / 20.0)
        assert g.shed_rate == pytest.approx(5 / 20)
        assert g.stall_rate == 0.0
        assert g.stall_threshold_seconds == pytest.approx(0.2)
        assert g.slo_id == "syn-slo"

    def test_stall_rate_counts_streaming_requests_over_threshold(self):
        # 4 streaming requests, 1 stalls (0.5s gap > 0.2s threshold) and
        # therefore ALSO fails the SLO's stall objective
        events = [
            make_event(0, sched=0, itl_series=(0.05, 0.05)),
            make_event(1, sched=1, itl_series=(0.05, 0.5)),  # stalled
            make_event(2, sched=2, itl_series=(0.05, 0.06)),
            make_event(3, sched=3, itl_series=(0.01,)),
            make_event(4, sched=4),  # non-streaming: not in stall denominator
        ]
        g = evaluate_goodput(events, make_slo(), window_seconds=5.0)
        assert g.streaming_count == 4
        assert g.stalled_count == 1
        assert g.stall_rate == pytest.approx(0.25)
        assert g.ratio == pytest.approx(4 / 5)  # the stalled one fails the SLO

    def test_canceled_and_errored_never_meet(self):
        events = [
            make_event(0, sched=0),  # meets
            make_event(1, sched=1, status="canceled", ttft=0.1, e2e=0.3),
            make_event(2, sched=2, status="error", ttft=None, e2e=30.0),
        ]
        g = evaluate_goodput(events, make_slo(), window_seconds=3.0)
        assert g.ratio == pytest.approx(1 / 3)
        assert g.offered_count == 3

    def test_per_request_itl_statistic(self):
        slo = make_slo(
            extra_objectives=[
                {
                    "signal": "itl_seconds",
                    "statistic": "p50",
                    "comparator": "<=",
                    "threshold": 0.25,
                    "unit": "seconds",
                    "provenance": {
                        "basis": "measured",
                        "as_of": "2026-07-10",
                        "source": "synthetic",
                    },
                }
            ]
        )
        passing = make_event(0, sched=0, itl_series=(0.1, 0.15, 0.2))  # own p50 0.15
        failing = make_event(1, sched=1, itl_series=(0.1, 0.3, 0.4))  # own p50 0.3 > 0.25 but... max_stall 0.4 > 0.2 also fails stall
        g = evaluate_goodput([passing, failing], slo, window_seconds=2.0)
        assert g.ratio == pytest.approx(0.5)


class TestStructure:
    def test_goodput_object_always_carries_adjacent_rates(self):
        fields = {f.name for f in dataclasses.fields(Goodput)}
        assert {"ratio", "shed_rate", "stall_rate", "slo_id"} <= fields

    def test_slo_without_stall_objective_refused(self):
        events = [make_event(0, sched=0)]
        with pytest.raises(SLOError, match="stall"):
            evaluate_goodput(events, make_slo(stall_max=None), window_seconds=1.0)

    def test_zero_events_refused(self):
        with pytest.raises(SLOError):
            evaluate_goodput([], make_slo(), window_seconds=1.0)
