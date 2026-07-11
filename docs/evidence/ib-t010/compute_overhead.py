#!/usr/bin/env python3
"""IB-T010 cross-arm overhead/degradation computation.

Loads two arms' raw events (same workload version + seed, warm-up excluded
per the manifests' declared policy), pools across repetitions, and reports:

  1. pooled-percentile DELTAS (arm B pooled pN - arm A pooled pN) -- the
     difference of the two arms' pooled percentile tables (matches the
     benchmark-result files);
  2. PAIRED per-request deltas (same seed => same workload items in both
     arms => event i in arm B pairs with event i in arm A): the full
     distribution of (B.ttft - A.ttft) per item, and its percentiles.
     This is the stronger per-request basis for a "gateway adds <= X ms"
     claim; it exists only because compare arms share the workload byte
     for byte.

Percentiles use the same Hyndman-Fan 7 method as the analysis package
(numpy.quantile default), on pooled raw samples -- never averaged across
repetitions.

Usage:
  compute_overhead.py --a DIR --b DIR --reps 3 --warmup-requests N \
      [--label-a direct --label-b gateway] [--signal ttft_seconds] --out FILE
"""
import argparse
import json
import math
import sys

import numpy as np


def load_arm(arm_dir: str, reps: int, warmup: int, signal: str):
    pooled = []
    per_rep = {}
    paired = {}  # workload_item -> value (only valid within one repetition; key by (rep, item))
    for rep in range(1, reps + 1):
        evs = []
        with open(f"{arm_dir}/rep-{rep}/events.jsonl", encoding="utf-8") as f:
            for line in f:
                evs.append(json.loads(line))
        evs.sort(key=lambda e: e["scheduled_send_ts"])
        kept = evs[warmup:]
        vals = []
        for e in kept:
            v = e.get(signal)
            if signal == "ttft_seconds":
                v = e.get("ttft_seconds")
            if v is None:
                continue
            vals.append(v)
            paired[(rep, e["workload_item"])] = v
        per_rep[rep] = vals
        pooled.extend(vals)
    return pooled, per_rep, paired


def pcts(vals):
    if not vals:
        return None
    a = np.asarray(vals, dtype=float)
    return {
        "n": int(a.size),
        "p50": float(np.quantile(a, 0.50)),
        "p90": float(np.quantile(a, 0.90)),
        "p95": float(np.quantile(a, 0.95)),
        "p99": float(np.quantile(a, 0.99)),
        "max": float(a.max()),
        "mean": float(a.mean()),
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--a", required=True, help="baseline arm dir (rep-N/events.jsonl)")
    ap.add_argument("--b", required=True, help="comparison arm dir")
    ap.add_argument("--label-a", default="a")
    ap.add_argument("--label-b", default="b")
    ap.add_argument("--reps", type=int, default=3)
    ap.add_argument("--reps-a", type=int, default=None)
    ap.add_argument("--reps-b", type=int, default=None)
    ap.add_argument("--warmup-requests", type=int, required=True)
    ap.add_argument("--signal", default="ttft_seconds")
    ap.add_argument("--paired", action="store_true",
                    help="also compute paired per-item deltas (requires same workload+seed and same rep count in both arms)")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    reps_a = args.reps_a or args.reps
    reps_b = args.reps_b or args.reps
    pooled_a, per_rep_a, paired_a = load_arm(args.a, reps_a, args.warmup_requests, args.signal)
    pooled_b, per_rep_b, paired_b = load_arm(args.b, reps_b, args.warmup_requests, args.signal)

    ta, tb = pcts(pooled_a), pcts(pooled_b)
    out = {
        "signal": args.signal,
        "warmup_requests_excluded_per_rep": args.warmup_requests,
        "pooling": "raw per-request samples pooled across repetitions (never averaged percentiles); percentiles = numpy.quantile (Hyndman-Fan 7), same method as the analysis package",
        args.label_a: {"pooled": ta,
                       "per_rep_p95": {r: (float(np.quantile(v, 0.95)) if v else None) for r, v in per_rep_a.items()},
                       "per_rep_p99": {r: (float(np.quantile(v, 0.99)) if v else None) for r, v in per_rep_a.items()}},
        args.label_b: {"pooled": tb,
                       "per_rep_p95": {r: (float(np.quantile(v, 0.95)) if v else None) for r, v in per_rep_b.items()},
                       "per_rep_p99": {r: (float(np.quantile(v, 0.99)) if v else None) for r, v in per_rep_b.items()}},
        "pooled_percentile_deltas_b_minus_a": {
            k: (tb[k] - ta[k]) for k in ("p50", "p90", "p95", "p99", "mean")
        },
        "pooled_percentile_ratios_b_over_a": {
            k: (tb[k] / ta[k] if ta[k] else None) for k in ("p50", "p95", "p99")
        },
    }

    if args.paired:
        keys = sorted(set(paired_a) & set(paired_b))
        deltas = [paired_b[k] - paired_a[k] for k in keys]
        dp = pcts(deltas)
        neg = sum(1 for d in deltas if d < 0)
        out["paired_per_request_delta_b_minus_a"] = {
            "n_pairs": len(deltas),
            "note": "same workload version+seed in both arms => workload item i is the same prompt/length/schedule offset in both; delta = B.ttft - A.ttft per (repetition, item). Negative deltas mean the item was FASTER via B than via A on that repetition (run-to-run noise).",
            "percentiles": dp,
            "negative_fraction": neg / len(deltas) if deltas else None,
        }

    json.dump(out, open(args.out, "w"), indent=2)
    print(json.dumps(out, indent=2))


if __name__ == "__main__":
    main()
