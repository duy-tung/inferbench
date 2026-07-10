# Charter — inferbench

## Mission

`inferbench` is the load-generation and benchmark-analysis system of the `inference-systems`
portfolio. It produces **methodologically valid, reproducible measurements** of OpenAI-compatible
inference endpoints — TTFT, ITL/TPOT, end-to-end latency, throughput, goodput@SLO, shed/stall/error
rates, saturation knees, and cost per successful request / per 1M tokens — and publishes them as
schema-valid result files and honest reports.

The repository has two halves:

- **Go load generator** — open-loop and Poisson arrivals with fixed seeds, concurrent SSE streams,
  deliberate cancellation and slow-client workloads, rate sweeps, deterministic replay of recorded
  workloads, and raw JSONL event capture.
- **Python analysis** — pooled-percentile statistics, bootstrap confidence intervals, warm-up
  exclusion, saturation-knee detection, goodput@SLO with shed and stall rates, cost calculation,
  single-variable A/B comparison, plotting, and report generation.

## Why this repository exists

Benchmark invalidity is the program's named risk **R4**: coordinated omission, uncontrolled
variables, mean-only reporting, and cherry-picked runs make inference benchmarks worthless or
misleading. `inferbench` exists to make validity *structural* — encoded in the arrival scheduler,
the statistics code, the report template, and the experiment-governance framework — rather than a
matter of discipline. Methodologically valid benchmarking is on the program's never-cut list.

## The "only load generator" rule

Program hard rule: **exactly one load-generation system exists in the portfolio, and it is this
repository.** No other repo may grow a second load generator, benchmark harness, or statistics
pipeline. If another repo needs load or measurements, it consumes this repo's CLI, result files, or
raw events — never a copy of its code. Conversely, `inferbench` owns *no* schema: workload, run
manifest, raw-event, and result schemas are owned by `serving-contracts` and consumed as a pinned
released bundle. (See `non-goals.md` for the full forbidden-edge list.)

## Independent value

`inferbench` benchmarks **any** OpenAI-compatible endpoint over the network — engine-direct
(llama.cpp, vLLM, the deterministic mock backend released by `infergate`) or with `infergate` in
front. It must work **without** `infergate`; demonstrating a complete, schema-valid run against a
non-infergate endpoint is part of this repo's Definition of Done (milestone M9). Integration with
the rest of the portfolio happens only via the API contract and result files. This repo never
imports gateway or engine source.

## Integration value

`inferbench`'s result files are the evidence backbone of the portfolio:

- **I2** — gates the local request path (`inferbench → infergate → mock`).
- **I3** — produces the first schema-valid benchmark report (`infergate → llama.cpp` on CPU).
- **I4** — measures gateway overhead (direct vs via-gateway) on GPU.
- **I6** — feeds `fleetlab`'s capacity models and closes the benchmark → capacity → deployment →
  re-benchmark loop, including publishing where the prediction was wrong.
- **I7** — measures client impact during failure campaigns.

## Positioning

This repository is portfolio evidence for the target role:

> Senior Backend / Platform Engineer capable of designing, building, benchmarking, operating, and
> reasoning about production-grade distributed AI inference infrastructure, with particular
> strength in streaming correctness, backpressure, scheduling boundaries, observability, capacity
> planning, reliability, and infrastructure orchestration.

`inferbench` specifically carries the *benchmarking* and *measurement-honesty* portions of that
claim. Its value comes from measurement and correctness evidence, not surface area: the simplest
implementation that meets the methodology spec wins.

## Acceptance (summary of Definition of Done)

1. Gate **G4** passed — methodology review of the analysis pipeline, report template, and
   experiment governance (IB-T006 + IB-T009).
2. Calibration report (IB-T007) with deltas vs reference tooling within tolerance or explained.
3. Experiment set 1 (CPU, IB-T010) and experiment set 2 (GPU, IB-T011 — or the documented CPU
   fallback deviation) published with manifests, pooled percentiles, goodput@SLO with shed rate
   adjacent, validity blocks, and one-command reproduction.
4. Independence proven against a non-infergate endpoint with schema-valid outputs.
5. CI green, including contract-compatibility validation against the pinned bundle;
   `go test -race ./...` clean.
