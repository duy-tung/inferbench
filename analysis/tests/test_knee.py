"""Saturation-knee detection on synthetic sweeps with placed knees."""

import pytest

from inferbench_analysis import KneeInputError, SweepPoint, detect_knee


def sweep(pairs):
    return [SweepPoint(offered_rate_rps=r, value=v) for r, v in pairs]


class TestKnownKnees:
    def test_hockey_stick_knee_found_at_placed_rate(self):
        pts = sweep(
            [
                (1, 0.10),
                (2, 0.10),
                (3, 0.11),
                (4, 0.10),
                (5, 0.10),
                (6, 0.11),
                (7, 0.24),
                (8, 0.60),
                (9, 1.50),
                (10, 4.00),
            ]
        )
        k = detect_knee(pts, "ttft_seconds p99")
        assert k is not None
        assert k.arrival_rate_rps == 7.0
        assert k.bracketed is True
        assert "plateau-departure" in k.method
        assert "1.5x" in k.method
        assert 0.0 <= k.confidence <= 1.0

    def test_unsorted_input_same_answer(self):
        pts = sweep(
            [
                (8, 0.60),
                (1, 0.10),
                (10, 4.00),
                (3, 0.11),
                (5, 0.10),
                (7, 0.24),
                (2, 0.10),
                (9, 1.50),
                (4, 0.10),
                (6, 0.11),
            ]
        )
        assert detect_knee(pts, "s").arrival_rate_rps == 7.0

    def test_single_noise_spike_is_not_a_knee(self):
        # a spike at rate 5 that recovers must not be called the knee
        pts = sweep(
            [
                (1, 0.10),
                (2, 0.10),
                (3, 0.10),
                (4, 0.10),
                (5, 0.90),  # transient
                (6, 0.10),
                (7, 0.10),
                (8, 0.40),
                (9, 1.00),
                (10, 3.00),
            ]
        )
        k = detect_knee(pts, "s")
        assert k.arrival_rate_rps == 8.0  # sustained departure only

    def test_flat_sweep_has_no_knee(self):
        pts = sweep([(r, 0.10 + 0.001 * r) for r in range(1, 9)])
        assert detect_knee(pts, "s") is None

    def test_knee_at_sweep_edge_is_not_bracketed(self):
        pts = sweep(
            [(1, 0.1), (2, 0.1), (3, 0.1), (4, 0.1), (5, 0.1), (6, 5.0)]
        )
        k = detect_knee(pts, "s")
        assert k.arrival_rate_rps == 6.0
        assert k.bracketed is False
        assert k.confidence <= 0.5
        assert "NOT bracketed" in k.method


class TestRefusals:
    def test_fewer_than_six_points_refused(self):
        pts = sweep([(1, 0.1), (2, 0.1), (3, 0.1), (4, 0.5), (5, 2.0)])
        with pytest.raises(KneeInputError, match=">= 6"):
            detect_knee(pts, "s")

    def test_duplicate_rates_refused(self):
        pts = sweep(
            [(1, 0.1), (2, 0.1), (2, 0.2), (4, 0.1), (5, 0.5), (6, 2.0)]
        )
        with pytest.raises(KneeInputError, match="duplicate"):
            detect_knee(pts, "s")

    def test_nonpositive_values_refused(self):
        pts = sweep([(r, 0.0) for r in range(1, 7)])
        with pytest.raises(KneeInputError):
            detect_knee(pts, "s")
