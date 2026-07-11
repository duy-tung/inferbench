# Benchmark report — ib-t010-e1-mock-direct

| | |
|---|---|
| result_id | `ib-t010-e1-mock-direct` |
| result created_at | 2026-07-11T11:11:33Z |
| report generated_at | 2026-07-11T15:12:42Z |
| contracts bundle pin | v0.2.0 |
| generator | inferbench-analysis 0.2.0 (IB-T006 honest-report machine) |
| repetitions pooled | 3 |

**Source of the numbers:** benchmark-result file `docs/evidence/ib-t010/results/ib-t010-e1-mock-direct.benchmark-result.json` (schema-validated against the pinned bundle). Bootstrap CIs and cross-run dispersion are not carried by the pinned result schema; regenerate the report from the raw events for those surfaces

## Hypothesis under test

Every run manifest declares the hypothesis it was executed for; a report is only interpretable against it (experiments.md rule 6).

> **exp-EXP-ib-t010-e1-gateway-overhead-direct-r1:** Routing chat-short-shaped traffic through infergate (via-gateway/gateway-mock) instead of directly at the engine (engine-direct) adds non-queue latency overhead whose pooled p95 is <10ms and pooled p99 is <20ms (the program SLO target, docs/tasks.md IB-T010 hypothesis (a)), falsifiable against LiteLLM's self-reported ~8ms p95 gateway overhead as a source-reported baseline (as of 2026-07 -- re-verify at use time). The expected direction is 'gateway adds a small, bounded amount of latency, not zero and not an order of magnitude more'; the run is designed at low offered rate relative to estimated capacity so queueing delay is negligible and the measured delta approximates non-queue overhead.

> **exp-EXP-ib-t010-e1-gateway-overhead-direct-r2:** Routing chat-short-shaped traffic through infergate (via-gateway/gateway-mock) instead of directly at the engine (engine-direct) adds non-queue latency overhead whose pooled p95 is <10ms and pooled p99 is <20ms (the program SLO target, docs/tasks.md IB-T010 hypothesis (a)), falsifiable against LiteLLM's self-reported ~8ms p95 gateway overhead as a source-reported baseline (as of 2026-07 -- re-verify at use time). The expected direction is 'gateway adds a small, bounded amount of latency, not zero and not an order of magnitude more'; the run is designed at low offered rate relative to estimated capacity so queueing delay is negligible and the measured delta approximates non-queue overhead.

> **exp-EXP-ib-t010-e1-gateway-overhead-direct-r3:** Routing chat-short-shaped traffic through infergate (via-gateway/gateway-mock) instead of directly at the engine (engine-direct) adds non-queue latency overhead whose pooled p95 is <10ms and pooled p99 is <20ms (the program SLO target, docs/tasks.md IB-T010 hypothesis (a)), falsifiable against LiteLLM's self-reported ~8ms p95 gateway overhead as a source-reported baseline (as of 2026-07 -- re-verify at use time). The expected direction is 'gateway adds a small, bounded amount of latency, not zero and not an order of magnitude more'; the run is designed at low offered rate relative to estimated capacity so queueing delay is negligible and the measured delta approximates non-queue overhead.

## Interpretation rules — what may and may not be concluded

These rules are embedded by the report generator and cannot be omitted; a reading of this report that violates them misquotes it.

1. **Comparability (verbatim, serving-contracts compatibility/compatibility-policy.md §7, pin 8d81492 (v0.2.0 tag pending)):** results are comparable only when model revision, quantization, tokenizer, engine version+flags, hardware, driver/CUDA, workload version+seed, and warm-up policy all match, **or** the difference is the single declared experimental variable. No cross-hardware or cross-tool comparison may be drawn from this report.
2. **Pooled percentiles:** every percentile below is computed on the pooled raw per-request events across all 3 repetition(s) of this run set (method `pooled-raw-events`). Percentiles are NEVER averaged across runs; cross-run dispersion, where shown, is median ± range of per-run summaries and is not a percentile table.
3. **Arrival process:** latency and goodput claims are valid only under open-loop arrivals; closed-loop contributions are flagged here and support throughput-ceiling statements only (closed-loop hides queueing delay — coordinated omission).
4. **Saturation:** no extrapolation past the saturation knee; when the knee estimate below is null, NO saturation or capacity claim may be made from this report.
5. **Goodput:** only meaningful next to its SLO reference, shed rate, and stall rate — they are printed adjacent below; quoting the goodput ratio without them misrepresents this report (a system can inflate goodput by shedding early or stalling mid-stream).
6. **Measurement points:** all latency series are CLIENT-side series measured from the scheduled send time (coordinated-omission-safe basis; contracts metrics mirror rule). Client TTFT is a different series from gateway TTFT — never conflate them.
7. **No mean-only reading:** means appear only beside full percentile columns; the mean of a latency distribution is not a summary of it.
8. **Provenance:** numbers in this report are measured (from the linked raw events) unless explicitly labeled otherwise; every external number carries basis + date where cited.

