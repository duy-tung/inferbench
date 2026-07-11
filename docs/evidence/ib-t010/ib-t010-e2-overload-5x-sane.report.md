# Benchmark report — ib-t010-e2-overload-5x-sane

| | |
|---|---|
| result_id | `ib-t010-e2-overload-5x-sane` |
| result created_at | 2026-07-11T15:24:34Z |
| report generated_at | 2026-07-11T15:26:20Z |
| contracts bundle pin | v0.2.0 |
| generator | inferbench-analysis 0.2.0 (IB-T006 honest-report machine) |
| repetitions pooled | 3 |

**Source of the numbers:** benchmark-result file `docs/evidence/ib-t010/results/ib-t010-e2-overload-5x-sane.benchmark-result.json` (schema-validated against the pinned bundle). Bootstrap CIs and cross-run dispersion are not carried by the pinned result schema; regenerate the report from the raw events for those surfaces

## Hypothesis under test

Every run manifest declares the hypothesis it was executed for; a report is only interpretable against it (experiments.md rule 6).

> **exp-EXP-ib-t010-e2-admission-value-sane-r1:** At an offered rate of approximately 5x the estimated admission-bounded capacity of the gateway+mock pair, configuring infergate's admission control sanely (bounded per-tenant queue, bounded global in-flight budget, bounded global queue, a short queue deadline) keeps accepted-request TTFT p95 degradation to <=20% versus the capacity-boundary (~1x) baseline measured under the SAME sane admission configuration, while excess requests are shed with typed HTTP responses (503 overloaded + Retry-After, docs/tasks.md IG-T010) rather than being queued indefinitely or crashing the process. Configuring admission effectively off (huge budget/queue caps) at the same 5x offered rate is the control: it is expected to shed close to nothing (no backpressure signal at all) -- the comparison isolates admission control's own queueing/shedding mechanics, since the deterministic mock backend imposes no concurrency-dependent latency of its own, so admission control is the ONLY source of capacity-boundary behavior in this experiment; that scope limitation is recorded honestly rather than implied away.

> **exp-EXP-ib-t010-e2-admission-value-sane-r2:** At an offered rate of approximately 5x the estimated admission-bounded capacity of the gateway+mock pair, configuring infergate's admission control sanely (bounded per-tenant queue, bounded global in-flight budget, bounded global queue, a short queue deadline) keeps accepted-request TTFT p95 degradation to <=20% versus the capacity-boundary (~1x) baseline measured under the SAME sane admission configuration, while excess requests are shed with typed HTTP responses (503 overloaded + Retry-After, docs/tasks.md IG-T010) rather than being queued indefinitely or crashing the process. Configuring admission effectively off (huge budget/queue caps) at the same 5x offered rate is the control: it is expected to shed close to nothing (no backpressure signal at all) -- the comparison isolates admission control's own queueing/shedding mechanics, since the deterministic mock backend imposes no concurrency-dependent latency of its own, so admission control is the ONLY source of capacity-boundary behavior in this experiment; that scope limitation is recorded honestly rather than implied away.

> **exp-EXP-ib-t010-e2-admission-value-sane-r3:** At an offered rate of approximately 5x the estimated admission-bounded capacity of the gateway+mock pair, configuring infergate's admission control sanely (bounded per-tenant queue, bounded global in-flight budget, bounded global queue, a short queue deadline) keeps accepted-request TTFT p95 degradation to <=20% versus the capacity-boundary (~1x) baseline measured under the SAME sane admission configuration, while excess requests are shed with typed HTTP responses (503 overloaded + Retry-After, docs/tasks.md IG-T010) rather than being queued indefinitely or crashing the process. Configuring admission effectively off (huge budget/queue caps) at the same 5x offered rate is the control: it is expected to shed close to nothing (no backpressure signal at all) -- the comparison isolates admission control's own queueing/shedding mechanics, since the deterministic mock backend imposes no concurrency-dependent latency of its own, so admission control is the ONLY source of capacity-boundary behavior in this experiment; that scope limitation is recorded honestly rather than implied away.

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

### exp-EXP-ib-t010-e2-admission-value-sane-r1

