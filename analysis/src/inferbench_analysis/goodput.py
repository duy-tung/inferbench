"""Goodput@SLO with shed rate and stall rate in the SAME pass (rule 7,
ADR-0002 §5; DistServe-style per-request SLO attainment).

STRUCTURAL adjacency (mirrors benchmark-result.goodput): the ONLY output is
the frozen :class:`Goodput` value object, which always carries ``shed_rate``
and ``stall_rate`` next to the ratio. No function returns a bare goodput
number, and an SLO with no stall objective is refused — goodput without a
stall threshold is not computable here at all.

Per-request evaluation semantics (documented decisions):

* A request MEETS the SLO iff its status is ``ok`` AND every objective holds
  on the request's own signal values. Shed, errored, and canceled requests
  never meet the SLO but always count in the offered denominator (shedding
  or cancel-storming cannot inflate the ratio).
* Objective thresholds are applied per request (DistServe SLO attainment):
  for scalar-per-request signals (ttft_seconds, e2e_duration_seconds,
  max_stall_seconds) the request's own value is compared against the
  threshold; for itl_seconds the objective's ``statistic`` is computed over
  the request's OWN gap series (or read from its recorded summary) first.
  The declared ``statistic`` of scalar signals describes the fleet-level
  conformance form of the SLO; ``max`` is the exact per-request reading.
* A request with a null signal that the SLO constrains fails that objective
  (a shed request has no TTFT — it did not meet a TTFT target), EXCEPT
  stall/ITL objectives on requests with fewer than two content chunks, which
  pass vacuously (a stream that never had two chunks cannot stall).
* ``stall_rate`` = stalled / streaming requests, where streaming = requests
  carrying an ITL record and stalled = ``max_stall_seconds`` strictly above
  the SLO stall threshold. With zero streaming requests the rate is 0.0 and
  the caller must surface that in the validity block (result.py does).
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Mapping, Sequence

import numpy as np

from .errors import SLOError
from .events import RawEvent

_SCALAR_SIGNALS = {
    "ttft_seconds",
    "client_ttft_seconds",
    "e2e_duration_seconds",
    "client_e2e_duration_seconds",
    "max_stall_seconds",
    "client_max_stall_seconds",
}
_ITL_SIGNALS = {"itl_seconds", "client_itl_seconds"}
_STALL_SIGNALS = {"max_stall_seconds", "client_max_stall_seconds"}


@dataclass(frozen=True)
class Goodput:
    """Goodput and its mandatory adjacent rates — one object, one pass."""

    slo_id: str
    slo_version: str | None
    requests_per_second_meeting_slo: float
    ratio: float  # meeting / ALL offered (incl. shed/canceled/errored)
    shed_rate: float
    stall_rate: float
    stall_threshold_seconds: float
    meeting_count: int
    offered_count: int
    streaming_count: int
    stalled_count: int


def _compare(value: float, comparator: str, threshold: float) -> bool:
    if comparator == "<":
        return value < threshold
    if comparator == "<=":
        return value <= threshold
    if comparator == ">":
        return value > threshold
    if comparator == ">=":
        return value >= threshold
    raise SLOError(f"unknown comparator '{comparator}'")


def _own_itl_statistic(ev: RawEvent, statistic: str) -> float | None:
    """The request's own ITL statistic, from its series (preferred) or its
    recorded summary. None => not computable for this request."""
    if ev.itl is None:
        return None
    if ev.itl.series_seconds:
        arr = np.asarray(ev.itl.series_seconds, dtype=float)
        if statistic == "mean":
            return float(arr.mean())
        if statistic == "max":
            return float(arr.max())
        q = {"p50": 0.50, "p90": 0.90, "p95": 0.95, "p99": 0.99, "p999": 0.999}.get(
            statistic
        )
        if q is None:
            raise SLOError(f"unsupported itl statistic '{statistic}'")
        return float(np.quantile(arr, q, method="linear"))
    if ev.itl.summary is not None:
        key = {
            "mean": "mean_seconds",
            "p50": "p50_seconds",
            "p95": "p95_seconds",
            "p99": "p99_seconds",
            "max": "max_seconds",
        }.get(statistic)
        if key is None or key not in ev.itl.summary:
            raise SLOError(
                f"itl statistic '{statistic}' not recoverable from a "
                "summary-only ITL record"
            )
        return float(ev.itl.summary[key])
    return None


def _objective_holds(ev: RawEvent, obj: Mapping) -> bool:
    signal = obj["signal"]
    comparator = obj["comparator"]
    threshold = obj["threshold"]
    if signal in _ITL_SIGNALS or signal in _STALL_SIGNALS:
        if ev.itl is None:
            return True  # vacuous: <2 content chunks, cannot stall
        if signal in _STALL_SIGNALS:
            return _compare(ev.itl.max_stall_seconds, comparator, threshold)
        value = _own_itl_statistic(ev, obj["statistic"])
        if value is None:
            return True
        return _compare(value, comparator, threshold)
    if signal in ("ttft_seconds", "client_ttft_seconds"):
        if ev.ttft_seconds is None:
            return False  # no first byte ever arrived: TTFT target not met
        return _compare(ev.ttft_seconds, comparator, threshold)
    if signal in ("e2e_duration_seconds", "client_e2e_duration_seconds"):
        return _compare(ev.e2e_seconds, comparator, threshold)
    raise SLOError(
        f"SLO objective signal '{signal}' is not a per-request client-side "
        "signal this analyzer can evaluate"
    )


def stall_threshold_of(slo: Mapping) -> float:
    """The SLO's stall threshold. REFUSES SLOs without a max-stall objective:
    goodput must never exist without a stall rate, and a stall rate needs a
    declared threshold (rule 7)."""
    thresholds = [
        o["threshold"]
        for o in slo["objectives"]
        if o["signal"] in _STALL_SIGNALS and o["comparator"] in ("<", "<=")
    ]
    if not thresholds:
        raise SLOError(
            f"SLO '{slo.get('slo_id', '?')}' declares no max_stall_seconds "
            "upper-bound objective — goodput@SLO requires a stall threshold "
            "so the stall rate can be computed beside it (experiments.md "
            "rule 7); refusing"
        )
    return float(min(thresholds))


def evaluate_goodput(
    events: Sequence[RawEvent], slo: Mapping, window_seconds: float
) -> Goodput:
    """Single pass over the measured-window events: goodput ratio, shed rate,
    and stall rate computed together and returned as one object."""
    if not events:
        raise SLOError("goodput over zero events")
    if window_seconds <= 0:
        raise SLOError(f"non-positive measured window ({window_seconds}s)")
    stall_threshold = stall_threshold_of(slo)
    objectives = slo["objectives"]

    offered = len(events)
    meeting = 0
    shed = 0
    streaming = 0
    stalled = 0
    for ev in events:
        if ev.shed or ev.status == "shed":
            shed += 1
        if ev.itl is not None:
            streaming += 1
            if ev.itl.max_stall_seconds > stall_threshold:
                stalled += 1
        if ev.status == "ok" and all(_objective_holds(ev, o) for o in objectives):
            meeting += 1

    return Goodput(
        slo_id=slo["slo_id"],
        slo_version=slo.get("version"),
        requests_per_second_meeting_slo=meeting / window_seconds,
        ratio=meeting / offered,
        shed_rate=shed / offered,
        stall_rate=(stalled / streaming) if streaming else 0.0,
        stall_threshold_seconds=stall_threshold,
        meeting_count=meeting,
        offered_count=offered,
        streaming_count=streaming,
        stalled_count=stalled,
    )
