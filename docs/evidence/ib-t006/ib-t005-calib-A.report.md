# Benchmark report — ib-t005-calib-A

| | |
|---|---|
| result_id | `ib-t005-calib-A` |
| result created_at | 2026-07-10T19:44:37Z |
| report generated_at | 2026-07-11T00:07:45Z |
| contracts bundle pin | 8d81492 (v0.2.0 tag pending) |
| generator | inferbench-analysis 0.2.0 (IB-T006 honest-report machine) |
| repetitions pooled | 1 |

**Source of the numbers:** benchmark-result file `docs/evidence/ib-t005/results/ib-t005-calib-A.benchmark-result.json` (schema-validated against the pinned bundle). Bootstrap CIs and cross-run dispersion are not carried by the pinned result schema; regenerate the report from the raw events for those surfaces

## Hypothesis under test

Every run manifest declares the hypothesis it was executed for; a report is only interpretable against it (experiments.md rule 6).

> **ib-t004-calib-A:** Measured client TTFT p50 lands within [configured-2ms, configured+15ms] and pooled client ITL p50 within [configured-2ms, configured+5ms] of the mock-configured 100ms/20ms, with p95 within +50ms/+15ms (tolerance statement in calibration.md).

## Interpretation rules — what may and may not be concluded

These rules are embedded by the report generator and cannot be omitted; a reading of this report that violates them misquotes it.

1. **Comparability (verbatim, serving-contracts compatibility/compatibility-policy.md §7, pin 8d81492 (v0.2.0 tag pending)):** results are comparable only when model revision, quantization, tokenizer, engine version+flags, hardware, driver/CUDA, workload version+seed, and warm-up policy all match, **or** the difference is the single declared experimental variable. No cross-hardware or cross-tool comparison may be drawn from this report.
2. **Pooled percentiles:** every percentile below is computed on the pooled raw per-request events across all 1 repetition(s) of this run set (method `pooled-raw-events`). Percentiles are NEVER averaged across runs; cross-run dispersion, where shown, is median ± range of per-run summaries and is not a percentile table.
3. **Arrival process:** latency and goodput claims are valid only under open-loop arrivals; closed-loop contributions are flagged here and support throughput-ceiling statements only (closed-loop hides queueing delay — coordinated omission).
4. **Saturation:** no extrapolation past the saturation knee; when the knee estimate below is null, NO saturation or capacity claim may be made from this report.
5. **Goodput:** only meaningful next to its SLO reference, shed rate, and stall rate — they are printed adjacent below; quoting the goodput ratio without them misrepresents this report (a system can inflate goodput by shedding early or stalling mid-stream).
6. **Measurement points:** all latency series are CLIENT-side series measured from the scheduled send time (coordinated-omission-safe basis; contracts metrics mirror rule). Client TTFT is a different series from gateway TTFT — never conflate them.
7. **No mean-only reading:** means appear only beside full percentile columns; the mean of a latency distribution is not a summary of it.
8. **Provenance:** numbers in this report are measured (from the linked raw events) unless explicitly labeled otherwise; every external number carries basis + date where cited.

## Run manifest(s) — full, embedded

The complete manifest of every pooled run (pins, flags, topology, hardware, warm-up policy, hypothesis). A result without its manifest is not publishable.

### ib-t004-calib-A

- manifest: `docs/evidence/ib-t004/calib-A/manifest.json`
- workload file: `docs/evidence/ib-t004/calib-A/workload.json`
- arrival process: open-loop Poisson, rate 4 req/s; workload_ref = calib-a@1.0.0 seed 1004001
- target topology: `gateway-mock`

```json
{
  "run_id": "ib-t004-calib-A",
  "target_topology": "gateway-mock",
  "workload_ref": {
    "name": "calib-a",
    "version": "1.0.0",
    "seed": 1004001
  },
  "engine": {
    "name": "mock",
    "version": "dev",
    "commit": "5d69aeb11228b5dcfbaca10c2e57f0a24603b8f9",
    "flags": {
      "error_rate": 0,
      "itl": "20ms",
      "seed": 42,
      "ttft": "100ms"
    }
  },
  "model": {
    "checkpoint": "mock-8b",
    "revision": "mockengine@5d69aeb",
    "tokenizer": "mockengine-estimator (~4 bytes/token, deterministic)"
  },
  "hardware": {
    "gpu_model": null,
    "gpu_count": 0,
    "vram_gb": null,
    "driver_version": null,
    "cuda_version": null,
    "instance_type": "local-dev-container (linux/amd64, CPU-only)"
  },
  "gateway": {
    "version": "dev@5d69aeb",
    "config_version": "flags-v1 (static flag config; snapshots land at IG-T004): -upstream-timeout 180s -stream-write-timeout 30s"
  },
  "client": {
    "location": "same-host (loopback)",
    "rtt_ms": 1.351346
  },
  "warm_up": {
    "policy": "none"
  },
  "repetitions": 1,
  "hypothesis": "Measured client TTFT p50 lands within [configured-2ms, configured+15ms] and pooled client ITL p50 within [configured-2ms, configured+5ms] of the mock-configured 100ms/20ms, with p95 within +50ms/+15ms (tolerance statement in calibration.md).",
  "started_at": "2026-07-10T14:43:52Z",
  "contracts_bundle_version": "8d81492 (v0.2.0 tag pending)",
  "notes": "IB-T004 calibration/verification template. scripts/calibrate-mock.sh derives per-scenario facts (engine ttft/itl flags, hypothesis, notes) from this file. Smoke/calibration evidence, not a benchmark: no warm-up handling, single repetition. rtt_ms measured at preflight when left null."
}
```

