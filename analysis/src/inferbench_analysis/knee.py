"""Saturation-knee detection from rate sweeps (ADR-0002 §4).

Primary method — plateau-departure threshold:

1. Sort sweep points by offered rate; refuse sweeps with < 6 points
   (experiments.md rule 3 — fewer points cannot bracket a knee honestly).
2. Estimate the low-rate plateau as the MEDIAN of the signal over the lowest
   ``plateau_points`` rates (default: the lowest third, at least 2).
3. The knee is the FIRST rate whose signal exceeds ``departure_factor`` ×
   plateau (default 1.5×) AND stays above it for every higher sweep rate
   (sustained departure — a single noisy point is not a knee).

Cross-check — kneedle-style maximum curvature: normalize rates and signal to
[0, 1]; for a convex increasing latency curve the knee is at the maximum of
(x_norm − y_norm). Agreement within one sweep point raises the reported
confidence; disagreement lowers it.

HONEST LIMITATIONS (stated in the emitted method string and ADR-0002):

* Resolution is limited to the sweep-point spacing — the knee is located at
  a measured sweep rate, never interpolated between rates, and the true knee
  lies somewhere in the interval (previous point, reported point].
* The method assumes a single plateau-then-degrade shape; multi-modal or
  non-monotone degradation (e.g. throughput collapse then recovery) can
  produce a misleading single knee — inspect the sweep curve.
* ``departure_factor`` is a declared judgment call, not a statistical test;
  it is echoed in the method string so every consumer sees it.
* A knee at the highest sweep rate is NOT bracketed: the true knee may lie
  beyond the sweep. It is reported with bracketed=False, capped confidence,
  and must be listed as a threat to validity (result assembly does this).

No extrapolation past the knee is performed anywhere (rule 11).
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Sequence

import numpy as np

from .errors import KneeInputError

MIN_SWEEP_POINTS = 6
DEFAULT_DEPARTURE_FACTOR = 1.5


@dataclass(frozen=True)
class SweepPoint:
    offered_rate_rps: float
    value: float  # the tracked signal at this rate (e.g. pooled ttft p99)


@dataclass(frozen=True)
class KneeEstimate:
    arrival_rate_rps: float
    signal: str
    method: str
    confidence: float
    bracketed: bool  # False => knee at sweep edge; true knee may lie beyond


def detect_knee(
    points: Sequence[SweepPoint],
    signal: str,
    *,
    departure_factor: float = DEFAULT_DEPARTURE_FACTOR,
    plateau_points: int | None = None,
) -> KneeEstimate | None:
    """Estimate the saturation knee; None when no sustained departure exists
    within the sweep (the target never left its plateau at swept rates)."""
    if len(points) < MIN_SWEEP_POINTS:
        raise KneeInputError(
            f"sweep has {len(points)} points; >= {MIN_SWEEP_POINTS} required "
            "(experiments.md rule 3)"
        )
    pts = sorted(points, key=lambda p: p.offered_rate_rps)
    rates = np.asarray([p.offered_rate_rps for p in pts], dtype=float)
    values = np.asarray([p.value for p in pts], dtype=float)
    if np.any(~np.isfinite(values)) or np.any(values <= 0):
        raise KneeInputError("sweep signal values must be finite and positive")
    if np.unique(rates).size != rates.size:
        raise KneeInputError(
            "duplicate offered rates in sweep — pool repetitions per rate "
            "point before knee detection"
        )

    k = plateau_points if plateau_points is not None else max(2, len(pts) // 3)
    if k >= len(pts):
        raise KneeInputError("plateau_points must leave points above the plateau")
    plateau = float(np.median(values[:k]))
    threshold = departure_factor * plateau

    # first index from which the signal stays above the departure threshold
    knee_idx: int | None = None
    above = values > threshold
    for i in range(len(pts) - 1, -1, -1):
        if above[i]:
            knee_idx = i
        else:
            break
    if knee_idx is None:
        return None

    # kneedle-style cross-check: max of (x_norm - y_norm) on the normalized
    # convex increasing curve
    x = (rates - rates[0]) / (rates[-1] - rates[0])
    y = (values - values.min()) / (values.max() - values.min())
    cross_idx = int(np.argmax(x - y))
    agree = abs(cross_idx - knee_idx) <= 1

    bracketed = knee_idx < len(pts) - 1
    confidence = 0.8 if agree else 0.5
    if not bracketed:
        confidence = min(confidence, 0.5)

    method = (
        "plateau-departure threshold: first offered rate with signal > "
        f"{departure_factor:g}x low-rate plateau median (plateau = median of "
        f"{k} lowest-rate points = {plateau:.6g}), sustained through the top "
        "of the sweep; kneedle-style max-curvature cross-check "
        f"{'agrees' if agree else 'disagrees'} "
        f"(cross-check point {cross_idx + 1}/{len(pts)}). Resolution limited "
        "to sweep-point spacing; assumes plateau-then-degrade shape; no "
        "extrapolation past the knee"
        + ("" if bracketed else "; knee at sweep edge — NOT bracketed")
    )
    return KneeEstimate(
        arrival_rate_rps=float(rates[knee_idx]),
        signal=signal,
        method=method,
        confidence=confidence,
        bracketed=bracketed,
    )
