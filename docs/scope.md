# Scope — inferbench

This repository owns exactly five things. Anything not listed here is out of scope (see
`non-goals.md` for the explicit exclusion list).

## 1. Load generation (Go)

- Open-loop Poisson arrivals with fixed seeds — send times computed from the seed before the run,
  independent of responses (the coordinated-omission defense).
- Closed-loop mode, existing only to find a throughput ceiling, with a mandatory disclosure flag
  on every artifact it produces.
- Concurrent SSE streams against any OpenAI-compatible endpoint (Contract 1 subset).
- Deliberate cancellation at declared points (queued / pre-first-token / mid-stream).
- Slow-client emulation (bounded read rate).
- Rate sweeps (≥6 points, 10% → 120% of estimated capacity) and deterministic replay of recorded
  workloads.
- Raw-event capture: one JSONL record per request per `raw-event.schema.json`, streamed with
  bounded memory.
- Run-manifest capture per `benchmark-run.schema.json`, with refusal to run on incomplete
  manifests.
- Self-diagnostics: schedule-slip watchdog, client host resource sampling, typed run aborts.
- Optional side-channel engine `/metrics` polling via the Contract 4 capability mapping.

## 2. Workload suite

The eight named, versioned, seeded workloads (authored here; schema owned by contracts):
`chat-short`, `rag-long-in`, `gen-long-out`, `shared-prefix` (controlled prefix-sharing ratio),
`mixed`, `bursty`, `cancel-storm`, `slow-client`. All with controlled input/output-length
distributions (output length capped or directed — never uncontrolled), declared arrival process,
and duration or request count. These files are the canonical suite `fleetlab` consumes.

## 3. Statistics and analysis (Python)

- Pooled-percentile computation across repetitions (never averaged percentiles — enforced in code).
- Bootstrap confidence intervals; warm-up exclusion (first ≥50 requests or 60–120 s).
- Saturation-knee detection from sweep data.
- Goodput@SLO with shed rate and stall rate computed in the same pass.
- Cost per successful request and per 1M tokens from cost-profile files.
- A/B comparison restricted to single-declared-variable experiments.
- Plotting.

## 4. Controlled experiments

Hypothesis-driven, single-variable, stop-conditioned experiments (no full-matrix sweeps),
governed by the framework in `internal/experiment/` (IB-T009):

- CPU set (IB-T010): gateway overhead direct-vs-via-gateway; admission control on/off at ~5×
  capacity.
- GPU set (IB-T011, behind gate G6): `max_num_seqs`, `max_num_batched_tokens`,
  `gpu_memory_utilization` (incl. preemption onset), context length, prefix caching on/off with
  controlled prefix-sharing ratio, chunked prefill, quantization, KV-cache dtype.
- Stretch (IB-T012, kill rules apply): speculative decoding/MTP, KV offloading, SGLang comparison.

## 5. Reports and result files

- Schema-valid `benchmark-result` files (Contract 3) — the portfolio's evidence format, consumed
  by `fleetlab` and `inference-lab`.
- Reports with embedded manifest, pooled-percentile tables, goodput@SLO (shed rate adjacent,
  stall rate beside), validity block, mandatory threats-to-validity and unexplained-anomalies
  sections, and a one-command reproduction line.
- The calibration report vs reference tooling (IB-T007).

## Scope boundary in one sentence

Load generation, workloads, statistics, experiments, reports — **and nothing else**. No gateway,
no engine, no schema ownership, no capacity model, no dashboards, no Kubernetes.