## Run manifest(s) — full, embedded

The complete manifest of every pooled run (pins, flags, topology, hardware, warm-up policy, hypothesis). A result without its manifest is not publishable.

### exp-EXP-ib-t010-e1-gateway-overhead-direct-r1

- manifest: `docs/evidence/ib-t010/e1-mock-compare/direct/rep-1/manifest.json`
- workload file: `docs/evidence/ib-t010/e1-mock-compare/direct/rep-1/workload.json`
- arrival process: open-loop Poisson, rate 6 req/s; workload_ref = ib-t010-e1-mock@1.0.0 seed 10010101
- target topology: `engine-direct`

```json
{
  "run_id": "exp-EXP-ib-t010-e1-gateway-overhead-direct-r1",
  "target_topology": "engine-direct",
  "workload_ref": {
    "name": "ib-t010-e1-mock",
    "version": "1.0.0",
    "seed": 10010101
  },
  "engine": {
    "name": "mock",
    "version": "dev",
    "commit": "6827d8c3d177464c17fae3b4dc6c2c475323333b",
    "flags": {
      "error_rate": 0,
      "itl_ms": 15,
      "seed": 42,
      "ttft_ms": 300
    }
  },
  "model": {
    "checkpoint": "mock-8b",
    "revision": "mockengine@6827d8c",
    "tokenizer": "mockengine-estimator"
  },
  "hardware": {
    "gpu_model": null,
    "gpu_count": 0,
    "vram_gb": null,
    "driver_version": null,
    "cuda_version": null,
    "instance_type": "local-dev-container (linux/amd64, 4 vCPU, CPU-only)"
  },
  "client": {
    "location": "same-host (loopback)",
    "rtt_ms": 1.172397
  },
  "warm_up": {
    "policy": "discard-requests",
    "value": 50
  },
  "repetitions": 3,
  "hypothesis": "Routing chat-short-shaped traffic through infergate (via-gateway/gateway-mock) instead of directly at the engine (engine-direct) adds non-queue latency overhead whose pooled p95 is <10ms and pooled p99 is <20ms (the program SLO target, docs/tasks.md IB-T010 hypothesis (a)), falsifiable against LiteLLM's self-reported ~8ms p95 gateway overhead as a source-reported baseline (as of 2026-07 -- re-verify at use time). The expected direction is 'gateway adds a small, bounded amount of latency, not zero and not an order of magnitude more'; the run is designed at low offered rate relative to estimated capacity so queueing delay is negligible and the measured delta approximates non-queue overhead.",
  "started_at": "2026-07-11T11:05:32Z",
  "contracts_bundle_version": "v0.2.0"
}
```

### exp-EXP-ib-t010-e1-gateway-overhead-direct-r2

- manifest: `docs/evidence/ib-t010/e1-mock-compare/direct/rep-2/manifest.json`
- workload file: `docs/evidence/ib-t010/e1-mock-compare/direct/rep-2/workload.json`
- arrival process: open-loop Poisson, rate 6 req/s; workload_ref = ib-t010-e1-mock@1.0.0 seed 10010101
- target topology: `engine-direct`