- manifest: `docs/evidence/ib-t010/e2-overload-compare/sane/rep-1/manifest.json`
- workload file: `docs/evidence/ib-t010/e2-overload-compare/sane/rep-1/workload.json`
- arrival process: open-loop Poisson, rate 189.0362 req/s; workload_ref = ib-t010-e2-overload@1.0.0 seed 10010201
- target topology: `gateway-mock`

```json
{
  "run_id": "exp-EXP-ib-t010-e2-admission-value-sane-r1",
  "target_topology": "gateway-mock",
  "workload_ref": {
    "name": "ib-t010-e2-overload",
    "version": "1.0.0",
    "seed": 10010201
  },
  "engine": {
    "name": "mock",
    "version": "dev",
    "commit": "6827d8c3d177464c17fae3b4dc6c2c475323333b",
    "flags": {
      "error_rate": 0,
      "itl_ms": 10,
      "seed": 43,
      "ttft_ms": 80
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
  "gateway": {
    "version": "dev@6827d8c",
    "config_version": "admission-sane-v1"
  },
  "client": {
    "location": "same-host (loopback)",
    "rtt_ms": 0.865664
  },
  "warm_up": {
    "policy": "discard-requests",
    "value": 50
  },
  "repetitions": 3,
  "hypothesis": "At an offered rate of approximately 5x the estimated admission-bounded capacity of the gateway+mock pair, configuring infergate's admission control sanely (bounded per-tenant queue, bounded global in-flight budget, bounded global queue, a short queue deadline) keeps accepted-request TTFT p95 degradation to <=20% versus the capacity-boundary (~1x) baseline measured under the SAME sane admission configuration, while excess requests are shed with typed HTTP responses (503 overloaded + Retry-After, docs/tasks.md IG-T010) rather than being queued indefinitely or crashing the process. Configuring admission effectively off (huge budget/queue caps) at the same 5x offered rate is the control: it is expected to shed close to nothing (no backpressure signal at all) -- the comparison isolates admission control's own queueing/shedding mechanics, since the deterministic mock backend imposes no concurrency-dependent latency of its own, so admission control is the ONLY source of capacity-boundary behavior in this experiment; that scope limitation is recorded honestly rather than implied away.",
  "started_at": "2026-07-11T15:22:45Z",
  "contracts_bundle_version": "v0.2.0",
  "notes": "gateway.config_version=admission-sane-v1 encodes exactly: -admission-tenant-queue-cap 3 -admission-global-inflight-budget 6 -admission-global-queue-cap 3 -admission-queue-deadline 500ms (latency-protective shallow-queue admission; see scripts/ib-t010-e2-admission-value.sh)"
}
```

### exp-EXP-ib-t010-e2-admission-value-sane-r2

- manifest: `docs/evidence/ib-t010/e2-overload-compare/sane/rep-2/manifest.json`
- workload file: `docs/evidence/ib-t010/e2-overload-compare/sane/rep-2/workload.json`
- arrival process: open-loop Poisson, rate 189.0362 req/s; workload_ref = ib-t010-e2-overload@1.0.0 seed 10010201
- target topology: `gateway-mock`

```json
{
  "run_id": "exp-EXP-ib-t010-e2-admission-value-sane-r2",
  "target_topology": "gateway-mock",
  "workload_ref": {
    "name": "ib-t010-e2-overload",
    "version": "1.0.0",
    "seed": 10010201
  },
  "engine": {
    "name": "mock",
    "version": "dev",
    "commit": "6827d8c3d177464c17fae3b4dc6c2c475323333b",
    "flags": {
      "error_rate": 0,
      "itl_ms": 10,
      "seed": 43,
      "ttft_ms": 80
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
  "gateway": {
    "version": "dev@6827d8c",
    "config_version": "admission-sane-v1"
  },
  "client": {
    "location": "same-host (loopback)",
    "rtt_ms": 0.616888
  },
  "warm_up": {
    "policy": "discard-requests",
    "value": 50
  },
  "repetitions": 3,
  "hypothesis": "At an offered rate of approximately 5x the estimated admission-bounded capacity of the gateway+mock pair, configuring infergate's admission control sanely (bounded per-tenant queue, bounded global in-flight budget, bounded global queue, a short queue deadline) keeps accepted-request TTFT p95 degradation to <=20% versus the capacity-boundary (~1x) baseline measured under the SAME sane admission configuration, while excess requests are shed with typed HTTP responses (503 overloaded + Retry-After, docs/tasks.md IG-T010) rather than being queued indefinitely or crashing the process. Configuring admission effectively off (huge budget/queue caps) at the same 5x offered rate is the control: it is expected to shed close to nothing (no backpressure signal at all) -- the comparison isolates admission control's own queueing/shedding mechanics, since the deterministic mock backend imposes no concurrency-dependent latency of its own, so admission control is the ONLY source of capacity-boundary behavior in this experiment; that scope limitation is recorded honestly rather than implied away.",
  "started_at": "2026-07-11T15:22:50Z",
  "contracts_bundle_version": "v0.2.0",
  "notes": "gateway.config_version=admission-sane-v1 encodes exactly: -admission-tenant-queue-cap 3 -admission-global-inflight-budget 6 -admission-global-queue-cap 3 -admission-queue-deadline 500ms (latency-protective shallow-queue admission; see scripts/ib-t010-e2-admission-value.sh)"
}
```

