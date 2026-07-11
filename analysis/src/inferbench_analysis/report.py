"""Honest benchmark report rendering (IB-T006) — the report template IS this
module.

REFUSAL-FIRST DESIGN (this is gate G4's attack surface — the honesty rules
are code structure, not convention):

* There is exactly ONE renderer (:func:`_render`) with a FIXED section order
  baked into code. There is no parameter to skip, reorder, or empty a
  mandatory section. This is deliberately stdlib string building and not a
  file-based template engine: an editable template file is precisely the
  artifact a hurried author would strip the validity sections from; here the
  mandatory sections are unconditional code paths (and a jinja2 dependency
  would buy nothing else — deps policy, analysis/pyproject.toml).
* A report cannot render without: a COMPLETE validity block
  (warm_up_handling, run_count, threats_to_validity, unexplained_anomalies —
  the exact benchmark-result contract fields), a non-empty HYPOTHESIS in
  every embedded manifest (displayed prominently, right under the title),
  goodput carrying shed_rate AND stall_rate (rendered adjacent, same table),
  and either latency percentile tables or a withheld-latency explanation
  (kind + reason + rates) — never a blank table.
* The "Unexplained anomalies" section is NEVER silently empty: it either
  enumerates anomalies or states "none observed" TOGETHER WITH the list of
  checks that were run; an empty anomalies list with an empty checks list is
  a typed refusal.
* The benchmark comparability rule is embedded VERBATIM from
  serving-contracts ``compatibility/compatibility-policy.md`` §7 (the schema
  says it "MUST be printed in every rendered report"). When the pinned
  bundle is supplied at render time, the embedded copy is checked against
  the bundle's policy file and drift is a typed refusal.
* ``cost: null`` always renders WITH its reason; a null cost whose reason is
  missing from the input is called out as a validity gap in the report.
* Closed-loop arrival (from the workload file, when available beside the
  manifest) is flagged in the header, the interpretation rules, and the
  latency section. When no workload file is available the report says so
  explicitly instead of implying open-loop.

Two builders feed the renderer:

* :func:`report_from_analysis` — from an in-memory :class:`AnalysisResult`
  (full fidelity: bootstrap CIs, cross-run dispersion, gate rates, withheld
  latency). This is the ONLY publishable surface for a valid run whose
  latency was withheld — the pinned contract has no null-table form, so no
  result file exists for those runs (see result.py).
* :func:`report_from_result_dict` — from an emitted benchmark-result file
  (schema-validated by the caller; re-checked structurally here so a
  validation bypass still refuses).
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Mapping, Sequence

from .contracts import Bundle
from .cost import Cost, CostUnavailable
from .errors import ReportInputError
from .percentiles import POOLED_METHOD, RunDispersion
from .result import AnalysisResult, LatencyTables, WithheldLatency

#: Version printed in every report header (kept in sync with pyproject.toml).
ANALYSIS_VERSION = "0.2.0"

#: The benchmark comparability rule, VERBATIM from serving-contracts
#: compatibility/compatibility-policy.md §7 at pin 8d81492 (v0.2.0 tag
#: pending). The schema requires it printed in every rendered report; when a
#: bundle is supplied at render time, drift between this copy and the
#: bundle's policy file is a typed refusal (re-pin this constant with the
#: bundle).
COMPARABILITY_RULE_VERBATIM = (
    "results are comparable only when model revision, quantization, "
    "tokenizer, engine version+flags, hardware, driver/CUDA, workload "
    "version+seed, and warm-up policy all match, **or** the difference is "
    "the single declared experimental variable."
)
COMPARABILITY_RULE_SOURCE = (
    "serving-contracts compatibility/compatibility-policy.md §7, "
    "pin 8d81492 (v0.2.0 tag pending)"
)

#: Checks the analysis pipeline runs on every run set — printed under
#: "Unexplained anomalies" whenever that list is empty, so "none observed"
#: is always a statement about checks performed, never a shrug. The two
#: {gate}/{window} slots are filled with the run set's actual numbers.
ANALYSIS_ANOMALY_CHECKS: tuple[str, ...] = (
    "manifest(s) and every raw event schema-validated against the pinned "
    "contracts bundle (the loader refuses manifest-less or schema-invalid "
    "data outright)",
    "run_id/repetition consistency between events and manifest enforced by "
    "the loader",
    "comparability keys (target_topology, workload_ref, engine, model, "
    "hardware, gateway, warm_up) verified identical across all pooled runs; "
    "duplicate run_ids refused (double-count/cherry-pick guard)",
    "warm-up exclusion counted per repetition in scheduled-send order and "
    "reconciled into the validity block",
    "declared error/shed gate evaluated over the measured window: {gate}",
    "zero-sample check on the contract-required latency signals "
    "(ttft_seconds, e2e_duration_seconds)",
    "goodput ratio, shed rate, and stall rate computed in one pass over the "
    "same measured window ({window})",
    "per-run p50 dispersion (median ± range) computed beside the pooled "
    "tables where run_count > 1",
)

#: Checks performed when rendering from an emitted benchmark-result file.
RESULT_FILE_ANOMALY_CHECKS: tuple[str, ...] = (
    "benchmark-result file schema-validated against the pinned contracts "
    "bundle",
    "linked run manifest(s) resolved, loaded, and schema-validated",
    "goodput block verified to carry shed_rate and stall_rate (contract-"
    "required siblings; a goodput without them is not renderable)",
    "validity block verified complete (warm_up_handling, run_count, "
    "threats_to_validity, unexplained_anomalies)",
    "validity.warm_up_handling cross-checked against the manifest's "
    "declared warm-up policy",
)

_LATENCY_TABLE_ORDER: tuple[str, ...] = (
    "ttft_seconds",
    "e2e_duration_seconds",
    "itl_seconds",
    "max_stall_seconds",
    "queue_wait_seconds",
)


# --------------------------------------------------------------------------
# model
# --------------------------------------------------------------------------


@dataclass(frozen=True)
class ManifestEntry:
    """One run manifest to embed, plus its workload file when available
    (the workload file carries the arrival process — the closed-loop flag
    source)."""

    path: str
    manifest: Mapping
    workload: Mapping | None = None
    workload_path: str | None = None


@dataclass(frozen=True)
class ReportModel:
    """Everything :func:`_render` needs. Mandatory honesty elements are
    validated by :func:`_check_model` before any markdown is produced."""

    result_id: str
    created_at: str
    generated_at: str
    source: str  # provenance of the numbers (result file vs in-memory analysis)
    pin: str  # contracts bundle pin (from the manifests)
    manifests: tuple[ManifestEntry, ...]
    throughput: Mapping
    # exactly one of (latency_tables, withheld) is set
    latency_tables: Mapping[str, Mapping] | None
    latency_ci: Mapping[str, Mapping] | None  # signal -> {label: (lo, hi)}
    ci_params: str | None  # description of bootstrap params, when CIs present
    withheld: Mapping | None  # kind/reason/error_rate/shed_rate/threshold
    dispersion: Mapping[str, RunDispersion] | None
    goodput: Mapping
    knee: Mapping | None
    cost: Mapping | None
    cost_null_reason: str | None
    warm_up_handling: str
    run_count: int
    pooled_event_count: int | None
    threats: tuple[str, ...]
    anomalies: tuple[str, ...]
    anomaly_checks: tuple[str, ...]
    gate: Mapping | None  # error_rate/shed_rate/threshold (analysis mode only)
    repro_command: str
    manifest_refs: tuple[str, ...]
    raw_event_refs: tuple[str, ...]
    result_file_ref: str | None


# --------------------------------------------------------------------------
# helpers
# --------------------------------------------------------------------------


def _fail(msg: str) -> None:
    raise ReportInputError(msg)


def _now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _sec(v: float) -> str:
    return f"{v:.6f}"


def _rate(v: float) -> str:
    return f"{v:.4f}"


def _normalize(text: str) -> str:
    """Whitespace/emphasis-insensitive form for the verbatim drift check."""
    return re.sub(r"\s+", " ", text.replace("*", "")).strip().lower()


def check_comparability_verbatim(bundle: Bundle) -> None:
    """Refuse if the pinned bundle's compatibility policy no longer contains
    the embedded comparability rule (the report must print it VERBATIM; on a
    re-pin the constant must be updated together)."""
    policy = bundle.root / "compatibility" / "compatibility-policy.md"
    if not policy.is_file():
        return  # bundle ships schemas only; embedded pinned copy is used
    text = _normalize(policy.read_text(encoding="utf-8"))
    if _normalize(COMPARABILITY_RULE_VERBATIM) not in text:
        _fail(
            "embedded comparability rule differs from the pinned bundle's "
            f"{policy} — the report must print the rule verbatim; update "
            "report.COMPARABILITY_RULE_VERBATIM (and its pin note) together "
            "with the bundle re-pin"
        )


def _pin_of(manifests: Sequence[ManifestEntry]) -> str:
    pins = []
    for m in manifests:
        p = m.manifest.get("contracts_bundle_version")
        if p and p not in pins:
            pins.append(p)
    return "; ".join(pins) if pins else "not declared in manifest"


def _arrival_info(entry: ManifestEntry) -> tuple[str, bool]:
    """(human description of the arrival process, is_closed_loop)."""
    ref = entry.manifest.get("workload_ref", {})
    ref_s = (
        f"workload_ref = {ref.get('name')}@{ref.get('version')} "
        f"seed {ref.get('seed')}"
    )
    if entry.workload is None:
        return (
            f"arrival process NOT inspectable from this input (no workload "
            f"file available beside the manifest); {ref_s}. Open-loop "
            "arrivals are NOT implied — verify against the versioned "
            "workload file before making any latency or goodput claim",
            False,
        )
    ap = entry.workload.get("arrival_process", {})
    kind = ap.get("type")
    if kind == "closed-loop":
        return (
            f"CLOSED-LOOP arrival (concurrency={ap.get('concurrency')}, "
            f"think_time={ap.get('think_time_seconds', 0)}s); {ref_s}",
            True,
        )
    if kind == "open-loop-poisson":
        if "rate_rps" in ap:
            desc = f"open-loop Poisson, rate {ap['rate_rps']} req/s"
        else:
            phases = ap.get("phases", [])
            desc = (
                f"open-loop Poisson, {len(phases)} phase(s) "
                + "; ".join(
                    f"{p['rate_rps']} req/s x {p['duration_seconds']}s"
                    for p in phases
                )
                + (", repeating" if ap.get("repeat_phases") else "")
            )
        return (f"{desc}; {ref_s}", False)
    return (f"arrival process type '{kind}' (unrecognized); {ref_s}", False)


def _table_row(signal: str, t: Mapping) -> str:
    p999 = _sec(t["p999"]) if t.get("p999") is not None else "—"
    return (
        f"| `{signal}` | {t['n']} | {_sec(t['p50'])} | {_sec(t['p90'])} | "
        f"{_sec(t['p95'])} | {_sec(t['p99'])} | {p999} | {_sec(t['max'])} | "
        f"{_sec(t['mean'])} |"
    )


# --------------------------------------------------------------------------
# model validation — the refusals that make dishonest rendering impossible
# --------------------------------------------------------------------------


def _check_model(m: ReportModel) -> None:
    if not m.manifests:
        _fail("no run manifest supplied — a report must embed the full manifest(s)")
    for entry in m.manifests:
        hyp = entry.manifest.get("hypothesis")
        if not isinstance(hyp, str) or not hyp.strip():
            _fail(
                f"manifest '{entry.path}' carries no hypothesis — the "
                "hypothesis is displayed prominently in every report and a "
                "hypothesis-less manifest is not reportable (experiments.md "
                "rule 6)"
            )
    if not isinstance(m.warm_up_handling, str) or not m.warm_up_handling.strip():
        _fail("validity block incomplete: warm_up_handling missing/empty")
    if not isinstance(m.run_count, int) or m.run_count < 1:
        _fail(f"validity block incomplete: run_count={m.run_count!r}")
    if m.threats is None or m.anomalies is None:
        _fail(
            "validity block incomplete: threats_to_validity and "
            "unexplained_anomalies must both be present (empty list = "
            "explicit 'none known', absence = refusal)"
        )
    if not m.anomalies and not m.anomaly_checks:
        _fail(
            "unexplained_anomalies is empty and no anomaly checks were "
            "declared — 'none observed' is only honest next to the list of "
            "checks that were run (experiments.md rule 9)"
        )
    g = m.goodput
    for key in ("ratio", "shed_rate", "stall_rate", "slo_id"):
        if g.get(key) is None:
            _fail(
                f"goodput block lacks '{key}' — goodput is never rendered "
                "without its SLO reference, shed rate, and stall rate "
                "adjacent (experiments.md rule 7)"
            )
    if (m.latency_tables is None) == (m.withheld is None):
        _fail(
            "exactly one of latency tables / withheld-latency explanation "
            "must be present — a report never shows a blank latency section"
        )
    if m.latency_tables is not None and not m.latency_tables:
        _fail("latency tables present but empty — not renderable")
    if m.withheld is not None:
        for key in ("kind", "reason", "error_rate", "shed_rate", "threshold"):
            if m.withheld.get(key) is None:
                _fail(
                    f"withheld-latency block lacks '{key}' — a withheld "
                    "table must always say WHY (kind, reason, rates, "
                    "declared threshold)"
                )
    if not m.repro_command.strip():
        _fail("reproduction command missing — every report names the exact "
              "command that regenerates it (experiments.md rule 8)")
    if not m.raw_event_refs or not m.manifest_refs:
        _fail("provenance links (manifests, raw events) missing")


# --------------------------------------------------------------------------
# builders
# --------------------------------------------------------------------------


def _load_workload_beside(manifest_path: str | Path, bundle: Bundle | None):
    """The generator writes workload.json next to manifest.json in every run
    directory; pick it up when present so the arrival process (and the
    closed-loop flag) can be reported."""
    p = Path(manifest_path).parent / "workload.json"
    if not p.is_file():
        return None, None
    with open(p, encoding="utf-8") as f:
        workload = json.load(f)
    if bundle is not None:
        bundle.validate("workload", workload, context=str(p))
    return workload, str(p)


def report_from_analysis(
    result: AnalysisResult,
    manifests: Sequence[ManifestEntry],
    *,
    repro_command: str,
    result_file_ref: str | None = None,
    generated_at: str | None = None,
    bundle: Bundle | None = None,
) -> str:
    """Render from an in-memory AnalysisResult (full fidelity: bootstrap
    CIs, dispersion, gate rates, withheld latency). The only publishable
    surface for valid runs whose latency is withheld."""
    if bundle is not None:
        check_comparability_verbatim(bundle)

    latency_tables = latency_ci = ci_params = withheld = None
    if isinstance(result.latency, WithheldLatency):
        w = result.latency
        withheld = {
            "kind": w.kind,
            "reason": w.reason,
            "error_rate": w.error_rate,
            "shed_rate": w.shed_rate,
            "threshold": w.threshold,
        }
    else:
        lat: LatencyTables = result.latency
        latency_tables = {}
        latency_ci = {}
        for name in _LATENCY_TABLE_ORDER:
            t = getattr(lat, name, None)
            if t is None:
                continue
            latency_tables[name] = {
                "n": t.n,
                "p50": t.p50,
                "p90": t.p90,
                "p95": t.p95,
                "p99": t.p99,
                "p999": t.p999,
                "max": t.max,
                "mean": t.mean,
            }
            if t.ci:
                latency_ci[name] = dict(t.ci)
        if not any(latency_ci.values()):
            latency_ci = None
        else:
            ci_params = (
                "nonparametric percentile bootstrap on the pooled samples "
                "(B=1000 resamples, 95% interval, seeded — ADR-0002); CIs "
                "are report-surface only, the pinned result schema carries "
                "no CI fields"
            )

    g = result.goodput
    goodput = {
        "slo_id": g.slo_id,
        "slo_version": g.slo_version,
        "ratio": g.ratio,
        "requests_per_second_meeting_slo": g.requests_per_second_meeting_slo,
        "shed_rate": g.shed_rate,
        "stall_rate": g.stall_rate,
        "stall_threshold_seconds": g.stall_threshold_seconds,
        "meeting_count": g.meeting_count,
        "offered_count": g.offered_count,
        "streaming_count": g.streaming_count,
        "stalled_count": g.stalled_count,
    }

    knee = None
    if result.knee is not None:
        knee = {
            "arrival_rate_rps": result.knee.arrival_rate_rps,
            "signal": result.knee.signal,
            "method": result.knee.method,
            "confidence": result.knee.confidence,
            "bracketed": result.knee.bracketed,
        }

    cost = cost_null_reason = None
    if isinstance(result.cost, Cost):
        c = result.cost
        cost = {
            "profile": f"{c.profile_id}@{c.profile_version}",
            "per_successful_request_usd": c.per_successful_request_usd,
            "per_million_tokens_usd": c.per_million_tokens_usd,
            "per_million_output_tokens_usd": c.per_million_output_tokens_usd,
            "usd_per_hour": c.usd_per_hour,
            "rate_provenance": dict(c.rate_provenance),
        }
    elif isinstance(result.cost, CostUnavailable):
        cost_null_reason = result.cost.reason

    gate_desc = (
        f"error rate {_rate(result.error_rate)} + shed rate "
        f"{_rate(result.shed_rate)} vs declared threshold "
        f"{result.gate_threshold:g}"
        + (" — GATE TRIPPED, latency withheld"
           if withheld is not None and withheld["kind"] == "error-shed-gate"
           else " — below threshold")
    )
    window_desc = (
        f"{result.throughput.window_seconds:.3f}s post-warm-up window, "
        f"{result.pooled_event_count} events, {result.run_count} repetition(s)"
    )
    checks = tuple(
        c.format(gate=gate_desc, window=window_desc)
        for c in ANALYSIS_ANOMALY_CHECKS
    )

    model = ReportModel(
        result_id=result.result_id,
        created_at=result.created_at,
        generated_at=generated_at or _now(),
        source=(
            "in-memory analysis of the linked raw events (no benchmark-result "
            "file exists for this run set: " + result.latency.reason + ")"
            if withheld is not None
            else "in-memory analysis of the linked raw events (same pipeline "
            "that emits the benchmark-result file; bootstrap CIs and cross-"
            "run dispersion shown here cannot ride in the result file at the "
            "pinned contracts version)"
        ),
        pin=_pin_of(manifests),
        manifests=tuple(manifests),
        throughput={
            "requests_per_second": result.throughput.requests_per_second,
            "output_tokens_per_second": result.throughput.output_tokens_per_second,
            "total_requests": result.throughput.total_requests,
            "total_output_tokens": result.throughput.total_output_tokens,
            "window_seconds": result.throughput.window_seconds,
        },
        latency_tables=latency_tables,
        latency_ci=latency_ci,
        ci_params=ci_params,
        withheld=withheld,
        dispersion=dict(result.dispersion) if result.dispersion else None,
        goodput=goodput,
        knee=knee,
        cost=cost,
        cost_null_reason=cost_null_reason,
        warm_up_handling=result.warmup.handling_statement(),
        run_count=result.run_count,
        pooled_event_count=result.pooled_event_count,
        threats=tuple(result.threats_to_validity),
        anomalies=tuple(result.unexplained_anomalies),
        anomaly_checks=checks,
        gate={
            "error_rate": result.error_rate,
            "shed_rate": result.shed_rate,
            "threshold": result.gate_threshold,
        },
        repro_command=repro_command,
        manifest_refs=tuple(result.manifest_refs),
        raw_event_refs=tuple(result.raw_event_refs),
        result_file_ref=result_file_ref,
    )
    return _render(model)


def report_from_result_dict(
    doc: Mapping,
    manifests: Sequence[ManifestEntry],
    *,
    repro_command: str,
    source_path: str,
    extra_threats: Sequence[str] = (),
    extra_anomalies: Sequence[str] = (),
    generated_at: str | None = None,
    bundle: Bundle | None = None,
) -> str:
    """Render from an emitted benchmark-result file. The caller should have
    schema-validated `doc`; the structural honesty checks are repeated here
    so a validation bypass still refuses."""
    if bundle is not None:
        check_comparability_verbatim(bundle)

    validity = doc.get("validity")
    if not isinstance(validity, Mapping):
        _fail(
            f"'{source_path}' has no validity block — a result without one "
            "is not a valid result and cannot be rendered as a report"
        )
    for key in ("warm_up_handling", "run_count", "threats_to_validity",
                "unexplained_anomalies"):
        if key not in validity:
            _fail(f"'{source_path}' validity block lacks '{key}' — refusing")

    pp = doc.get("pooled_percentiles")
    if not isinstance(pp, Mapping) or not pp.get("tables"):
        _fail(f"'{source_path}' has no pooled_percentiles.tables — refusing")
    if pp.get("method") != POOLED_METHOD:
        _fail(
            f"'{source_path}' pooled_percentiles.method is "
            f"{pp.get('method')!r}, not '{POOLED_METHOD}' — percentiles not "
            "computed on pooled raw events are not renderable"
        )
    tables = {
        name: dict(pp["tables"][name])
        for name in _LATENCY_TABLE_ORDER
        if name in pp["tables"]
    }

    g = doc.get("goodput")
    if not isinstance(g, Mapping):
        _fail(f"'{source_path}' has no goodput block — refusing")
    goodput = {
        "slo_id": g.get("slo_ref", {}).get("id"),
        "slo_version": g.get("slo_ref", {}).get("version"),
        "ratio": g.get("ratio"),
        "requests_per_second_meeting_slo": g.get("requests_per_second_meeting_slo"),
        "shed_rate": g.get("shed_rate"),
        "stall_rate": g.get("stall_rate"),
        "stall_threshold_seconds": g.get("stall_threshold_seconds"),
        "meeting_count": None,
        "offered_count": None,
        "streaming_count": None,
        "stalled_count": None,
    }

    threats = list(validity.get("threats_to_validity", []))
    anomalies = list(validity.get("unexplained_anomalies", []))
    threats += [f"{t} [added at report generation]" for t in extra_threats]
    anomalies += [f"{a} [added at report generation]" for a in extra_anomalies]

    cost = doc.get("cost")
    cost_null_reason = None
    if cost is None:
        matching = [t for t in threats if "cost is null" in t or "cost per" in t]
        cost_null_reason = (
            matching[0]
            if matching
            else "the result file carries cost: null but records no reason "
            "in threats_to_validity — this is itself a validity gap in the "
            "emitting pipeline; treat the null as unexplained"
        )
    else:
        ref = cost.get("cost_profile_ref", {})
        cost = {
            "profile": f"{ref.get('id')}@{ref.get('version')}",
            "per_successful_request_usd": cost.get("per_successful_request_usd"),
            "per_million_tokens_usd": cost.get("per_million_tokens_usd"),
            "per_million_output_tokens_usd": cost.get("per_million_output_tokens_usd"),
            "usd_per_hour": None,
            "rate_provenance": None,
        }

    # warm-up consistency: the handling statement must reflect the
    # manifest-declared policy (the analyzer embeds the policy name).
    handling = validity["warm_up_handling"]
    for entry in manifests:
        policy = (entry.manifest.get("warm_up") or {}).get("policy")
        if policy and f"'{policy}'" not in handling:
            _fail(
                f"validity.warm_up_handling ({handling!r}) does not reflect "
                f"the warm-up policy '{policy}' declared in manifest "
                f"'{entry.path}' — inconsistent artifacts, refusing"
            )

    model = ReportModel(
        result_id=doc.get("result_id", "?"),
        created_at=doc.get("created_at", "?"),
        generated_at=generated_at or _now(),
        source=(
            f"benchmark-result file `{source_path}` (schema-validated "
            "against the pinned bundle). Bootstrap CIs and cross-run "
            "dispersion are not carried by the pinned result schema; "
            "regenerate the report from the raw events for those surfaces"
        ),
        pin=_pin_of(manifests),
        manifests=tuple(manifests),
        throughput=dict(doc.get("throughput", {})),
        latency_tables=tables,
        latency_ci=None,
        ci_params=None,
        withheld=None,
        dispersion=None,
        goodput=goodput,
        knee=dict(doc["knee_estimate"]) if doc.get("knee_estimate") else None,
        cost=cost,
        cost_null_reason=cost_null_reason,
        warm_up_handling=handling,
        run_count=validity["run_count"],
        pooled_event_count=pp.get("pooled_event_count"),
        threats=tuple(threats),
        anomalies=tuple(anomalies),
        anomaly_checks=RESULT_FILE_ANOMALY_CHECKS,
        gate=None,
        repro_command=repro_command,
        manifest_refs=tuple(doc.get("links", {}).get("run_manifests", [])),
        raw_event_refs=tuple(doc.get("links", {}).get("raw_events", [])),
        result_file_ref=source_path,
    )
    return _render(model)


# --------------------------------------------------------------------------
# the renderer — fixed section order, no way to omit a section
# --------------------------------------------------------------------------


def _render(m: ReportModel) -> str:
    _check_model(m)

    arrivals = [(e, *_arrival_info(e)) for e in m.manifests]
    any_closed = any(closed for _, _, closed in arrivals)

    out: list[str] = []
    w = out.append

    # ---- title + provenance header ------------------------------------
    w(f"# Benchmark report — {m.result_id}")
    w("")
    if any_closed:
        w("> **FLAG: CLOSED-LOOP ARRIVAL CONTRIBUTES TO THIS REPORT.** "
          "Closed-loop arrival throttles offered load to service capacity, "
          "hiding queueing delay and understating tail latency under "
          "saturation (coordinated omission). Only throughput-ceiling "
          "statements may be drawn from the closed-loop contribution — no "
          "latency or goodput claim is valid from it.")
        w("")
    w("| | |")
    w("|---|---|")
    w(f"| result_id | `{m.result_id}` |")
    w(f"| result created_at | {m.created_at} |")
    w(f"| report generated_at | {m.generated_at} |")
    w(f"| contracts bundle pin | {m.pin} |")
    w(f"| generator | inferbench-analysis {ANALYSIS_VERSION} (IB-T006 honest-report machine) |")
    w(f"| repetitions pooled | {m.run_count} |")
    w("")
    w(f"**Source of the numbers:** {m.source}")
    w("")

    # ---- hypothesis (prominent, before any number) ---------------------
    w("## Hypothesis under test")
    w("")
    w("Every run manifest declares the hypothesis it was executed for; a "
      "report is only interpretable against it (experiments.md rule 6).")
    w("")
    for e in m.manifests:
        rid = e.manifest.get("run_id", e.path)
        w(f"> **{rid}:** {e.manifest['hypothesis']}")
        w("")

    # ---- interpretation rules ------------------------------------------
    w("## Interpretation rules — what may and may not be concluded")
    w("")
    w("These rules are embedded by the report generator and cannot be "
      "omitted; a reading of this report that violates them misquotes it.")
    w("")
    w(f"1. **Comparability (verbatim, {COMPARABILITY_RULE_SOURCE}):** "
      f"{COMPARABILITY_RULE_VERBATIM} No cross-hardware or cross-tool "
      "comparison may be drawn from this report.")
    w("2. **Pooled percentiles:** every percentile below is computed on the "
      f"pooled raw per-request events across all {m.run_count} "
      "repetition(s) of this run set (method `pooled-raw-events`). "
      "Percentiles are NEVER averaged across runs; cross-run dispersion, "
      "where shown, is median ± range of per-run summaries and is not a "
      "percentile table.")
    w("3. **Arrival process:** latency and goodput claims are valid only "
      "under open-loop arrivals; closed-loop contributions are flagged "
      "here and support throughput-ceiling statements only (closed-loop "
      "hides queueing delay — coordinated omission).")
    w("4. **Saturation:** no extrapolation past the saturation knee; when "
      "the knee estimate below is null, NO saturation or capacity claim "
      "may be made from this report.")
    w("5. **Goodput:** only meaningful next to its SLO reference, shed "
      "rate, and stall rate — they are printed adjacent below; quoting the "
      "goodput ratio without them misrepresents this report (a system can "
      "inflate goodput by shedding early or stalling mid-stream).")
    w("6. **Measurement points:** all latency series are CLIENT-side "
      "series measured from the scheduled send time (coordinated-omission-"
      "safe basis; contracts metrics mirror rule). Client TTFT is a "
      "different series from gateway TTFT — never conflate them.")
    w("7. **No mean-only reading:** means appear only beside full "
      "percentile columns; the mean of a latency distribution is not a "
      "summary of it.")
    w("8. **Provenance:** numbers in this report are measured (from the "
      "linked raw events) unless explicitly labeled otherwise; every "
      "external number carries basis + date where cited.")
    w("")

    # ---- manifests -------------------------------------------------------
    w("## Run manifest(s) — full, embedded")
    w("")
    w("The complete manifest of every pooled run (pins, flags, topology, "
      "hardware, warm-up policy, hypothesis). A result without its "
      "manifest is not publishable.")
    w("")
    for e, arrival, closed in arrivals:
        rid = e.manifest.get("run_id", e.path)
        w(f"### {rid}")
        w("")
        w(f"- manifest: `{e.path}`")
        if e.workload_path:
            w(f"- workload file: `{e.workload_path}`")
        flag = " — **FLAGGED: closed-loop**" if closed else ""
        w(f"- arrival process: {arrival}{flag}")
        w(f"- target topology: `{e.manifest.get('target_topology')}`")
        w("")
        w("```json")
        w(json.dumps(e.manifest, indent=2, sort_keys=False))
        w("```")
        w("")

    # ---- results ---------------------------------------------------------
    w("## Results")
    w("")
    t = m.throughput
    w("### Throughput (measured window)")
    w("")
    w("| metric | value |")
    w("|---|---|")
    w(f"| ok-requests / second | {t.get('requests_per_second', 0):.4f} |")
    w(f"| output tokens / second | {t.get('output_tokens_per_second', 0):.2f} |")
    w(f"| total requests (all statuses) | {t.get('total_requests')} |")
    w(f"| total output tokens | {t.get('total_output_tokens')} |")
    if t.get("window_seconds") is not None:
        w(f"| measured window | {t['window_seconds']:.3f} s |")
    if m.pooled_event_count is not None:
        w(f"| pooled events (post warm-up) | {m.pooled_event_count} |")
    w("")

    if m.withheld is not None:
        wd = m.withheld
        w(f"### Latency percentiles — WITHHELD ({wd['kind']})")
        w("")
        w("No latency table is shown because none exists in this analysis; "
          "rendering one would fabricate meaning. **Why:**")
        w("")
        w(f"> {wd['reason']}")
        w("")
        w("| gate accounting | value |")
        w("|---|---|")
        w(f"| error rate (measured window) | {_rate(wd['error_rate'])} |")
        w(f"| shed rate (measured window) | {_rate(wd['shed_rate'])} |")
        w(f"| declared gate threshold (error+shed) | {wd['threshold']:g} |")
        w(f"| withholding kind | `{wd['kind']}` |")
        w("")
        w("The run set itself remains VALID: its throughput, error/shed "
          "accounting, goodput-vs-SLO, and validity data above/below are "
          "real measurements. Only the latency percentile tables are "
          "meaningless and therefore absent. No benchmark-result file "
          "exists for this run set — the pinned contract requires numeric "
          "percentile tables with no null form, and emitting fabricated or "
          "gated numbers is forbidden; THIS REPORT is the publishable "
          "artifact (contracts observation recorded in "
          "docs/implementation-notes.md).")
        w("")
    else:
        w("### Latency — pooled percentiles")
        w("")
        w(f"Method: `{POOLED_METHOD}` — percentiles computed on the pooled "
          "raw per-request samples across repetitions (never averaged "
          "across runs). Seconds.")
        if any_closed:
            w("")
            w("**Reminder: closed-loop contribution flagged above — these "
              "latency figures understate queueing delay and support no "
              "latency claim.**")
        w("")
        w("| signal | n | p50 | p90 | p95 | p99 | p999 | max | mean |")
        w("|---|---|---|---|---|---|---|---|---|")
        for name, tab in m.latency_tables.items():
            w(_table_row(name, tab))
        w("")
        w("(p999 is only resolved at n ≥ 1000 pooled samples; '—' means the "
          "pool cannot support it. The mean column is context for the "
          "percentiles, never a substitute.)")
        w("")
        if m.latency_ci:
            w(f"**Bootstrap 95% confidence intervals** — {m.ci_params}:")
            w("")
            w("| signal | percentile | 95% CI (seconds) |")
            w("|---|---|---|")
            for name, cis in m.latency_ci.items():
                for label, (lo, hi) in cis.items():
                    w(f"| `{name}` | {label} | [{_sec(lo)}, {_sec(hi)}] |")
            w("")
        if m.dispersion:
            w("**Cross-run dispersion** (median ± range of per-run "
              "summaries, experiments.md rule 4 — reported beside the "
              "pooled tables, never instead of them):")
            w("")
            w("| signal (statistic) | median | min | max | per-run |")
            w("|---|---|---|---|---|")
            for key, d in m.dispersion.items():
                per_run = "; ".join(
                    f"{k}: {_sec(v)}" for k, v in d.per_run.items()
                )
                w(f"| `{key}` | {_sec(d.median)} | {_sec(d.min)} | "
                  f"{_sec(d.max)} | {per_run} |")
            w("")

    # ---- goodput: ratio + shed + stall in ONE table, always ------------
    g = m.goodput
    slo = g["slo_id"] + (f"@{g['slo_version']}" if g.get("slo_version") else "")
    w(f"### Goodput @ SLO `{slo}` — with shed and stall rates adjacent")
    w("")
    w("Shed and stall rates are part of the goodput figure, not footnotes: "
      "goodput can be gamed by shedding early, and a stream can meet its "
      "TTFT target and still stall mid-generation. All three are computed "
      "in one pass over the same measured window.")
    w("")
    counts = g.get("meeting_count") is not None
    w("| goodput block | value |")
    w("|---|---|")
    ratio_detail = (
        f" ({g['meeting_count']}/{g['offered_count']} offered)" if counts else ""
    )
    w(f"| goodput ratio (meeting / ALL offered, incl. shed+canceled+errored) "
      f"| {_rate(g['ratio'])}{ratio_detail} |")
    w(f"| requests/second meeting SLO | "
      f"{g['requests_per_second_meeting_slo']:.4f} |")
    w(f"| **shed rate (adjacent by rule)** | {_rate(g['shed_rate'])} |")
    stall_detail = ""
    if counts:
        stall_detail = (
            f" ({g['stalled_count']}/{g['streaming_count']} streaming requests)"
        )
    thr = (
        f" at stall threshold {g['stall_threshold_seconds']:g}s"
        if g.get("stall_threshold_seconds") is not None
        else ""
    )
    w(f"| **stall rate (adjacent by rule)** | "
      f"{_rate(g['stall_rate'])}{stall_detail}{thr} |")
    w("")

    # ---- knee -----------------------------------------------------------
    w("### Saturation knee")
    w("")
    if m.knee is None:
        w("`knee_estimate: null` — no rate sweep contributed to this run "
          "set, so no saturation point was measured and **no capacity or "
          "saturation claim may be made from this report** (interpretation "
          "rule 4; also listed under threats to validity).")
    else:
        k = m.knee
        w("| knee estimate | value |")
        w("|---|---|")
        w(f"| arrival rate at knee | {k['arrival_rate_rps']:g} req/s |")
        w(f"| signal | `{k['signal']}` |")
        w(f"| confidence | {k['confidence']:g} |")
        if k.get("bracketed") is not None:
            w(f"| bracketed within sweep | {k['bracketed']} |")
        w("")
        w(f"Method: {k['method']}")
    w("")

    # ---- cost -----------------------------------------------------------
    w("### Cost")
    w("")
    if m.cost is None:
        w(f"`cost: null` — **why:** {m.cost_null_reason}")
    else:
        c = m.cost
        w("| cost | value |")
        w("|---|---|")
        w(f"| cost profile | `{c['profile']}` |")
        w(f"| per successful request | "
          f"{c['per_successful_request_usd']:.8f} USD |")
        w(f"| per 1M tokens (in+out) | {c['per_million_tokens_usd']:.4f} USD |")
        if c.get("per_million_output_tokens_usd") is not None:
            w(f"| per 1M output tokens | "
              f"{c['per_million_output_tokens_usd']:.4f} USD |")
        if c.get("usd_per_hour") is not None:
            prov = c.get("rate_provenance") or {}
            w(f"| applied rate | {c['usd_per_hour']} USD/h "
              f"({prov.get('basis', '?')}, as of {prov.get('as_of', '?')}) |")
    w("")

    # ---- validity block (mandatory) --------------------------------------
    w("## Validity block (mandatory)")
    w("")
    w(f"- **Warm-up handling:** {m.warm_up_handling}")
    w(f"- **Run count / pooling statement:** {m.run_count} repetition(s) "
      "pooled; all percentile tables above are computed on the pooled raw "
      "events of these repetitions (never on averaged per-run percentiles).")
    if m.gate is not None:
        w(f"- **Declared error/shed gate:** latency percentiles are withheld "
          f"above error+shed rate {m.gate['threshold']:g}; this run set "
          f"measured error rate {_rate(m.gate['error_rate'])} + shed rate "
          f"{_rate(m.gate['shed_rate'])}.")
    else:
        w("- **Declared error/shed gate:** the pinned result schema carries "
          "no gate fields; the gate disclosure, if tripped, appears under "
          "threats to validity.")
    if any_closed:
        w("- **Closed-loop flag:** at least one contributing workload uses "
          "closed-loop arrival — see the flag at the top of this report.")
    else:
        closed_note = (
            "no contributing workload declares closed-loop arrival"
            if all(e.workload is not None for e in m.manifests)
            else "arrival process could not be inspected for every run "
            "(workload file unavailable) — open-loop is NOT implied"
        )
        w(f"- **Closed-loop flag:** {closed_note}.")
    w("")

    w("### Threats to validity (mandatory)")
    w("")
    if m.threats:
        for t_ in m.threats:
            w(f"- {t_}")
    else:
        w("- No threats recorded by the analysis beyond the automated "
          "checks listed under unexplained anomalies. (An empty list is an "
          "explicit claim, not an omission.)")
    w("")

    w("### Unexplained anomalies (mandatory — never silently empty)")
    w("")
    if m.anomalies:
        for a in m.anomalies:
            w(f"- {a}")
    else:
        w("**None observed.** We looked and found none; an anomaly-free "
          "claim is only honest next to the checks that were run:")
        w("")
        for c in m.anomaly_checks:
            w(f"- {c}")
    w("")

    # ---- reproduction ----------------------------------------------------
    w("## Reproduction — one command")
    w("")
    w("This report regenerates from the linked artifacts with exactly:")
    w("")
    w("```sh")
    w(m.repro_command)
    w("```")
    w("")
    w(f"Pinned versions: serving-contracts bundle {m.pin}; "
      f"inferbench-analysis {ANALYSIS_VERSION}.")
    if m.result_file_ref:
        w("")
        w(f"The benchmark-result file of record is `{m.result_file_ref}`; "
          "it was emitted from the linked raw events by "
          "`python3 -m inferbench_analysis analyze` (IB-T005) and self-"
          "validates against the pinned schema.")
    w("")

    # ---- links -----------------------------------------------------------
    w("## Provenance links")
    w("")
    w("- run manifests: " + ", ".join(f"`{p}`" for p in m.manifest_refs))
    w("- raw events: " + ", ".join(f"`{p}`" for p in m.raw_event_refs))
    if m.result_file_ref:
        w(f"- benchmark-result file: `{m.result_file_ref}`")
    w("")

    return "\n".join(out)
