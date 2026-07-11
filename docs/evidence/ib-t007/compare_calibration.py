#!/usr/bin/env python3
"""IB-T007 comparison: inferbench vs the llama.cpp-based reference tooling.

Loads:
  - inferbench's own raw events (events.jsonl) from the direct-to-engine run
  - the two refclient.py pass outputs (shared / taskset-pinned)
  - llama-bench's JSON output (model-level anchor)

and emits docs/evidence/ib-t007/comparison-summary.json, the numeric basis
for calibration-reference.md. Percentiles are nearest-rank on pooled raw
samples (evidence-grade, matches scripts/eventstats.py -- this is
tool-calibration diagnostics, not a published benchmark claim, so the
IB-T005 bootstrap engine is not invoked here).

Usage: compare_calibration.py --evidence-dir docs/evidence/ib-t007
"""
import argparse
import json
import os
import sys
from datetime import datetime


def parse_ts(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00")).timestamp()


def pct(sorted_vals, q):
    if not sorted_vals:
        return None
    idx = max(0, min(len(sorted_vals) - 1, round(q / 100 * (len(sorted_vals) - 1))))
    return sorted_vals[idx]


def summary(vals):
    if not vals:
        return None
    s = sorted(vals)
    return {"count": len(s), "mean": sum(s) / len(s), "p50": pct(s, 50), "p95": pct(s, 95),
            "max": s[-1], "min": s[0]}


def load_inferbench_events(path, warmup):
    ttft, itl = [], []
    total = 0
    kept_status = {}
    with open(path, encoding="utf-8") as f:
        events = [json.loads(line) for line in f if line.strip()]
    events.sort(key=lambda e: e["scheduled_send_ts"])
    total = len(events)
    for e in events[warmup:]:
        kept_status[e["status"]] = kept_status.get(e["status"], 0) + 1
        if e.get("ttft_seconds") is not None:
            ttft.append(e["ttft_seconds"])
        series = (e.get("itl") or {}).get("series_seconds") or []
        itl.extend(series)
    return {
        "n_total_events": total,
        "n_warmup_discarded": warmup,
        "status_counts_after_warmup": kept_status,
        "ttft_seconds": summary(ttft),
        "itl_seconds_pooled": summary(itl),
        "raw_ttft": ttft,
        "raw_itl": itl,
    }


def delta_pct(a, b):
    """b relative to a, as a percentage; None if not computable."""
    if a in (None, 0) or b is None:
        return None
    return (b - a) / a * 100.0


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--evidence-dir", default=".")
    ap.add_argument("--warmup", type=int, default=5)
    ap.add_argument("--out", default=None)
    args = ap.parse_args()
    d = args.evidence_dir
    out_path = args.out or os.path.join(d, "comparison-summary.json")

    ib = load_inferbench_events(os.path.join(d, "inferbench-run", "events.jsonl"), args.warmup)
    with open(os.path.join(d, "refclient-shared.json"), encoding="utf-8") as f:
        ref_shared = json.load(f)
    with open(os.path.join(d, "refclient-pinned.json"), encoding="utf-8") as f:
        ref_pinned = json.load(f)

    llama_bench = None
    lb_path = os.path.join(d, "llama-bench.json")
    if os.path.exists(lb_path) and os.path.getsize(lb_path) > 0:
        try:
            with open(lb_path, encoding="utf-8") as f:
                llama_bench = json.load(f)
        except json.JSONDecodeError:
            llama_bench = {"parse_error": "llama-bench.json did not parse as JSON; see llama-bench.err.log"}

    # Comparison A: inferbench vs refclient-shared, both direct-to-engine,
    # same-shape workload, client-side TTFT/ITL.
    ib_ttft = ib["ttft_seconds"]
    ref_ttft = ref_shared["client_ttft_seconds"]
    ib_itl = ib["itl_seconds_pooled"]
    ref_itl = ref_shared["client_itl_seconds_pooled"]

    comparison_a = {
        "inferbench_ttft": ib_ttft,
        "refclient_shared_ttft": ref_ttft,
        "ttft_p50_delta_seconds": (ref_ttft["p50"] - ib_ttft["p50"]) if ib_ttft and ref_ttft else None,
        "ttft_p50_delta_pct_of_inferbench": delta_pct(ib_ttft["p50"], ref_ttft["p50"]) if ib_ttft and ref_ttft else None,
        "ttft_p95_delta_seconds": (ref_ttft["p95"] - ib_ttft["p95"]) if ib_ttft and ref_ttft else None,
        "ttft_p95_delta_pct_of_inferbench": delta_pct(ib_ttft["p95"], ref_ttft["p95"]) if ib_ttft and ref_ttft else None,
        "inferbench_itl_pooled": ib_itl,
        "refclient_shared_itl_pooled": ref_itl,
        "itl_p50_delta_seconds": (ref_itl["p50"] - ib_itl["p50"]) if ib_itl and ref_itl else None,
        "itl_p95_delta_seconds": (ref_itl["p95"] - ib_itl["p95"]) if ib_itl and ref_itl else None,
    }

    # Comparison B: within refclient-shared, client-measured TTFT vs the
    # server's own self-reported timings for the SAME requests (paired).
    comparison_b = {
        "client_ttft": ref_shared["client_ttft_seconds"],
        "server_prompt_ms": ref_shared["server_prompt_ms"],
        "server_predicted_ms_per_token": ref_shared["server_predicted_ms_per_token"],
        "client_minus_server_proxy_overhead_seconds": ref_shared["client_minus_server_proxy_overhead_seconds"],
    }

    # Comparison C: CPU contention -- shared (unpinned) vs taskset-pinned.
    comparison_c = {
        "shared": {
            "client_ttft": ref_shared["client_ttft_seconds"],
            "client_itl_pooled": ref_shared["client_itl_seconds_pooled"],
            "wall_seconds": ref_shared["wall_seconds"],
        },
        "pinned": {
            "client_ttft": ref_pinned["client_ttft_seconds"],
            "client_itl_pooled": ref_pinned["client_itl_seconds_pooled"],
            "wall_seconds": ref_pinned["wall_seconds"],
            "label": ref_pinned["label"],
        },
        "ttft_p50_delta_seconds_pinned_minus_shared": (
            (ref_pinned["client_ttft_seconds"]["p50"] - ref_shared["client_ttft_seconds"]["p50"])
            if ref_pinned["client_ttft_seconds"] and ref_shared["client_ttft_seconds"] else None
        ),
        "ttft_p95_delta_seconds_pinned_minus_shared": (
            (ref_pinned["client_ttft_seconds"]["p95"] - ref_shared["client_ttft_seconds"]["p95"])
            if ref_pinned["client_ttft_seconds"] and ref_shared["client_ttft_seconds"] else None
        ),
    }

    out = {
        "inferbench_run": {k: v for k, v in ib.items() if not k.startswith("raw_")},
        "refclient_shared_meta": {k: v for k, v in ref_shared.items() if k != "records"},
        "refclient_pinned_meta": {k: v for k, v in ref_pinned.items() if k != "records"},
        "llama_bench": llama_bench,
        "comparison_a_inferbench_vs_refclient": comparison_a,
        "comparison_b_client_vs_server_timings_paired": comparison_b,
        "comparison_c_cpu_contention_shared_vs_pinned": comparison_c,
    }
    with open(out_path, "w", encoding="utf-8") as f:
        json.dump(out, f, indent=2)
    print(json.dumps(out, indent=2))


if __name__ == "__main__":
    main()