### exp-EXP-ib-t010-e2-admission-value-sane-r3

- manifest: `docs/evidence/ib-t010/e2-overload-compare/sane/rep-3/manifest.json`
- workload file: `docs/evidence/ib-t010/e2-overload-compare/sane/rep-3/workload.json`
- arrival process: open-loop Poisson, rate 189.0362 req/s; workload_ref = ib-t010-e2-overload@1.0.0 seed 10010201
- target topology: `gateway-mock`

```json
{
  "run_id": "exp-EXP-ib-t010-e2-admission-value-sane-r3",
  "target_topology": "gateway-mock",
  "workload_ref": {
    "name": "ib-t010-e2-overload",
    "version": "1.0.0",
    "seed": 10010201
  },
  "engine": {
    "name": "mock",
    "version": "dev",
    "commit": "6827d8c3d177464c17fae3b4dc6c2c475323333b",
    "flags": {
      "error_rate": 0,
      "itl_ms": 10,
      "seed": 43,
      "ttft_ms": 80
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
  "gateway": {
    "version": "dev@6827d8c",
    "config_version": "admission-sane-v1"
  },
  "client": {
    "location": "same-host (loopback)",
    "rtt_ms": 0.622
  },
  "warm_up": {
    "policy": "discard-requests",
    "value": 50
  },
  "repetitions": 3,
  "hypothesis": "At an offered rate of approximately 5x the estimated admission-bounded capacity of the gateway+mock pair, configuring infergate's admission control sanely (bounded per-tenant queue, bounded global in-flight budget, bounded global queue, a short queue deadline) keeps accepted-request TTFT p95 degradation to <=20% versus the capacity-boundary (~1x) baseline measured under the SAME sane admission configuration, while excess requests are shed with typed HTTP responses (503 overloaded + Retry-After, docs/tasks.md IG-T010) rather than being queued indefinitely or crashing the process. Configuring admission effectively off (huge budget/queue caps) at the same 5x offered rate is the control: it is expected to shed close to nothing (no backpressure signal at all) -- the comparison isolates admission control's own queueing/shedding mechanics, since the deterministic mock backend imposes no concurrency-dependent latency of its own, so admission control is the ONLY source of capacity-boundary behavior in this experiment; that scope limitation is recorded honestly rather than implied away.",
  "started_at": "2026-07-11T15:22:56Z",
  "contracts_bundle_version": "v0.2.0",
  "notes": "gateway.config_version=admission-sane-v1 encodes exactly: -admission-tenant-queue-cap 3 -admission-global-inflight-budget 6 -admission-global-queue-cap 3 -admission-queue-deadline 500ms (latency-protective shallow-queue admission; see scripts/ib-t010-e2-admission-value.sh)"
}
```

## Results

### Throughput (measured window)

| metric | value |
|---|---|
| ok-requests / second | 37.9254 |
| output tokens / second | 281.96 |
| total requests (all statuses) | 2550 |
| total output tokens | 4260 |
| pooled events (post warm-up) | 2550 |

### Latency — pooled percentiles

Method: `pooled-raw-events` — percentiles computed on the pooled raw per-request samples across repetitions (never averaged across runs). Seconds.