## Results

### Throughput (measured window)

| metric | value |
|---|---|
| ok-requests / second | 3.1175 |
| output tokens / second | 198.74 |
| total requests (all statuses) | 120 |
| total output tokens | 7650 |
| pooled events (post warm-up) | 120 |

### Latency — pooled percentiles

Method: `pooled-raw-events` — percentiles computed on the pooled raw per-request samples across repetitions (never averaged across runs). Seconds.

| signal | n | p50 | p90 | p95 | p99 | p999 | max | mean |
|---|---|---|---|---|---|---|---|---|
| `ttft_seconds` | 120 | 0.102833 | 0.103709 | 0.104329 | 0.105027 | — | 0.118783 | 0.103000 |
| `e2e_duration_seconds` | 120 | 1.414857 | 2.487111 | 2.557468 | 2.717662 | — | 2.750780 | 1.411110 |
| `itl_seconds` | 7530 | 0.020806 | 0.021153 | 0.021231 | 0.022534 | 0.048802 | 0.059435 | 0.020843 |
| `max_stall_seconds` | 117 | 0.021469 | 0.040597 | 0.048781 | 0.058310 | — | 0.059435 | 0.025512 |

(p999 is only resolved at n ≥ 1000 pooled samples; '—' means the pool cannot support it. The mean column is context for the percentiles, never a substitute.)

### Goodput @ SLO `mock-loopback-baseline@1.0.0` — with shed and stall rates adjacent

Shed and stall rates are part of the goodput figure, not footnotes: goodput can be gamed by shedding early, and a stream can meet its TTFT target and still stall mid-generation. All three are computed in one pass over the same measured window.

| goodput block | value |
|---|---|
| goodput ratio (meeting / ALL offered, incl. shed+canceled+errored) | 1.0000 |
| requests/second meeting SLO | 3.1175 |
| **shed rate (adjacent by rule)** | 0.0000 |
| **stall rate (adjacent by rule)** | 0.0000 at stall threshold 0.25s |

### Saturation knee

`knee_estimate: null` — no rate sweep contributed to this run set, so no saturation point was measured and **no capacity or saturation claim may be made from this report** (interpretation rule 4; also listed under threats to validity).

### Cost

`cost: null` — **why:** no cost profile applies to this run set — cost is null (cost figures are only computed from a declared, provenanced cost-profile file, never from assumed rates)

## Validity block (mandatory)

- **Warm-up handling:** manifest warm-up policy 'none': no warm-up exclusion applied; 0 of 120 events excluded
- **Run count / pooling statement:** 1 repetition(s) pooled; all percentile tables above are computed on the pooled raw events of these repetitions (never on averaged per-run percentiles).
- **Declared error/shed gate:** the pinned result schema carries no gate fields; the gate disclosure, if tripped, appears under threats to validity.
- **Closed-loop flag:** no contributing workload declares closed-loop arrival.

### Threats to validity (mandatory)

- run_count=1 is below the >=3-repetitions methodology minimum (experiments.md rule 4); cross-run dispersion is not assessable
- no rate sweep in this run set — knee_estimate is null; no claim is made about saturation behavior
- no cost profile applies to this run set — cost is null (cost figures are only computed from a declared, provenanced cost-profile file, never from assumed rates)
- warm-up policy 'none' declared in the manifest: no warm-up exclusion applied; results include any cold-start effects (experiments.md rule 2 requires exclusion for published benchmark claims)

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
python3 -m inferbench_analysis report --bundle /home/user/serving-contracts --result docs/evidence/ib-t005/results/ib-t005-calib-A.benchmark-result.json --out docs/evidence/ib-t006
```

Pinned versions: serving-contracts bundle 8d81492 (v0.2.0 tag pending); inferbench-analysis 0.2.0.

The benchmark-result file of record is `docs/evidence/ib-t005/results/ib-t005-calib-A.benchmark-result.json`; it was emitted from the linked raw events by `python3 -m inferbench_analysis analyze` (IB-T005) and self-validates against the pinned schema.

## Provenance links

- run manifests: `docs/evidence/ib-t004/calib-A/manifest.json`
- raw events: `docs/evidence/ib-t004/calib-A/events.jsonl`
- benchmark-result file: `docs/evidence/ib-t005/results/ib-t005-calib-A.benchmark-result.json`
