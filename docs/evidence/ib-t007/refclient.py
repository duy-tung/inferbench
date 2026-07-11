#!/usr/bin/env python3
"""IB-T007 CPU calibration reference client (independent of inferbench).

Purpose: a minimal, deliberately SEPARATE implementation of the client-side
TTFT/ITL measurement that inferbench (internal/client/client.go) performs,
hitting llama-server's OpenAI-compatible streaming chat endpoint directly.
It shares no code with inferbench's Go client -- agreement between the two
is therefore not an artifact of a shared bug, which is the whole point of
IB-T007 ("prove the generator's numbers are not an artifact of the
generator").

For every request it captures, from the SAME HTTP response (paired, not a
separate request):
  - client_ttft_seconds:  dispatch instant (immediately before send) ->
    first response byte belonging to a content-bearing SSE delta. Mirrors
    inferbench's scheduled_send_ts -> first-body-byte definition
    (internal/client/client.go doc comment); since this script issues
    requests sequentially (see below), "dispatch instant" and "scheduled
    send instant" coincide (no queue to slip against).
  - client_itl_seconds: gaps between successive content-bearing SSE
    deltas, stamped at arrival before JSON parsing (mirrors inferbench's
    per-chunk stamping order).
  - server_timings: the `timings` object llama-server attaches to the
    FINAL SSE chunk of a chat.completion.chunk stream once
    stream_options.include_usage=true (empty-choices usage chunk) --
    prompt_n/prompt_ms/prompt_per_second, predicted_n/predicted_ms/
    predicted_per_second. This is the server's own self-reported timing,
    read from the wire of the exact same request/response this script
    just measured client-side -- there is no second request involved.

Arrival process: sequential/closed-loop (send request i+1 only once
request i has fully completed). This is a DELIBERATE, DOCUMENTED
difference from inferbench's open-loop-Poisson schedule (see the
differences table in docs/evidence/ib-t007/calibration-reference.md).
Against a single-slot (`-np 1`) llama-server, inferbench's low offered
rate already keeps at most one request in flight in practice, so the two
arrival models converge on "N independent single-request measurements"
even though they are not the same code path.

Temperature is pinned to 0 (greedy decoding) with a fixed sampler seed so
runs are reproducible and predicted_n is not itself a source of
run-to-run variance (a workload knob under this script's control, unlike
inferbench's stochastic-length workload -- documented as a difference).

Usage:
  refclient.py --base-url http://127.0.0.1:PORT --model NAME \
      --n 30 --warmup 3 --seed 1007002 \
      --input-min-words 20 --input-max-words 60 \
      --output-min-tokens 16 --output-max-tokens 40 \
      --timeout 60 --label shared --out OUT.json
"""
import argparse
import json
import random
import sys
import time

import requests

VOCAB = (
    "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu "
    "xi omicron pi rho sigma tau upsilon phi chi psi omega orbit river signal "
    "vector cache thread kernel packet queue socket buffer stream matrix "
    "tensor gradient lattice photon proton neutron helix cipher meridian "
    "cascade harbor lantern compass ember granite willow"
).split()


def make_prompt(rng, n_words):
    words = [rng.choice(VOCAB) for _ in range(n_words)]
    return "Continue this list with five more related words, one per line: " + " ".join(words)


def pct(sorted_vals, q):
    """Nearest-rank percentile on pre-sorted data (evidence-grade, matches
    scripts/eventstats.py's method used throughout this repo's calibration
    evidence -- not the IB-T005 bootstrap engine, which is for published
    benchmark claims, not tool-calibration diagnostics)."""
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
        "min": s[0],
    }


