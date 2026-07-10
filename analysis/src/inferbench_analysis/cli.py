"""CLI: analyze a run set into a benchmark-result file.

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

Exit codes:
    0  result emitted and self-validated against the pinned schema
    1  typed refusal (invalid input, methodology violation, config error)
    3  run set VALID but result not expressible as a schema-valid
       benchmark-result (latency withheld by the error/shed gate or by a
       zero-sample required signal) — details on stdout, no file written
"""

from __future__ import annotations

import argparse
import json
import sys

from .contracts import Bundle
from .cost import Cost, CostInputs, CostUnavailable
from .errors import AnalysisError, ResultNotExpressibleError
from .events import load_run
from .percentiles import BootstrapParams, PercentileTable
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


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="inferbench-analysis")
    sub = parser.add_subparsers(dest="command", required=True)
    p = sub.add_parser("analyze", help="analyze a run set into a benchmark-result")
    p.add_argument("--bundle", required=True, help="pinned contracts bundle dir")
    p.add_argument(
        "--run",
        action="append",
        required=True,
        dest="runs",
        help="run directory (manifest.json + events.jsonl); repeatable",
    )
    p.add_argument("--slo", required=True, help="slo.schema.json instance file")
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
        help="strip this prefix from manifest/raw-event link paths in the "
        "emitted result (repo-relative provenance links)",
    )
    p.add_argument("--result-id", required=True)
    p.add_argument("--out", required=True, help="output benchmark-result path")
    args = parser.parse_args(argv)

    try:
        bundle = Bundle(args.bundle)
        runs = [load_run(d, bundle) for d in args.runs]

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

        config = AnalysisConfig(
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
        result = analyze_run_set(runs, config, result_id=args.result_id)
        if args.ref_prefix:
            result = _strip_prefix(result, args.ref_prefix)
        _print_summary(result)
        result.write_benchmark_result(args.out, bundle)
        print(f"\nwrote schema-valid benchmark-result: {args.out}")
        return 0
    except ResultNotExpressibleError as e:
        print(f"\nRESULT NOT EXPRESSIBLE (run remains valid): {e}")
        return 3
    except AnalysisError as e:
        print(f"refused: {type(e).__name__}: {e}", file=sys.stderr)
        return 1


def _strip_prefix(result: AnalysisResult, prefix: str) -> AnalysisResult:
    from dataclasses import replace

    strip = lambda s: s[len(prefix) :] if s.startswith(prefix) else s  # noqa: E731
    return replace(
        result,
        manifest_refs=tuple(strip(s) for s in result.manifest_refs),
        raw_event_refs=tuple(strip(s) for s in result.raw_event_refs),
    )


if __name__ == "__main__":
    sys.exit(main())
