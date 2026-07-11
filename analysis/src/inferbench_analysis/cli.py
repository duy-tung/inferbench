"""CLI: analyze a run set into a benchmark-result file, and render honest
Markdown reports.

Usage:
    python -m inferbench_analysis analyze \
        --bundle /path/to/serving-contracts-bundle \
        --run RUN_DIR [--run RUN_DIR ...] \
        --slo SLO_FILE \
        [--cost-profile FILE --hardware-profile-id ID
         [--pricing-model M] [--region R]] \
        [--gate-threshold 0.05] [--bootstrap-resamples 1000]
        [--bootstrap-seed N] [--no-bootstrap] \
        [--threat TEXT ...] [--anomaly TEXT ...] \
        [--ref-prefix PREFIX] \
        --result-id ID --out FILE.benchmark-result.json

    # report from an emitted benchmark-result file (IB-T006):
    python -m inferbench_analysis report \
        --bundle BUNDLE --result FILE.benchmark-result.json \
        [--root DIR] [--threat TEXT ...] [--anomaly TEXT ...] [--out DIR]

    # report regenerated from raw events (same flags as analyze; the ONLY
    # publishable surface for valid runs whose latency is withheld):
    python -m inferbench_analysis report \
        --bundle BUNDLE --run RUN_DIR [--run ...] --slo SLO_FILE \
        --result-id ID [analyze flags] [--out DIR]

Exit codes:
    0  analyze: result emitted + self-validated / report: rendered
    1  typed refusal (invalid input, methodology violation, config error;
       for report: any mandatory honesty element missing)
    3  analyze only: run set VALID but result not expressible as a
       schema-valid benchmark-result (latency withheld by the error/shed
       gate or by a zero-sample required signal) — details on stdout, no
       file written; use `report --run ...` to publish the run's validity
       data
"""

from __future__ import annotations

import argparse
import json
import shlex
import sys
from pathlib import Path

from .contracts import Bundle
from .cost import Cost, CostInputs, CostUnavailable
from .errors import AnalysisError, LoaderError, ResultNotExpressibleError
from .events import load_run
from .percentiles import BootstrapParams, PercentileTable
from .report import (
    ManifestEntry,
    _load_workload_beside,
    report_from_analysis,
    report_from_result_dict,
)
from .result import (
    DEFAULT_GATE_THRESHOLD,
    AnalysisConfig,
    AnalysisResult,
    LatencyTables,
    WithheldLatency,
    analyze_run_set,
)


def _fmt_table(name: str, t: PercentileTable) -> str:
    ci = ""
    if t.ci and "p99" in t.ci:
        lo, hi = t.ci["p99"]
        ci = f"  p99 95% CI [{lo:.6f}, {hi:.6f}]"
    p999 = f" p999={t.p999:.6f}" if t.p999 is not None else ""
    return (
        f"  {name}: n={t.n} p50={t.p50:.6f} p90={t.p90:.6f} p95={t.p95:.6f} "
        f"p99={t.p99:.6f}{p999} max={t.max:.6f} mean={t.mean:.6f}{ci}"
    )


def _print_summary(res: AnalysisResult) -> None:
    print(f"result_id: {res.result_id}")
    print(
        f"measured window: {res.throughput.window_seconds:.3f}s, "
        f"{res.pooled_event_count} events ({res.run_count} repetition(s)); "
        f"warm-up: {res.warmup.excluded_total} excluded"
    )
    print(
        f"throughput: {res.throughput.requests_per_second:.4f} ok-req/s, "
        f"{res.throughput.output_tokens_per_second:.2f} out-tok/s"
    )
    print(
        f"error rate {res.error_rate:.4f} + shed rate {res.shed_rate:.4f} "
        f"vs declared gate threshold {res.gate_threshold:g}"
    )
    if isinstance(res.latency, WithheldLatency):
        print(f"latency percentiles: WITHHELD ({res.latency.kind})")
        print(f"  {res.latency.reason}")
    else:
        print("pooled latency percentiles (method: pooled-raw-events):")
        lat: LatencyTables = res.latency
        print(_fmt_table("ttft_seconds", lat.ttft_seconds))
        print(_fmt_table("e2e_duration_seconds", lat.e2e_duration_seconds))
        if lat.itl_seconds:
            print(_fmt_table("itl_seconds", lat.itl_seconds))
        if lat.max_stall_seconds:
            print(_fmt_table("max_stall_seconds", lat.max_stall_seconds))
    g = res.goodput
    print(
        f"goodput@SLO[{g.slo_id}"
        + (f"@{g.slo_version}" if g.slo_version else "")
        + f"]: ratio={g.ratio:.4f} ({g.meeting_count}/{g.offered_count}), "
        f"{g.requests_per_second_meeting_slo:.4f} req/s meeting SLO | "
        f"shed_rate={g.shed_rate:.4f} | stall_rate={g.stall_rate:.4f} "
        f"(threshold {g.stall_threshold_seconds:g}s, "
        f"{g.stalled_count}/{g.streaming_count} streaming)"
    )
    if isinstance(res.cost, Cost):
        c = res.cost
        print(
            f"cost[{c.profile_id}@{c.profile_version}]: "
            f"{c.per_successful_request_usd:.8f} USD/ok-request, "
            f"{c.per_million_tokens_usd:.4f} USD/1M tokens "
            f"(rate {c.usd_per_hour}/h, {c.rate_provenance['basis']} "
            f"as of {c.rate_provenance['as_of']})"
        )
    else:
        print(f"cost: null — {res.cost.reason}")
    print(f"knee_estimate: {'null (no sweep)' if res.knee is None else res.knee}")
    print("threats_to_validity:")
    for t in res.threats_to_validity:
        print(f"  - {t}")
    print(
        "unexplained_anomalies: "
        + (
            "[] (we looked and found none)"
            if not res.unexplained_anomalies
            else ""
        )
    )
    for a in res.unexplained_anomalies:
        print(f"  - {a}")


