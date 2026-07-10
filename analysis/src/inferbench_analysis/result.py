"""Run-set analysis assembly + benchmark-result emission.

This is where the honesty rules become STRUCTURE:

* Percentile tables exist only as pooled tables (percentiles.py guard).
* Goodput exists only with shed_rate + stall_rate + slo_ref beside it
  (goodput.Goodput mirrors the contract's goodput block).
* ERROR/SHED GATE: when the measured window's combined error+shed rate
  exceeds the DECLARED threshold, the latency percentile tables are replaced
  by :class:`WithheldLatency` — the tables do not exist on the result object,
  so no caller can quote them — and the reason is written into the validity
  block. The run itself stays VALID (its throughput, error/shed accounting,
  goodput-vs-SLO and validity data are all real); only its latency table is
  meaningless. The same withholding applies when a contract-required signal
  has zero samples (e.g. a 100%-shed run has no TTFT at all).
* Emission: ``to_benchmark_result_dict()`` self-validates against the pinned
  benchmark-result schema and REFUSES (ResultNotExpressibleError) to
  serialize a result whose latency is withheld — the pinned contract has no
  null form for percentile tables, and emitting fabricated or gated numbers
  is forbidden. The refusal message carries the validity reason.

Pooling semantics per signal (documented decisions):

* ttft_seconds pools every event with a non-null TTFT regardless of final
  status — a first byte that arrived is a true TTFT measurement even if the
  stream later failed or was canceled.
* e2e_duration_seconds pools status=ok events only — the end-to-end latency
  of a failed request is a time-to-failure, not a completion latency.
* itl_seconds pools the individual inter-chunk gaps of every event carrying
  a series-form ITL record; summary-only events cannot be pooled and are
  counted into the validity block (contract rule).
* max_stall_seconds pools the per-request max stall of every ITL-bearing
  event.
* The measured window is the post-warm-up span sum over repetitions:
  max(end_ts) − min(scheduled_send_ts) per repetition (scheduled-send basis,
  CO-safe), summed — repetitions run back-to-back, not overlapped.
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Mapping, Sequence

from .contracts import Bundle
from .cost import Cost, CostInputs, CostUnavailable, compute_cost
from .errors import ResultNotExpressibleError
from .events import RawEvent, Run, check_poolable
from .goodput import Goodput, evaluate_goodput
from .knee import KneeEstimate
from .percentiles import (
    POOLED_METHOD,
    BootstrapParams,
    PercentileTable,
    RunDispersion,
    per_run_dispersion,
    pooled_table,
)
from .warmup import WarmupReport, apply_warmup

#: Default declared error/shed gate threshold (ADR-0002 changelog): latency
#: percentiles are withheld when more than 5% of measured-window requests
#: ended error or shed. Overridable per analysis — the declared value is
#: always echoed into the result, so a permissive gate is visible.
DEFAULT_GATE_THRESHOLD = 0.05


@dataclass(frozen=True)
class AnalysisConfig:
    slo: Mapping  # schema-validated slo.schema.json instance
    gate_threshold: float = DEFAULT_GATE_THRESHOLD
    bootstrap: BootstrapParams | None = field(default_factory=BootstrapParams)
    cost_inputs: CostInputs | None = None
    knee: KneeEstimate | None = None  # from a rate sweep, when one exists
    extra_threats: tuple[str, ...] = ()
    anomalies: tuple[str, ...] = ()  # empty tuple = "we looked, found none"


@dataclass(frozen=True)
class LatencyTables:
    """The pooled latency percentile tables (only when the gate passes)."""

    ttft_seconds: PercentileTable
    e2e_duration_seconds: PercentileTable
    itl_seconds: PercentileTable | None
    max_stall_seconds: PercentileTable | None


@dataclass(frozen=True)
class WithheldLatency:
    """Latency percentiles are ABSENT from this analysis — structurally.

    kind='error-shed-gate': the declared error+shed threshold was exceeded;
    quoting latency percentiles from a window dominated by failures would be
    meaningless (a 100%-timeout run has 'latencies' that measure the timeout,
    not the service).
    kind='no-samples': a contract-required signal has zero samples.
    The run set remains a valid run; only its latency table is withheld.
    """

    kind: str  # "error-shed-gate" | "no-samples"
    reason: str
    error_rate: float
    shed_rate: float
    threshold: float


@dataclass(frozen=True)
class Throughput:
    requests_per_second: float  # status=ok completions per measured second
    output_tokens_per_second: float
    total_requests: int
    total_output_tokens: int
    window_seconds: float


@dataclass(frozen=True)
class AnalysisResult:
    result_id: str
    created_at: str
    manifest_refs: tuple[str, ...]
    raw_event_refs: tuple[str, ...]
    throughput: Throughput
    latency: LatencyTables | WithheldLatency
    dispersion: Mapping[str, RunDispersion]  # report-surface (rule 4)
    goodput: Goodput
    knee: KneeEstimate | None
    cost: Cost | CostUnavailable
    warmup: WarmupReport
    run_count: int
    threats_to_validity: tuple[str, ...]
    unexplained_anomalies: tuple[str, ...]
    pooled_event_count: int
    gate_threshold: float
    error_rate: float
    shed_rate: float

    # -- serialization ----------------------------------------------------

    def to_benchmark_result_dict(self, bundle: Bundle) -> dict:
        """Serialize to a benchmark-result instance, self-validated against
        the pinned schema. REFUSES withheld-latency results: the contract has
        no null-table form, and quoting gated latency is forbidden."""
        if isinstance(self.latency, WithheldLatency):
            raise ResultNotExpressibleError(
                f"latency percentiles are withheld ({self.latency.kind}): "
                f"{self.latency.reason} — the run set is a VALID run, but a "
                "schema-valid benchmark-result cannot be emitted at the "
                "pinned contracts version (percentile tables are required and "
                "have no null form); publish the run's validity data via the "
                "report path instead of a result file"
            )
        tables: dict[str, dict] = {
            "ttft_seconds": _table_dict(self.latency.ttft_seconds),
            "e2e_duration_seconds": _table_dict(self.latency.e2e_duration_seconds),
        }
        if self.latency.itl_seconds is not None:
            tables["itl_seconds"] = _table_dict(self.latency.itl_seconds)
        if self.latency.max_stall_seconds is not None:
            tables["max_stall_seconds"] = _table_dict(self.latency.max_stall_seconds)

        goodput: dict = {
            "slo_ref": {"id": self.goodput.slo_id},
            "requests_per_second_meeting_slo": self.goodput.requests_per_second_meeting_slo,
            "ratio": self.goodput.ratio,
            "shed_rate": self.goodput.shed_rate,
            "stall_rate": self.goodput.stall_rate,
            "stall_threshold_seconds": self.goodput.stall_threshold_seconds,
        }
        if self.goodput.slo_version is not None:
            goodput["slo_ref"]["version"] = self.goodput.slo_version

        knee = None
        if self.knee is not None:
            knee = {
                "arrival_rate_rps": self.knee.arrival_rate_rps,
                "signal": self.knee.signal,
                "method": self.knee.method,
                "confidence": self.knee.confidence,
            }

        cost = None
        if isinstance(self.cost, Cost):
            cost = {
                "cost_profile_ref": {
                    "id": self.cost.profile_id,
                    "version": self.cost.profile_version,
                },
                "per_successful_request_usd": self.cost.per_successful_request_usd,
                "per_million_tokens_usd": self.cost.per_million_tokens_usd,
            }
            if self.cost.per_million_output_tokens_usd is not None:
                cost["per_million_output_tokens_usd"] = (
                    self.cost.per_million_output_tokens_usd
                )

        doc = {
            "result_id": self.result_id,
            "created_at": self.created_at,
            "links": {
                "run_manifests": list(self.manifest_refs),
                "raw_events": list(self.raw_event_refs),
            },
            "throughput": {
                "requests_per_second": self.throughput.requests_per_second,
                "output_tokens_per_second": self.throughput.output_tokens_per_second,
                "total_requests": self.throughput.total_requests,
                "total_output_tokens": self.throughput.total_output_tokens,
            },
            "pooled_percentiles": {
                "method": POOLED_METHOD,
                "pooled_event_count": self.pooled_event_count,
                "tables": tables,
            },
            "goodput": goodput,
            "knee_estimate": knee,
            "cost": cost,
            "validity": {
                "warm_up_handling": self.warmup.handling_statement(),
                "run_count": self.run_count,
                "threats_to_validity": list(self.threats_to_validity),
                "unexplained_anomalies": list(self.unexplained_anomalies),
            },
        }
        bundle.validate("benchmark-result", doc, context=self.result_id)
        return doc

    def write_benchmark_result(self, path: str | Path, bundle: Bundle) -> None:
        doc = self.to_benchmark_result_dict(bundle)
        p = Path(path)
        p.parent.mkdir(parents=True, exist_ok=True)
        with open(p, "w", encoding="utf-8") as f:
            json.dump(doc, f, indent=2, sort_keys=False)
            f.write("\n")


def _table_dict(t: PercentileTable) -> dict:
    d = {
        "n": t.n,
        "p50": t.p50,
        "p90": t.p90,
        "p95": t.p95,
        "p99": t.p99,
        "max": t.max,
        "mean": t.mean,
    }
    if t.p999 is not None:
        d["p999"] = t.p999
    return d


def _rep_key(run_idx: int, run: Run, rep: int) -> str:
    return f"{run.manifest['run_id']}/rep{rep}"


def analyze_run_set(
    runs: Sequence[Run],
    config: AnalysisConfig,
    *,
    result_id: str,
    created_at: str | None = None,
) -> AnalysisResult:
    """Analyze one run set (manifests must agree on every comparability key).

    One statistics pass: warm-up exclusion -> measured window -> throughput,
    error/shed gate, pooled latency tables (or withheld), goodput with shed +
    stall, cost, validity block.
    """
    check_poolable(runs)
    ids = [r.manifest["run_id"] for r in runs]
    if len(set(ids)) != len(ids):
        raise ResultNotExpressibleError(
            f"duplicate run_id in run set: {ids} — the same run must not be "
            "pooled twice (cherry-picking/double-counting guard)"
        )
    measured, warmup_report = apply_warmup(runs)

    # measured window: per-repetition span on the scheduled-send basis, summed
    spans: dict[str, tuple[float, float]] = {}
    for run_idx, run in enumerate(runs):
        reps = sorted({e.repetition for e in run.events})
        for rep in reps:
            evs = [e for e in measured if e.run_id == run.manifest["run_id"] and e.repetition == rep]
            if not evs:
                continue
            key = _rep_key(run_idx, run, rep)
            spans[key] = (
                min(e.scheduled_send_ts for e in evs),
                max(e.end_ts for e in evs),
            )
    window_seconds = sum(hi - lo for lo, hi in spans.values())
    run_count = sum(len({e.repetition for e in run.events}) for run in runs)

    total = len(measured)
    ok = [e for e in measured if e.status == "ok"]
    errors = [e for e in measured if e.status == "error"]
    shed = [e for e in measured if e.status == "shed"]
    canceled = [e for e in measured if e.status == "canceled"]
    output_tokens = sum(e.output_tokens for e in measured)
    input_tokens = sum(e.input_tokens for e in measured)

    throughput = Throughput(
        requests_per_second=len(ok) / window_seconds,
        output_tokens_per_second=output_tokens / window_seconds,
        total_requests=total,
        total_output_tokens=output_tokens,
        window_seconds=window_seconds,
    )

    error_rate = len(errors) / total
    shed_rate = len(shed) / total

    # per-repetition sample groups: the pooling unit is raw samples per run
    def by_rep(selector) -> dict[str, list[float]]:
        groups: dict[str, list[float]] = {}
        for run_idx, run in enumerate(runs):
            for e in measured:
                if e.run_id != run.manifest["run_id"]:
                    continue
                v = selector(e)
                if v is None:
                    continue
                key = _rep_key(run_idx, run, e.repetition)
                if isinstance(v, (list, tuple)):
                    groups.setdefault(key, []).extend(float(x) for x in v)
                else:
                    groups.setdefault(key, []).append(float(v))
        return groups

    ttft_groups = by_rep(lambda e: e.ttft_seconds)
    e2e_groups = by_rep(lambda e: e.e2e_seconds if e.status == "ok" else None)
    itl_groups = by_rep(
        lambda e: e.itl.series_seconds if e.itl and e.itl.series_seconds else None
    )
    stall_groups = by_rep(lambda e: e.itl.max_stall_seconds if e.itl else None)
    summary_only = sum(
        1 for e in measured if e.itl and not e.itl.series_seconds and e.itl.summary
    )
    streaming_count = sum(1 for e in measured if e.itl)

    threats: list[str] = list(config.extra_threats)

    # --- error/shed gate (declared threshold, CO re-review requirement) ---
    combined = error_rate + shed_rate
    latency: LatencyTables | WithheldLatency
    if combined > config.gate_threshold:
        reason = (
            f"error+shed rate {combined:.4f} (error {error_rate:.4f} + shed "
            f"{shed_rate:.4f}) exceeds the declared gate threshold "
            f"{config.gate_threshold:g}: latency percentiles are withheld — a "
            "window dominated by failures has no meaningful latency table "
            "(the run itself remains valid; its error/shed accounting and "
            "goodput are reported)"
        )
        latency = WithheldLatency(
            kind="error-shed-gate",
            reason=reason,
            error_rate=error_rate,
            shed_rate=shed_rate,
            threshold=config.gate_threshold,
        )
        threats.append(f"latency percentiles withheld: {reason}")
    elif not any(ttft_groups.values()) or not any(e2e_groups.values()):
        missing = "ttft_seconds" if not any(ttft_groups.values()) else "e2e_duration_seconds"
        reason = (
            f"signal '{missing}' has zero pooled samples in the measured "
            "window (e.g. no request ever produced a first byte / no request "
            "completed ok) — the contract-required latency table cannot be "
            "computed; latency is withheld, the run remains valid"
        )
        latency = WithheldLatency(
            kind="no-samples",
            reason=reason,
            error_rate=error_rate,
            shed_rate=shed_rate,
            threshold=config.gate_threshold,
        )
        threats.append(f"latency percentiles withheld: {reason}")
    else:
        latency = LatencyTables(
            ttft_seconds=pooled_table(ttft_groups, bootstrap=config.bootstrap),
            e2e_duration_seconds=pooled_table(e2e_groups, bootstrap=config.bootstrap),
            itl_seconds=(
                pooled_table(itl_groups, bootstrap=config.bootstrap)
                if any(itl_groups.values())
                else None
            ),
            max_stall_seconds=(
                pooled_table(stall_groups, bootstrap=config.bootstrap)
                if any(stall_groups.values())
                else None
            ),
        )

    dispersion: dict[str, RunDispersion] = {}
    if isinstance(latency, LatencyTables) and run_count > 1:
        dispersion["ttft_seconds_p50"] = per_run_dispersion(ttft_groups, "p50")
        dispersion["e2e_duration_seconds_p50"] = per_run_dispersion(e2e_groups, "p50")

    goodput = evaluate_goodput(measured, config.slo, window_seconds)

    cost = compute_cost(
        config.cost_inputs,
        window_seconds=window_seconds,
        ok_count=len(ok),
        total_tokens=input_tokens + output_tokens,
        output_tokens=output_tokens,
    )

    # --- auto-generated validity threats (honesty is not optional) -------
    if run_count < 3:
        threats.append(
            f"run_count={run_count} is below the >=3-repetitions methodology "
            "minimum (experiments.md rule 4); cross-run dispersion is not "
            "assessable"
        )
    if config.knee is None:
        threats.append(
            "no rate sweep in this run set — knee_estimate is null; no claim "
            "is made about saturation behavior"
        )
    elif not config.knee.bracketed:
        threats.append(
            "knee estimate lies at the highest swept rate and is NOT "
            "bracketed — the true knee may lie beyond the sweep"
        )
    if isinstance(cost, CostUnavailable):
        threats.append(cost.reason)
    if warmup_report.policy == "none":
        threats.append(
            "warm-up policy 'none' declared in the manifest: no warm-up "
            "exclusion applied; results include any cold-start effects "
            "(experiments.md rule 2 requires exclusion for published "
            "benchmark claims)"
        )
    if streaming_count == 0:
        threats.append(
            "no ITL-bearing requests in the measured window: itl_seconds/"
            "max_stall_seconds tables absent; stall_rate is 0.0 computed over "
            "0 streaming requests (vacuous, not evidence of stall-freedom)"
        )
    if summary_only:
        threats.append(
            f"{summary_only} event(s) carry summary-only ITL records; their "
            "gaps cannot be pooled into itl_seconds (contract rule) and are "
            "excluded from the ITL table"
        )
    if canceled:
        threats.append(
            f"{len(canceled)} deliberately canceled request(s) (workload "
            "cancellation profile) count in the offered denominator and never "
            "meet the SLO; goodput ratio reflects that by design"
        )

    return AnalysisResult(
        result_id=result_id,
        created_at=created_at
        or datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        manifest_refs=tuple(r.manifest_path for r in runs),
        raw_event_refs=tuple(r.events_path for r in runs),
        throughput=throughput,
        latency=latency,
        dispersion=dispersion,
        goodput=goodput,
        knee=config.knee,
        cost=cost,
        warmup=warmup_report,
        run_count=run_count,
        threats_to_validity=tuple(threats),
        unexplained_anomalies=tuple(config.anomalies),
        pooled_event_count=total,
        gate_threshold=config.gate_threshold,
        error_rate=error_rate,
        shed_rate=shed_rate,
    )