```json
{
  "run_id": "exp-EXP-ib-t010-e1-gateway-overhead-direct-r2",
  "target_topology": "engine-direct",
  "workload_ref": {
    "name": "ib-t010-e1-mock",
    "version": "1.0.0",
    "seed": 10010101
  },
  "engine": {
    "name": "mock",
    "version": "dev",
    "commit": "6827d8c3d177464c17fae3b4dc6c2c475323333b",
    "flags": {
      "error_rate": 0,
      "itl_ms": 15,
      "seed": 42,
      "ttft_ms": 300
    }
  },
  "model": {
    "checkpoint": "mock-8b",
    "revision": "mockengine@6827d8c",
    "tokenizer": "mockengine-estimator"
  },
  "hardware": {
    "gpu_model": null,
    "gpu_count": 0,
    "vram_gb": null,
    "driver_version": null,
    "cuda_version": null,
    "instance_type": "local-dev-container (linux/amd64, 4 vCPU, CPU-only)"
  },
  "client": {
    "location": "same-host (loopback)",
    "rtt_ms": 0.529013
  },
  "warm_up": {
    "policy": "discard-requests",
    "value": 50
  },
  "repetitions": 3,
  "hypothesis": "Routing chat-short-shaped traffic through infergate (via-gateway/gateway-mock) instead of directly at the engine (engine-direct) adds non-queue latency overhead whose pooled p95 is <10ms and pooled p99 is <20ms (the program SLO target, docs/tasks.md IB-T010 hypothesis (a)), falsifiable against LiteLLM's self-reported ~8ms p95 gateway overhead as a source-reported baseline (as of 2026-07 -- re-verify at use time). The expected direction is 'gateway adds a small, bounded amount of latency, not zero and not an order of magnitude more'; the run is designed at low offered rate relative to estimated capacity so queueing delay is negligible and the measured delta approximates non-queue overhead.",
  "started_at": "2026-07-11T11:06:17Z",
  "contracts_bundle_version": "v0.2.0"
}
```

### exp-EXP-ib-t010-e1-gateway-overhead-direct-r3

- manifest: `docs/evidence/ib-t010/e1-mock-compare/direct/rep-3/manifest.json`
- workload file: `docs/evidence/ib-t010/e1-mock-compare/direct/rep-3/workload.json`
- arrival process: open-loop Poisson, rate 6 req/s; workload_ref = ib-t010-e1-mock@1.0.0 seed 10010101
- target topology: `engine-direct`

```json
{
  "run_id": "exp-EXP-ib-t010-e1-gateway-overhead-direct-r3",
  "target_topology": "engine-direct",
  "workload_ref": {
    "name": "ib-t010-e1-mock",
    "version": "1.0.0",
    "seed": 10010101
  },
  "engine": {
    "name": "mock",
    "version": "dev",
    "commit": "6827d8c3d177464c17fae3b4dc6c2c475323333b",
    "flags": {
      "error_rate": 0,
      "itl_ms": 15,
      "seed": 42,
      "ttft_ms": 300
    }
  },
  "model": {
    "checkpoint": "mock-8b",
    "revision": "mockengine@6827d8c",
    "tokenizer": "mockengine-estimator"
  },
  "hardware": {
    "gpu_model": null,
    "gpu_count": 0,
    "vram_gb": null,
    "driver_version": null,
    "cuda_version": null,
    "instance_type": "local-dev-container (linux/amd64, 4 vCPU, CPU-only)"
  },
  "client": {
    "location": "same-host (loopback)",
    "rtt_ms": 0.74111
  },
  "warm_up": {
    "policy": "discard-requests",
    "value": 50
  },
  "repetitions": 3,
  "hypothesis": "Routing chat-short-shaped traffic through infergate (via-gateway/gateway-mock) instead of directly at the engine (engine-direct) adds non-queue latency overhead whose pooled p95 is <10ms and pooled p99 is <20ms (the program SLO target, docs/tasks.md IB-T010 hypothesis (a)), falsifiable against LiteLLM's self-reported ~8ms p95 gateway overhead as a source-reported baseline (as of 2026-07 -- re-verify at use time). The expected direction is 'gateway adds a small, bounded amount of latency, not zero and not an order of magnitude more'; the run is designed at low offered rate relative to estimated capacity so queueing delay is negligible and the measured delta approximates non-queue overhead.",
  "started_at": "2026-07-11T11:07:01Z",
  "contracts_bundle_version": "v0.2.0"
}
```

## Results

### Throughput (measured window)

| metric | value |
|---|---|
| ok-requests / second | 5.8091 |
| output tokens / second | 85.84 |
| total requests (all statuses) | 630 |
| total output tokens | 9309 |
| pooled events (post warm-up) | 630 |

### Latency — pooled percentiles

Method: `pooled-raw-events` — percentiles computed on the pooled raw per-request samples across repetitions (never averaged across runs). Seconds.

| signal | n | p50 | p90 | p95 | p99 | p999 | max | mean |
|---|---|---|---|---|---|---|---|---|
| `ttft_seconds` | 630 | 0.301967 | 0.302630 | 0.302790 | 0.317944 | — | 0.364772 | 0.302395 |
| `e2e_duration_seconds` | 630 | 0.504074 | 0.740789 | 0.758337 | 0.851899 | — | 0.917664 | 0.519010 |
| `itl_seconds` | 8679 | 0.015667 | 0.016049 | 0.016125 | 0.016256 | 0.036680 | 0.078120 | 0.015716 |
| `max_stall_seconds` | 594 | 0.016084 | 0.016271 | 0.016469 | 0.058327 | — | 0.078120 | 0.016912 |