def _add_analysis_args(p: argparse.ArgumentParser, *, required: bool) -> None:
    """Flags shared by `analyze` and `report --run ...` (the report renders
    from the same analysis pipeline; flag semantics are identical)."""
    p.add_argument(
        "--run",
        action="append",
        required=required,
        dest="runs",
        help="run directory (manifest.json + events.jsonl); repeatable",
    )
    p.add_argument("--slo", required=required, help="slo.schema.json instance file")
    p.add_argument("--result-id", required=required)
    p.add_argument("--cost-profile", help="cost-profile.schema.json instance file")
    p.add_argument("--hardware-profile-id", help="rate selector in the cost profile")
    p.add_argument("--pricing-model", choices=["on-demand", "reserved", "spot"])
    p.add_argument("--region")
    p.add_argument(
        "--gate-threshold",
        type=float,
        default=DEFAULT_GATE_THRESHOLD,
        help="declared error+shed rate above which latency percentiles are "
        f"withheld (default {DEFAULT_GATE_THRESHOLD})",
    )
    p.add_argument("--bootstrap-resamples", type=int, default=1000)
    p.add_argument("--bootstrap-seed", type=int, default=20260710)
    p.add_argument("--no-bootstrap", action="store_true")
    p.add_argument("--threat", action="append", default=[], dest="threats")
    p.add_argument("--anomaly", action="append", default=[], dest="anomalies")
    p.add_argument(
        "--ref-prefix",
        default="",
        help="strip this prefix from manifest/raw-event link paths "
        "(repo-relative provenance links)",
    )


def _config_from_args(
    args: argparse.Namespace, bundle: Bundle, parser: argparse.ArgumentParser
) -> AnalysisConfig:
    with open(args.slo, encoding="utf-8") as f:
        slo = json.load(f)
    bundle.validate("slo", slo, context=args.slo)

    cost_inputs = None
    if args.cost_profile:
        if not args.hardware_profile_id:
            parser.error("--cost-profile requires --hardware-profile-id")
        with open(args.cost_profile, encoding="utf-8") as f:
            profile = json.load(f)
        bundle.validate("cost-profile", profile, context=args.cost_profile)
        cost_inputs = CostInputs(
            profile=profile,
            hardware_profile_id=args.hardware_profile_id,
            pricing_model=args.pricing_model,
            region=args.region,
        )

    return AnalysisConfig(
        slo=slo,
        gate_threshold=args.gate_threshold,
        bootstrap=(
            None
            if args.no_bootstrap
            else BootstrapParams(
                resamples=args.bootstrap_resamples, seed=args.bootstrap_seed
            )
        ),
        cost_inputs=cost_inputs,
        extra_threats=tuple(args.threats),
        anomalies=tuple(args.anomalies),
    )


def _strip(s: str, prefix: str) -> str:
    return s[len(prefix):] if prefix and s.startswith(prefix) else s


def _cmd_analyze(args: argparse.Namespace, parser: argparse.ArgumentParser) -> int:
    bundle = Bundle(args.bundle)
    runs = [load_run(d, bundle) for d in args.runs]
    config = _config_from_args(args, bundle, parser)
    result = analyze_run_set(runs, config, result_id=args.result_id)
    if args.ref_prefix:
        result = _strip_prefix(result, args.ref_prefix)
    _print_summary(result)
    result.write_benchmark_result(args.out, bundle)
    print(f"\nwrote schema-valid benchmark-result: {args.out}")
    return 0


