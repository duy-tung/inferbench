"""Pooled percentiles + bootstrap CIs (experiments.md rule 5, ADR-0002 §1–2).

STRUCTURAL GUARD — pooling, never averaging:

* ``PercentileTable`` can only be constructed by :func:`pooled_table`, which
  takes RAW SAMPLES grouped per run and concatenates them before computing
  anything. Direct construction (i.e. from precomputed per-run percentile
  numbers, averaged or otherwise) raises ``PoolingGuardError``.
* No function in this package accepts two PercentileTables and combines them;
  the emitted contract field ``pooled_percentiles.method`` is the schema
  constant ``"pooled-raw-events"`` — averaging is not expressible.
* Cross-run dispersion is a separate type (:class:`RunDispersion`, median ±
  range of per-run summaries) that is not a PercentileTable and cannot be
  placed where a pooled table is required.

Percentile definition: linear interpolation between order statistics
(Hyndman–Fan type 7, ``numpy.quantile(..., method="linear")``), computed on
the pooled raw samples. Exact, no sketching (ADR-0002 alternatives).

Bootstrap CIs: nonparametric percentile bootstrap — resample the pooled
dataset with replacement B times, recompute each percentile, take the
(1−conf)/2 and 1−(1−conf)/2 empirical quantiles of the B replicates.
Finalized parameters (ADR-0002 changelog): B=1000 resamples, 95% interval,
seeded generator (default seed 20260710) so CIs are reproducible.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Mapping, Sequence

import numpy as np

from .errors import EmptyPoolError, PoolingGuardError

#: The only percentile method this package can emit (mirrors the
#: benchmark-result schema const).
POOLED_METHOD = "pooled-raw-events"

#: p999 is only reported when the pool can resolve it (>= 1000 samples);
#: below that it would just restate the maximum with false precision.
P999_MIN_SAMPLES = 1000

_GUARD = object()  # construction token: only pooled_table() has it


@dataclass(frozen=True)
class BootstrapParams:
    """ADR-0002 §2 finalized parameters."""

    resamples: int = 1000
    confidence: float = 0.95
    seed: int = 20260710


@dataclass(frozen=True)
class PercentileTable:
    """Percentiles of ONE signal over the pooled raw samples.

    ``ci`` maps a percentile label (e.g. "p99") to its bootstrap
    (lo, hi) interval; None when bootstrap was not requested. CIs are
    report-surface data — the benchmark-result percentileTable has no CI
    fields at the pinned contracts version.
    """

    n: int
    p50: float
    p90: float
    p95: float
    p99: float
    max: float
    mean: float
    p999: float | None = None
    ci: Mapping[str, tuple[float, float]] | None = None
    method: str = POOLED_METHOD
    _guard: object = field(default=None, repr=False, compare=False)

    def __post_init__(self) -> None:
        if self._guard is not _GUARD:
            raise PoolingGuardError(
                "PercentileTable must be built from pooled raw samples via "
                "pooled_table(); constructing one from precomputed percentile "
                "values (e.g. averaged per-run percentiles) is forbidden "
                "(experiments.md rule 5, ADR-0002 §1)"
            )
        if self.method != POOLED_METHOD:
            raise PoolingGuardError(
                f"percentile method must be '{POOLED_METHOD}'"
            )


@dataclass(frozen=True)
class RunDispersion:
    """Cross-run dispersion of one per-run summary statistic — reported
    ALONGSIDE the pooled table (median ± range, experiments.md rule 4),
    never instead of it. Deliberately not a PercentileTable."""

    statistic: str
    per_run: Mapping[str, float]
    median: float
    min: float
    max: float


_PCTS: tuple[tuple[str, float], ...] = (
    ("p50", 50.0),
    ("p90", 90.0),
    ("p95", 95.0),
    ("p99", 99.0),
)


def bootstrap_ci(
    samples: np.ndarray,
    percentiles: Sequence[float],
    params: BootstrapParams,
) -> list[tuple[float, float]]:
    """Percentile-bootstrap CI for each requested percentile of `samples`."""
    if samples.size == 0:
        raise EmptyPoolError("bootstrap over zero samples")
    rng = np.random.default_rng(params.seed)
    q = np.asarray(percentiles, dtype=float) / 100.0
    reps = np.empty((params.resamples, q.size), dtype=float)
    # chunk the resample matrix to bound memory at ~32 MB
    chunk = max(1, min(params.resamples, int(4_000_000 / max(samples.size, 1)) or 1))
    done = 0
    while done < params.resamples:
        b = min(chunk, params.resamples - done)
        idx = rng.integers(0, samples.size, size=(b, samples.size))
        reps[done : done + b] = np.quantile(samples[idx], q, axis=1).T
        done += b
    alpha = 1.0 - params.confidence
    lo = np.quantile(reps, alpha / 2.0, axis=0)
    hi = np.quantile(reps, 1.0 - alpha / 2.0, axis=0)
    return [(float(l), float(h)) for l, h in zip(lo, hi)]


def pooled_table(
    samples_by_run: Mapping[str, Sequence[float]],
    *,
    bootstrap: BootstrapParams | None = None,
) -> PercentileTable:
    """THE construction path for percentile tables.

    Takes raw per-request samples grouped by run/repetition, POOLS them into
    one dataset, and computes percentiles on the pool. There is deliberately
    no signature that accepts per-run percentile summaries.
    """
    arrays = [np.asarray(v, dtype=float) for v in samples_by_run.values()]
    pool = np.concatenate(arrays) if arrays else np.empty(0)
    if pool.size == 0:
        raise EmptyPoolError("pooled percentile table over zero samples")
    if np.any(~np.isfinite(pool)) or np.any(pool < 0):
        raise EmptyPoolError("pool contains non-finite or negative samples")

    labels = [name for name, _ in _PCTS]
    qs = [q for _, q in _PCTS]
    p999 = None
    if pool.size >= P999_MIN_SAMPLES:
        labels.append("p999")
        qs.append(99.9)
    values = np.quantile(pool, np.asarray(qs) / 100.0, method="linear")
    out = dict(zip(labels, (float(v) for v in values)))

    ci = None
    if bootstrap is not None:
        pairs = bootstrap_ci(pool, qs, bootstrap)
        ci = dict(zip(labels, pairs))

    return PercentileTable(
        n=int(pool.size),
        p50=out["p50"],
        p90=out["p90"],
        p95=out["p95"],
        p99=out["p99"],
        p999=out.get("p999"),
        max=float(pool.max()),
        mean=float(pool.mean()),
        ci=ci,
        _guard=_GUARD,
    )


def per_run_dispersion(
    samples_by_run: Mapping[str, Sequence[float]], statistic: str = "p50"
) -> RunDispersion:
    """Median ± range of a per-run summary statistic (rule 4). Report this
    NEXT TO the pooled table; it is intentionally a different type."""
    stat_q = {"p50": 50.0, "p90": 90.0, "p95": 95.0, "p99": 99.0}
    if statistic not in stat_q:
        raise PoolingGuardError(f"unsupported dispersion statistic '{statistic}'")
    per_run: dict[str, float] = {}
    for run_key, vals in samples_by_run.items():
        arr = np.asarray(vals, dtype=float)
        if arr.size == 0:
            continue
        per_run[run_key] = float(
            np.quantile(arr, stat_q[statistic] / 100.0, method="linear")
        )
    if not per_run:
        raise EmptyPoolError("dispersion over zero samples")
    vals = np.asarray(list(per_run.values()))
    return RunDispersion(
        statistic=statistic,
        per_run=per_run,
        median=float(np.median(vals)),
        min=float(vals.min()),
        max=float(vals.max()),
    )