(p999 is only resolved at n ≥ 1000 pooled samples; '—' means the pool cannot support it. The mean column is context for the percentiles, never a substitute.)

### Goodput @ SLO `ib-t010-e1-mock-baseline@1.0.0` — with shed and stall rates adjacent

Shed and stall rates are part of the goodput figure, not footnotes: goodput can be gamed by shedding early, and a stream can meet its TTFT target and still stall mid-generation. All three are computed in one pass over the same measured window.

| goodput block | value |
|---|---|
| goodput ratio (meeting / ALL offered, incl. shed+canceled+errored) | 0.9873 |
| requests/second meeting SLO | 5.7353 |
| **shed rate (adjacent by rule)** | 0.0000 |
| **stall rate (adjacent by rule)** | 0.0135 at stall threshold 0.05s |

### Saturation knee

`knee_estimate: null` — no rate sweep contributed to this run set, so no saturation point was measured and **no capacity or saturation claim may be made from this report** (interpretation rule 4; also listed under threats to validity).

### Cost

`cost: null` — **why:** no cost profile applies to this run set — cost is null (cost figures are only computed from a declared, provenanced cost-profile file, never from assumed rates)

## Validity block (mandatory)

- **Warm-up handling:** manifest warm-up policy 'discard-requests' (50 requests per repetition, ordered by scheduled_send_ts): 150 events excluded, 630 kept (exp-EXP-ib-t010-e1-gateway-overhead-direct-r1/rep1: 50 excluded; exp-EXP-ib-t010-e1-gateway-overhead-direct-r2/rep2: 50 excluded; exp-EXP-ib-t010-e1-gateway-overhead-direct-r3/rep3: 50 excluded)
- **Run count / pooling statement:** 3 repetition(s) pooled; all percentile tables above are computed on the pooled raw events of these repetitions (never on averaged per-run percentiles).
- **Declared error/shed gate:** the pinned result schema carries no gate fields; the gate disclosure, if tripped, appears under threats to validity.
- **Closed-loop flag:** no contributing workload declares closed-loop arrival.

### Threats to validity (mandatory)

- no rate sweep in this run set — knee_estimate is null; no claim is made about saturation behavior
- no cost profile applies to this run set — cost is null (cost figures are only computed from a declared, provenanced cost-profile file, never from assumed rates)

### Unexplained anomalies (mandatory — never silently empty)

**None observed.** We looked and found none; an anomaly-free claim is only honest next to the checks that were run:

- benchmark-result file schema-validated against the pinned contracts bundle
- linked run manifest(s) resolved, loaded, and schema-validated
- goodput block verified to carry shed_rate and stall_rate (contract-required siblings; a goodput without them is not renderable)
- validity block verified complete (warm_up_handling, run_count, threats_to_validity, unexplained_anomalies)
- validity.warm_up_handling cross-checked against the manifest's declared warm-up policy

## Reproduction — one command

This report regenerates from the linked artifacts with exactly:

```sh
python3 -m inferbench_analysis report --bundle /home/user/serving-contracts --result docs/evidence/ib-t010/results/ib-t010-e1-mock-direct.benchmark-result.json --root . --out docs/evidence/ib-t010
```

Pinned versions: serving-contracts bundle v0.2.0; inferbench-analysis 0.2.0.

The benchmark-result file of record is `docs/evidence/ib-t010/results/ib-t010-e1-mock-direct.benchmark-result.json`; it was emitted from the linked raw events by `python3 -m inferbench_analysis analyze` (IB-T005) and self-validates against the pinned schema.

## Provenance links

- run manifests: `docs/evidence/ib-t010/e1-mock-compare/direct/rep-1/manifest.json`, `docs/evidence/ib-t010/e1-mock-compare/direct/rep-2/manifest.json`, `docs/evidence/ib-t010/e1-mock-compare/direct/rep-3/manifest.json`
- raw events: `docs/evidence/ib-t010/e1-mock-compare/direct/rep-1/events.jsonl`, `docs/evidence/ib-t010/e1-mock-compare/direct/rep-2/events.jsonl`, `docs/evidence/ib-t010/e1-mock-compare/direct/rep-3/events.jsonl`
- benchmark-result file: `docs/evidence/ib-t010/results/ib-t010-e1-mock-direct.benchmark-result.json`