def _cmd_report(
    args: argparse.Namespace, parser: argparse.ArgumentParser, argv: list[str]
) -> int:
    if bool(args.result) == bool(args.runs):
        parser.error("report needs exactly one of --result FILE or --run DIR")

    bundle = Bundle(args.bundle)
    # methodology rule 8: the report names the exact command that
    # regenerates it — reconstructed from this invocation's own argv.
    repro = "python3 -m inferbench_analysis " + shlex.join(argv)

    if args.result:
        with open(args.result, encoding="utf-8") as f:
            doc = json.load(f)
        bundle.validate("benchmark-result", doc, context=args.result)
        root = Path(args.root)
        entries = []
        for link in doc["links"]["run_manifests"]:
            mp = Path(link) if Path(link).is_absolute() else root / link
            if not mp.is_file():
                raise LoaderError(
                    f"linked run manifest '{link}' not found under root "
                    f"'{root}' — a report must embed the full manifest; pass "
                    "--root pointing at the directory the result's links are "
                    "relative to"
                )
            with open(mp, encoding="utf-8") as f:
                manifest = json.load(f)
            bundle.validate("benchmark-run", manifest, context=str(mp))
            workload, wpath = _load_workload_beside(mp, bundle)
            entries.append(
                ManifestEntry(
                    path=link,
                    manifest=manifest,
                    workload=workload,
                    workload_path=(
                        _strip(wpath, str(root).rstrip("/") + "/") if wpath else None
                    ),
                )
            )
        markdown = report_from_result_dict(
            doc,
            entries,
            repro_command=repro,
            source_path=args.result,
            extra_threats=tuple(args.threats),
            extra_anomalies=tuple(args.anomalies),
            bundle=bundle,
        )
        result_id = doc["result_id"]
    else:
        if not args.slo or not args.result_id:
            parser.error("report --run requires --slo and --result-id")
        runs = [load_run(d, bundle) for d in args.runs]
        config = _config_from_args(args, bundle, parser)
        result = analyze_run_set(runs, config, result_id=args.result_id)
        if args.ref_prefix:
            result = _strip_prefix(result, args.ref_prefix)
        entries = []
        for run in runs:
            workload, wpath = _load_workload_beside(run.manifest_path, bundle)
            entries.append(
                ManifestEntry(
                    path=_strip(run.manifest_path, args.ref_prefix),
                    manifest=run.manifest,
                    workload=workload,
                    workload_path=(
                        _strip(wpath, args.ref_prefix) if wpath else None
                    ),
                )
            )
        markdown = report_from_analysis(
            result, entries, repro_command=repro, bundle=bundle
        )
        result_id = result.result_id

    if args.out:
        out = Path(args.out) / f"{result_id}.report.md"
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(markdown, encoding="utf-8")
        print(f"wrote report: {out}")
    else:
        print(markdown)
    return 0


def main(argv: list[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    parser = argparse.ArgumentParser(prog="inferbench-analysis")
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("analyze", help="analyze a run set into a benchmark-result")
    p.add_argument("--bundle", required=True, help="pinned contracts bundle dir")
    _add_analysis_args(p, required=True)
    p.add_argument("--out", required=True, help="output benchmark-result path")

    r = sub.add_parser(
        "report",
        help="render the honest Markdown report (from a benchmark-result "
        "file, or regenerated from raw events)",
    )
    r.add_argument("--bundle", required=True, help="pinned contracts bundle dir")
    r.add_argument(
        "--result",
        help="benchmark-result file to report on (mutually exclusive with --run)",
    )
    r.add_argument(
        "--root",
        default=".",
        help="directory the result file's provenance links are relative to "
        "(with --result; default .)",
    )
    _add_analysis_args(r, required=False)
    r.add_argument(
        "--out",
        help="output directory: writes <result_id>.report.md (default: stdout)",
    )

    args = parser.parse_args(argv)

    try:
        if args.command == "analyze":
            return _cmd_analyze(args, parser)
        return _cmd_report(args, parser, argv)
    except ResultNotExpressibleError as e:
        print(f"\nRESULT NOT EXPRESSIBLE (run remains valid): {e}")
        return 3
    except AnalysisError as e:
        print(f"refused: {type(e).__name__}: {e}", file=sys.stderr)
        return 1


def _strip_prefix(result: AnalysisResult, prefix: str) -> AnalysisResult:
    from dataclasses import replace

    return replace(
        result,
        manifest_refs=tuple(_strip(s, prefix) for s in result.manifest_refs),
        raw_event_refs=tuple(_strip(s, prefix) for s in result.raw_event_refs),
    )


if __name__ == "__main__":
    sys.exit(main())
