"""Typed refusals of the analysis core.

Every refusal in this package is one of these types with a human-readable
reason. The methodology rules (docs/experiments.md) are enforced by refusing,
never by silently proceeding.
"""

from __future__ import annotations


class AnalysisError(Exception):
    """Base class for all typed analysis refusals."""


class LoaderError(AnalysisError):
    """Input refused: manifest-less, schema-invalid, or inconsistent data."""


class PoolingGuardError(AnalysisError):
    """Attempt to construct percentile results from anything other than
    pooled raw samples (e.g. averaging per-run percentiles — experiments.md
    rule 5)."""


class EmptyPoolError(AnalysisError):
    """A percentile table was requested over zero samples."""


class WarmupError(AnalysisError):
    """Warm-up policy missing/inconsistent, or exclusion left no events."""


class SLOError(AnalysisError):
    """SLO instance unusable for goodput (e.g. missing the stall objective)."""


class KneeInputError(AnalysisError):
    """Sweep input violates the sweep methodology (rule 3: >= 6 points)."""


class CostError(AnalysisError):
    """Cost profile unusable (no/ambiguous matching rate)."""


class ComparabilityError(AnalysisError):
    """Refusal to pool repetitions whose manifests differ on a comparability
    key (experiments.md rule 10)."""


class ResultNotExpressibleError(AnalysisError):
    """The run set is valid but its result cannot be serialized as a
    schema-valid benchmark-result at the pinned contracts version — e.g. the
    latency tables are withheld by the error/shed gate, or a required signal
    has zero samples. The run is NOT invalid; its latency table is
    meaningless/unquotable and the contract has no null-table form."""
