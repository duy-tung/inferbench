#!/usr/bin/env python3
"""Snapshot the gateway's Prometheus /metrics inference_ttft_seconds and
inference_queue_wait_seconds histograms, and compute an approximate
percentile via linear interpolation within the bucket straddling the target
rank -- the standard Prometheus histogram_quantile approximation, applied
by hand here since we only need a couple of scalar percentiles for the
report, not a full PromQL engine.

Usage: scrape_ttft_hist.py <metrics_url> [--diff before.json]
  - no --diff: fetch and save a raw snapshot (cumulative counts) to stdout as JSON
  - --diff before.json: fetch current, subtract 'before' bucket-for-bucket
    (delta histogram for exactly the run that happened in between), then
    print p50/p95/p99 estimates for inference_ttft_seconds and
    inference_queue_wait_seconds.
"""
import sys, json, re, urllib.request

def fetch(url):
    with urllib.request.urlopen(url, timeout=10) as r:
        return r.read().decode()

def parse_histogram(text, metric):
    # lines like: inference_ttft_seconds_bucket{le="0.025",model="...",backend="..."} 3
    buckets = {}  # le (float, or +Inf) -> cumulative count (summed over all label combos)
    count = 0.0
    for line in text.splitlines():
        if line.startswith("#"):
            continue
        if line.startswith(metric + "_bucket{"):
            m = re.search(r'le="([^"]+)"', line)
            if not m:
                continue
            le = m.group(1)
            val = float(line.rsplit(" ", 1)[1])
            le_key = float("inf") if le == "+Inf" else float(le)
            buckets[le_key] = buckets.get(le_key, 0.0) + val
        elif line.startswith(metric + "_count{") or line.startswith(metric + "_count "):
            val = float(line.rsplit(" ", 1)[1])
            count += val
    return buckets, count

def subtract(cur, prev):
    out = {}
    for le, v in cur.items():
        out[le] = v - prev.get(le, 0.0)
    return out

def quantile(buckets, q, total):
    if total <= 0:
        return None
    target = q * total
    prev_le = 0.0
    prev_cum = 0.0
    for le in sorted(buckets.keys()):
        cum = buckets[le]
        if cum >= target:
            if le == float("inf"):
                return prev_le  # can't interpolate past the last finite bucket; report its edge
            if cum == prev_cum:
                return le
            frac = (target - prev_cum) / (cum - prev_cum)
            return prev_le + frac * (le - prev_le)
        prev_le = le
        prev_cum = cum
    return prev_le

def main():
    url = sys.argv[1]
    text = fetch(url)
    ttft_buckets, ttft_count = parse_histogram(text, "inference_ttft_seconds")
    qw_buckets, qw_count = parse_histogram(text, "inference_queue_wait_seconds")
    snapshot = {
        "ttft_buckets": ttft_buckets, "ttft_count": ttft_count,
        "qw_buckets": qw_buckets, "qw_count": qw_count,
    }
    if len(sys.argv) > 2 and sys.argv[2] == "--diff":
        before = json.load(open(sys.argv[3]))
        before_ttft = {float(k): v for k, v in before["ttft_buckets"].items()}
        before_qw = {float(k): v for k, v in before["qw_buckets"].items()}
        d_ttft = subtract(ttft_buckets, before_ttft)
        d_qw = subtract(qw_buckets, before_qw)
        d_ttft_total = ttft_count - before["ttft_count"]
        d_qw_total = qw_count - before["qw_count"]
        result = {
            "gateway_ttft_seconds": {
                "n": d_ttft_total,
                "p50": quantile(d_ttft, 0.50, d_ttft_total),
                "p90": quantile(d_ttft, 0.90, d_ttft_total),
                "p95": quantile(d_ttft, 0.95, d_ttft_total),
                "p99": quantile(d_ttft, 0.99, d_ttft_total),
            },
            "gateway_queue_wait_seconds": {
                "n": d_qw_total,
                "p50": quantile(d_qw, 0.50, d_qw_total),
                "p90": quantile(d_qw, 0.90, d_qw_total),
                "p95": quantile(d_qw, 0.95, d_qw_total),
                "p99": quantile(d_qw, 0.99, d_qw_total),
            },
            "method": "prometheus histogram_quantile-style linear interpolation within the straddling bucket (bucket boundaries per infergate internal/telemetry/vocabulary.go TTFTBuckets/QueueWaitBuckets); coarse at sub-bucket resolution, reported as a cross-check against client-side pooled percentiles, not a replacement for them",
        }
        print(json.dumps(result, indent=2))
    else:
        json.dump(snapshot, sys.stdout, indent=2)

if __name__ == "__main__":
    main()
