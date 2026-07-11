#!/usr/bin/env python3
"""Derive a measured-basis model-serving SLO file from a set of run dirs
(slo.schema.json requires provenance.basis == "measured" for model-serving
scope). Thresholds = measured maxima across ALL given runs (unfiltered,
including warm-up -- the loosest honest bound), rounded up by the given
headroom factor. Used by IB-T010 for the E1 llama.cpp and E2 SLO files.

Usage: make_slo_from_events.py --slo-id ID --applies-to TEXT --out FILE \
          [--headroom 1.35] RUN_DIR...
"""
import argparse
import glob
import json
import math
import sys
from datetime import date


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--slo-id", required=True)
    ap.add_argument("--applies-to", required=True)
    ap.add_argument("--description", default="")
    ap.add_argument("--headroom", type=float, default=1.35)
    ap.add_argument("--out", required=True)
    ap.add_argument("run_dirs", nargs="+")
    args = ap.parse_args()

    max_ttft = 0.0
    max_e2e = 0.0
    max_stall = 0.0
    n = 0
    for d in args.run_dirs:
        for path in glob.glob(f"{d}/events.jsonl") + glob.glob(f"{d}/rep-*/events.jsonl"):
            with open(path, encoding="utf-8") as f:
                for line in f:
                    ev = json.loads(line)
                    n += 1
                    if ev.get("ttft_seconds") is not None:
                        max_ttft = max(max_ttft, ev["ttft_seconds"])
                    if ev.get("end_ts") and ev.get("scheduled_send_ts") is not None and ev.get("ttft_seconds") is not None:
                        pass  # e2e derived below from itl if present
                    itl = ev.get("itl")
                    if itl and itl.get("max_stall_seconds") is not None:
                        max_stall = max(max_stall, itl["max_stall_seconds"])
                    # e2e: end_ts - scheduled_send_ts is what the analyzer measures;
                    # approximate from timestamps
                    try:
                        from datetime import datetime
                        st = datetime.fromisoformat(ev["scheduled_send_ts"].replace("Z", "+00:00"))
                        en = datetime.fromisoformat(ev["end_ts"].replace("Z", "+00:00"))
                        max_e2e = max(max_e2e, (en - st).total_seconds())
                    except Exception:
                        pass

    if n == 0:
        sys.exit("no events found")

    def up(x):
        # round up to 2 significant-ish decimals after headroom
        v = x * args.headroom
        return math.ceil(v * 20) / 20  # nearest 0.05 upward

    today = date.today().isoformat()
    src = f"inferbench {', '.join(args.run_dirs)} raw events ({n} events scanned, unfiltered including warm-up)"
    slo = {
        "slo_id": args.slo_id,
        "version": "1.0.0",
        "scope": "model-serving",
        "description": args.description or (
            f"Measured-basis SLO derived from the maxima of {args.applies_to}; exists so "
            "goodput@SLO can be reported with shed+stall adjacent. Not a production target."),
        "applies_to": {"workload_ref": args.applies_to},
        "objectives": [
            {
                "signal": "ttft_seconds", "statistic": "max", "comparator": "<=",
                "threshold": up(max_ttft), "unit": "seconds",
                "provenance": {"basis": "measured", "as_of": today, "source": src,
                               "notes": f"threshold = measured max {max_ttft:.3f}s x {args.headroom:g} headroom, rounded up to 0.05 grid"},
            },
            {
                "signal": "e2e_duration_seconds", "statistic": "max", "comparator": "<=",
                "threshold": up(max_e2e), "unit": "seconds",
                "provenance": {"basis": "measured", "as_of": today, "source": src,
                               "notes": f"threshold = measured max {max_e2e:.3f}s x {args.headroom:g} headroom, rounded up to 0.05 grid"},
            },
            {
                "signal": "max_stall_seconds", "statistic": "max", "comparator": "<=",
                "threshold": up(max_stall), "unit": "seconds",
                "provenance": {"basis": "measured", "as_of": today, "source": src,
                               "notes": f"threshold = measured max {max_stall:.3f}s x {args.headroom:g} headroom, rounded up to 0.05 grid"},
            },
        ],
        "notes": ("Per-request evaluation: statistic 'max' objectives read as per-request bounds. "
                  "Derived from the named runs' own events (measured basis, slo.schema.json rule); "
                  "re-derive when the target or topology changes."),
    }
    json.dump(slo, open(args.out, "w"), indent=2)
    print(f"wrote {args.out}: ttft<={slo['objectives'][0]['threshold']}s "
          f"e2e<={slo['objectives'][1]['threshold']}s stall<={slo['objectives'][2]['threshold']}s (n={n})")


if __name__ == "__main__":
    main()