def one_request(session, base_url, model, prompt, max_tokens, timeout, sampler_seed):
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": max_tokens,
        "stream": True,
        "stream_options": {"include_usage": True},
        "temperature": 0.0,
        "seed": sampler_seed,
    }
    t_dispatch = time.perf_counter()
    ttft = None
    last_chunk_t = None
    itls = []
    n_content_chunks = 0
    server_timings = None
    usage = None
    err = None
    try:
        with session.post(f"{base_url}/v1/chat/completions", json=payload,
                           stream=True, timeout=timeout) as resp:
            resp.raise_for_status()
            for raw_line in resp.iter_lines(decode_unicode=True):
                now = time.perf_counter()
                if not raw_line or not raw_line.startswith("data: "):
                    continue
                data = raw_line[len("data: "):]
                if data.strip() == "[DONE]":
                    break
                obj = json.loads(data)
                choices = obj.get("choices", [])
                if choices:
                    delta = choices[0].get("delta", {})
                    content = delta.get("content")
                    if content:
                        if ttft is None:
                            ttft = now - t_dispatch
                        else:
                            itls.append(now - last_chunk_t)
                        last_chunk_t = now
                        n_content_chunks += 1
                if not choices and "usage" in obj:
                    usage = obj["usage"]
                if "timings" in obj:
                    server_timings = obj["timings"]
    except Exception as e:  # classified minimally; never retried (matches ADR-0001)
        err = f"{type(e).__name__}: {e}"
    t_end = time.perf_counter()
    return {
        "ttft_seconds": ttft,
        "e2e_seconds": (t_end - t_dispatch) if ttft is not None else None,
        "itl_seconds": itls,
        "n_content_chunks": n_content_chunks,
        "usage": usage,
        "server_timings": server_timings,
        "error": err,
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", required=True)
    ap.add_argument("--model", required=True)
    ap.add_argument("--n", type=int, required=True, help="total requests including warm-up")
    ap.add_argument("--warmup", type=int, default=3)
    ap.add_argument("--seed", type=int, required=True, help="seeds this script's own prompt/length RNG")
    ap.add_argument("--sampler-seed", type=int, default=20260711)
    ap.add_argument("--input-min-words", type=int, default=20)
    ap.add_argument("--input-max-words", type=int, default=60)
    ap.add_argument("--output-min-tokens", type=int, default=16)
    ap.add_argument("--output-max-tokens", type=int, default=40)
    ap.add_argument("--timeout", type=float, default=60.0)
    ap.add_argument("--label", required=True, help="run label, e.g. shared / taskset-pinned")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    rng = random.Random(args.seed)
    session = requests.Session()

    records = []
    t_pass_start = time.perf_counter()
    for i in range(args.n):
        n_words = rng.randint(args.input_min_words, args.input_max_words)
        max_tokens = rng.randint(args.output_min_tokens, args.output_max_tokens)
        prompt = make_prompt(rng, n_words)
        rec = one_request(session, args.base_url, args.model, prompt, max_tokens,
                           args.timeout, args.sampler_seed)
        rec["index"] = i
        rec["is_warmup"] = i < args.warmup
        rec["prompt_words"] = n_words
        rec["max_tokens"] = max_tokens
        records.append(rec)
        print(f"[{args.label}] req {i+1}/{args.n} ttft={rec['ttft_seconds']} "
              f"err={rec['error']}", file=sys.stderr)
    t_pass_end = time.perf_counter()

    kept = [r for r in records if not r["is_warmup"] and r["error"] is None]
    ttft_vals = [r["ttft_seconds"] for r in kept if r["ttft_seconds"] is not None]
    itl_vals = []
    for r in kept:
        itl_vals.extend(r["itl_seconds"])

    overhead_vals = []  # client_ttft - (server prompt_ms + avg decode ms/token)/1000
    server_prompt_ms = []
    server_predicted_ms_per_tok = []
    for r in kept:
        st = r.get("server_timings")
        if not st or r["ttft_seconds"] is None:
            continue
        prompt_ms = st.get("prompt_ms")
        predicted_ms = st.get("predicted_ms")
        predicted_n = st.get("predicted_n")
        if prompt_ms is None:
            continue
        server_prompt_ms.append(prompt_ms)
        proxy_first_token_s = prompt_ms / 1000.0
        if predicted_ms and predicted_n:
            per_tok_ms = predicted_ms / predicted_n
            server_predicted_ms_per_tok.append(per_tok_ms)
            proxy_first_token_s += per_tok_ms / 1000.0
        overhead_vals.append(r["ttft_seconds"] - proxy_first_token_s)

    out = {
        "label": args.label,
        "base_url": args.base_url,
        "model": args.model,
        "n_total": args.n,
        "n_warmup": args.warmup,
        "n_kept": len(kept),
        "n_errors": sum(1 for r in records if r["error"] is not None),
        "seed": args.seed,
        "sampler_seed": args.sampler_seed,
        "wall_seconds": t_pass_end - t_pass_start,
        "client_ttft_seconds": summary(ttft_vals),
        "client_itl_seconds_pooled": summary(itl_vals),
        "server_prompt_ms": summary(server_prompt_ms),
        "server_predicted_ms_per_token": summary(server_predicted_ms_per_tok),
        "client_minus_server_proxy_overhead_seconds": summary(overhead_vals),
        "records": records,
    }
    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(out, f, indent=2)
    print(f"[{args.label}] wrote {args.out}: n_kept={len(kept)} "
          f"ttft_p50={out['client_ttft_seconds'] and out['client_ttft_seconds']['p50']}",
          file=sys.stderr)


if __name__ == "__main__":
    main()