| signal | n | p50 | p90 | p95 | p99 | p999 | max | mean |
|---|---|---|---|---|---|---|---|---|
| `ttft_seconds` | 573 | 0.151038 | 0.189679 | 0.201764 | 0.219338 | — | 0.233133 | 0.150574 |
| `e2e_duration_seconds` | 573 | 0.218934 | 0.293831 | 0.309173 | 0.355634 | — | 0.388761 | 0.219295 |
| `itl_seconds` | 3687 | 0.010674 | 0.011021 | 0.011119 | 0.011288 | 0.011505 | 0.014115 | 0.010662 |
| `max_stall_seconds` | 526 | 0.011008 | 0.011255 | 0.011326 | 0.011470 | — | 0.014115 | 0.010994 |

(p999 is only resolved at n ≥ 1000 pooled samples; '—' means the pool cannot support it. The mean column is context for the percentiles, never a substitute.)

### Goodput @ SLO `ib-t010-e2-baseline@1.0.0` — with shed and stall rates adjacent

Shed and stall rates are part of the goodput figure, not footnotes: goodput can be gamed by shedding early, and a stream can meet its TTFT target and still stall mid-generation. All three are computed in one pass over the same measured window.

| goodput block | value |
|---|---|
| goodput ratio (meeting / ALL offered, incl. shed+canceled+errored) | 0.2247 |
| requests/second meeting SLO | 37.9254 |
| **shed rate (adjacent by rule)** | 0.7753 |
| **stall rate (adjacent by rule)** | 0.0000 at stall threshold 0.1s |

### Saturation knee

`knee_estimate: null` — no rate sweep contributed to this run set, so no saturation point was measured and **no capacity or saturation claim may be made from this report** (interpretation rule 4; also listed under threats to validity).

### Cost

`cost: null` — **why:** no cost profile applies to this run set — cost is null (cost figures are only computed from a declared, provenanced cost-profile file, never from assumed rates)

## Validity block (mandatory)

- **Warm-up handling:** manifest warm-up policy 'discard-requests' (50 requests per repetition, ordered by scheduled_send_ts): 150 events excluded, 2550 kept (exp-EXP-ib-t010-e2-admission-value-sane-r1/rep1: 50 excluded; exp-EXP-ib-t010-e2-admission-value-sane-r2/rep2: 50 excluded; exp-EXP-ib-t010-e2-admission-value-sane-r3/rep3: 50 excluded)
- **Run count / pooling statement:** 3 repetition(s) pooled; all percentile tables above are computed on the pooled raw events of these repetitions (never on averaged per-run percentiles).
- **Declared error/shed gate:** the pinned result schema carries no gate fields; the gate disclosure, if tripped, appears under threats to validity.
- **Closed-loop flag:** no contributing workload declares closed-loop arrival.

### Threats to validity (mandatory)

- deliberate 5x-capacity overload point: the ~77% typed shed rate IS the treatment under test (admission control shedding excess demand with 503+Retry-After), not a failure mode being hidden; the declared gate threshold is raised to 0.95 for exactly this point, disclosed here, and the latency table below covers ACCEPTED requests only (shed requests never produce a first byte)
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
python3 -m inferbench_analysis report --bundle /home/user/serving-contracts --result docs/evidence/ib-t010/results/ib-t010-e2-overload-5x-sane.benchmark-result.json --root . --out docs/evidence/ib-t010
```

Pinned versions: serving-contracts bundle v0.2.0; inferbench-analysis 0.2.0.

The benchmark-result file of record is `docs/evidence/ib-t010/results/ib-t010-e2-overload-5x-sane.benchmark-result.json`; it was emitted from the linked raw events by `python3 -m inferbench_analysis analyze` (IB-T005) and self-validates against the pinned schema.

## Provenance links

- run manifests: `docs/evidence/ib-t010/e2-overload-compare/sane/rep-1/manifest.json`, `docs/evidence/ib-t010/e2-overload-compare/sane/rep-2/manifest.json`, `docs/evidence/ib-t010/e2-overload-compare/sane/rep-3/manifest.json`
- raw events: `docs/evidence/ib-t010/e2-overload-compare/sane/rep-1/events.jsonl`, `docs/evidence/ib-t010/e2-overload-compare/sane/rep-2/events.jsonl`, `docs/evidence/ib-t010/e2-overload-compare/sane/rep-3/events.jsonl`
- benchmark-result file: `docs/evidence/ib-t010/results/ib-t010-e2-overload-5x-sane.benchmark-result.json`
