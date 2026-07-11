"""inferbench analysis core (IB-T005) + honest report generator (IB-T006).

Statistics engine for benchmark raw events (serving-contracts Contract 3):
pooled percentiles (never averaged across runs — structurally enforced),
bootstrap CIs, warm-up exclusion, saturation-knee detection, goodput@SLO
with shed and stall rates adjacent, cost from provenanced cost profiles,
schema-valid benchmark-result emission, and refusal-first Markdown report
rendering (mandatory validity/threats/anomalies sections, hypothesis
prominent, withheld-latency explanation, comparability rule verbatim).
"""

from .contracts import Bundle
from .cost import Cost, CostInputs, CostUnavailable, compute_cost
from .errors import (
    AnalysisError,
    ComparabilityError,
    CostError,
    EmptyPoolError,
    KneeInputError,
    LoaderError,
    PoolingGuardError,
    ReportInputError,
    ResultNotExpressibleError,
    SLOError,
    WarmupError,
)
from .report import (
    ANALYSIS_VERSION,
    COMPARABILITY_RULE_VERBATIM,
    ManifestEntry,
    ReportModel,
    check_comparability_verbatim,
    report_from_analysis,
    report_from_result_dict,
)
from .events import RawEvent, Run, check_poolable, load_run
from .goodput import Goodput, evaluate_goodput
from .knee import KneeEstimate, SweepPoint, detect_knee
from .percentiles import (
    POOLED_METHOD,
    BootstrapParams,
    PercentileTable,
    RunDispersion,
    bootstrap_ci,
    per_run_dispersion,
    pooled_table,
)
from .result import (
    DEFAULT_GATE_THRESHOLD,
    AnalysisConfig,
    AnalysisResult,
    LatencyTables,
    Throughput,
    WithheldLatency,
    analyze_run_set,
)
from .warmup import WarmupReport, apply_warmup

__version__ = ANALYSIS_VERSION

__all__ = [
    "ANALYSIS_VERSION",
    "COMPARABILITY_RULE_VERBATIM",
    "ManifestEntry",
    "ReportInputError",
    "ReportModel",
    "check_comparability_verbatim",
    "report_from_analysis",
    "report_from_result_dict",
    "AnalysisConfig",
    "AnalysisError",
    "AnalysisResult",
    "BootstrapParams",
    "Bundle",
    "ComparabilityError",
    "Cost",
    "CostError",
    "CostInputs",
    "CostUnavailable",
    "DEFAULT_GATE_THRESHOLD",
    "EmptyPoolError",
    "Goodput",
    "KneeEstimate",
    "KneeInputError",
    "LatencyTables",
    "LoaderError",
    "POOLED_METHOD",
    "PercentileTable",
    "PoolingGuardError",
    "RawEvent",
    "ResultNotExpressibleError",
    "Run",
    "RunDispersion",
    "SLOError",
    "SweepPoint",
    "Throughput",
    "WarmupError",
    "WarmupReport",
    "WithheldLatency",
    "analyze_run_set",
    "apply_warmup",
    "bootstrap_ci",
    "check_poolable",
    "compute_cost",
    "detect_knee",
    "evaluate_goodput",
    "load_run",
    "per_run_dispersion",
    "pooled_table",
]
