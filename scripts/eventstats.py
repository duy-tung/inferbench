#!/usr/bin/env python3
"""Evidence-side summary of one raw-event JSONL file (IB-T004 calibration).

Prints per-run counts and client-side latency summaries (TTFT, pooled ITL,
max stall, E2E from scheduled_send_ts, send slip, cancellation points) as a
small JSON document. This is calibration/dry-run EVIDENCE tooling only —
the benchmark statistics engine (pooled percentiles, bootstrap CIs, warm-up
exclusion) is IB-T005 and supersedes this for any published result.

Usage: eventstats.py events.jsonl
"""

import json
import sys
from datetime import datetime


def parse_ts(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00")).timestamp()


def pct(sorted_vals, q):
    """Nearest-rank percentile on pre-sorted data (evidence tooling only)."""
    if not sorted_vals:
        return None
    idx = max(0, min(len(sorted_vals) - 1, round(q / 100 * (len(sorted_vals) - 1))))
    return sorted_vals[idx]


def summary(vals):
    if not vals:
        return None
    s = sorted(vals)
    return {
        "count": len(s),
        "mean": sum(s) / len(s),
        "p50": pct(s, 50),
        "p95": pct(s, 95),
        "max": s[-1],
    }


def main(path):
    counts = {}
    ttft, itl_pooled, max_stall, e2e, slip = [], [], [], [], []
    cancel_elapsed, cancel_tokens = [], []
    slip_absent = 0
    total = 0
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            ev = json.loads(line)
            total += 1
            counts[ev["status"]] = counts.get(ev["status"], 0) + 1
            if ev.get("ttft_seconds") is not None:
                ttft.append(ev["ttft_seconds"])
            itl = ev.get("itl")
            if itl and itl.get("series_seconds"):
                itl_pooled.extend(itl["series_seconds"])
                max_stall.append(itl["max_stall_seconds"])
            e2e.append(parse_ts(ev["end_ts"]) - parse_ts(ev["scheduled_send_ts"]))
            if "send_slip_seconds" in ev:
                slip.append(ev["send_slip_seconds"])
            else:
                slip_absent += 1
            cp = ev.get("cancellation_point")
            if cp:
                cancel_elapsed.append(cp["elapsed_seconds"])
                if cp.get("output_tokens_at_cancel") is not None:
                    cancel_tokens.append(cp["output_tokens_at_cancel"])

    out = {
        "file": path,
        "events": total,
        "status_counts": counts,
        "client_ttft_seconds": summary(ttft),
        "client_itl_seconds_pooled": summary(itl_pooled),
        "max_stall_seconds": summary(max_stall),
        "client_e2e_seconds": summary(e2e),
        "send_slip_seconds": summary(slip),
        "send_slip_absent_events": slip_absent,
        "cancellation_elapsed_seconds": summary(cancel_elapsed),
        "cancellation_tokens_at_cancel": summary(cancel_tokens),
    }
    json.dump(out, sys.stdout, indent=2)
    print()


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    main(sys.argv[1])
