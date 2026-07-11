# Benchmark report — ib-t010-e2b-baseline-1x-sane

| | |
|---|---|
| result_id | `ib-t010-e2b-baseline-1x-sane` |
| result created_at | 2026-07-11T15:52:09Z |
| report generated_at | 2026-07-11T15:56:51Z |
| contracts bundle pin | v0.2.0 |
| generator | inferbench-analysis 0.2.0 (IB-T006 honest-report machine) |
| repetitions pooled | 3 |

**Source of the numbers:** benchmark-result file `docs/evidence/ib-t010/results/ib-t010-e2b-baseline-1x-sane.benchmark-result.json` (schema-validated against the pinned bundle). Bootstrap CIs and cross-run dispersion are not carried by the pinned result schema; regenerate the report from the raw events for those surfaces

## Hypothesis under test

Every run manifest declares the hypothesis it was executed for; a report is only interpretable against it (experiments.md rule 6).

> **ib-t010-e2b-baseline-r1:** IB-T010 E2b (pre-declared G5-verifier follow-up to E2's REFUTED verdict): shrinking the admission queue cap from 3 (E2 admission-sane-v1) to 1 (E2b admission-sane-v1b), holding -admission-global-inflight-budget=6 and -admission-queue-deadline=500ms fixed, is expected to cut the queue-transit contribution to accepted-request TTFT roughly 3x, pulling the accepted-request TTFT p95 degradation at ~5x offered rate vs the SAME-config ~1x capacity-boundary baseline under the G5 <=20% criterion, at the cost of a higher shed rate than E2's cap=3 arm at the same offered rate. Sheds remain typed 503 overloaded + Retry-After.

> **ib-t010-e2b-baseline-r2:** IB-T010 E2b (pre-declared G5-verifier follow-up to E2's REFUTED verdict): shrinking the admission queue cap from 3 (E2 admission-sane-v1) to 1 (E2b admission-sane-v1b), holding -admission-global-inflight-budget=6 and -admission-queue-deadline=500ms fixed, is expected to cut the queue-transit contribution to accepted-request TTFT roughly 3x, pulling the accepted-request TTFT p95 degradation at ~5x offered rate vs the SAME-config ~1x capacity-boundary baseline under the G5 <=20% criterion, at the cost of a higher shed rate than E2's cap=3 arm at the same offered rate. Sheds remain typed 503 overloaded + Retry-After.

> **ib-t010-e2b-baseline-r3:** IB-T010 E2b (pre-declared G5-verifier follow-up to E2's REFUTED verdict): shrinking the admission queue cap from 3 (E2 admission-sane-v1) to 1 (E2b admission-sane-v1b), holding -admission-global-inflight-budget=6 and -admission-queue-deadline=500ms fixed, is expected to cut the queue-transit contribution to accepted-request TTFT roughly 3x, pulling the accepted-request TTFT p95 degradation at ~5x offered rate vs the SAME-config ~1x capacity-boundary baseline under the G5 <=20% criterion, at the cost of a higher shed rate than E2's cap=3 arm at the same offered rate. Sheds remain typed 503 overloaded + Retry-After.

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

### ib-t010-e2b-baseline-r1

- manifest: `docs/evidence/ib-t010/e2b-baseline/rep-1/manifest.json`
- workload file: `docs/evidence/ib-t010/e2b-baseline/rep-1/workload.json`
- arrival process: open-loop Poisson, rate 37.8072 req/s; workload_ref = ib-t010-e2-baseline@1.0.0 seed 10010202
- target topology: `gateway-mock`

```json
{
  "run_id": "ib-t010-e2b-baseline-r1",
  "target_topology": "gateway-mock",
  "workload_ref": {
    "name": "ib-t010-e2-baseline",
    "version": "1.0.0",
    "seed": 10010202
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
    "config_version": "admission-sane-v1b"
  },
  "client": {
    "location": "same-host (loopback)",
    "rtt_ms": 0.894052
  },
  "warm_up": {
    "policy": "discard-requests",
    "value": 50
  },
  "repetitions": 3,
  "hypothesis": "IB-T010 E2b (pre-declared G5-verifier follow-up to E2's REFUTED verdict): shrinking the admission queue cap from 3 (E2 admission-sane-v1) to 1 (E2b admission-sane-v1b), holding -admission-global-inflight-budget=6 and -admission-queue-deadline=500ms fixed, is expected to cut the queue-transit contribution to accepted-request TTFT roughly 3x, pulling the accepted-request TTFT p95 degradation at ~5x offered rate vs the SAME-config ~1x capacity-boundary baseline under the G5 <=20% criterion, at the cost of a higher shed rate than E2's cap=3 arm at the same offered rate. Sheds remain typed 503 overloaded + Retry-After.",
  "started_at": "2026-07-11T15:51:06Z",
  "contracts_bundle_version": "v0.2.0",
  "notes": "gateway.config_version=admission-sane-v1b encodes exactly: -admission-tenant-queue-cap 1 -admission-global-inflight-budget 6 -admission-global-queue-cap 1 -admission-queue-deadline 500ms (single changed variable vs E2's admission-sane-v1: queue cap 3->1, both tenant and global caps move together; budget and deadline unchanged; see scripts/ib-t010-e2b-queue-cap.sh and hypotheses/EXP-ib-t010-e2b-queue-cap.json)"
}
```

### ib-t010-e2b-baseline-r2

- manifest: `docs/evidence/ib-t010/e2b-baseline/rep-2/manifest.json`
- workload file: `docs/evidence/ib-t010/e2b-baseline/rep-2/workload.json`
- arrival process: open-loop Poisson, rate 37.8072 req/s; workload_ref = ib-t010-e2-baseline@1.0.0 seed 10010202
- target topology: `gateway-mock`

```json
{
  "run_id": "ib-t010-e2b-baseline-r2",
  "target_topology": "gateway-mock",
  "workload_ref": {
    "name": "ib-t010-e2-baseline",
    "version": "1.0.0",
    "seed": 10010202
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
    "config_version": "admission-sane-v1b"
  },
  "client": {
    "location": "same-host (loopback)",
    "rtt_ms": 0.831364
  },
  "warm_up": {
    "policy": "discard-requests",
    "value": 50
  },
  "repetitions": 3,
  "hypothesis": "IB-T010 E2b (pre-declared G5-verifier follow-up to E2's REFUTED verdict): shrinking the admission queue cap from 3 (E2 admission-sane-v1) to 1 (E2b admission-sane-v1b), holding -admission-global-inflight-budget=6 and -admission-queue-deadline=500ms fixed, is expected to cut the queue-transit contribution to accepted-request TTFT roughly 3x, pulling the accepted-request TTFT p95 degradation at ~5x offered rate vs the SAME-config ~1x capacity-boundary baseline under the G5 <=20% criterion, at the cost of a higher shed rate than E2's cap=3 arm at the same offered rate. Sheds remain typed 503 overloaded + Retry-After.",
  "started_at": "2026-07-11T15:51:16Z",
  "contracts_bundle_version": "v0.2.0",
  "notes": "gateway.config_version=admission-sane-v1b encodes exactly: -admission-tenant-queue-cap 1 -admission-global-inflight-budget 6 -admission-global-queue-cap 1 -admission-queue-deadline 500ms (single changed variable vs E2's admission-sane-v1: queue cap 3->1, both tenant and global caps move together; budget and deadline unchanged; see scripts/ib-t010-e2b-queue-cap.sh and hypotheses/EXP-ib-t010-e2b-queue-cap.json)"
}
```

### ib-t010-e2b-baseline-r3

- manifest: `docs/evidence/ib-t010/e2b-baseline/rep-3/manifest.json`
- workload file: `docs/evidence/ib-t010/e2b-baseline/rep-3/workload.json`
- arrival process: open-loop Poisson, rate 37.8072 req/s; workload_ref = ib-t010-e2-baseline@1.0.0 seed 10010202
- target topology: `gateway-mock`

```json
{
  "run_id": "ib-t010-e2b-baseline-r3",
  "target_topology": "gateway-mock",
  "workload_ref": {
    "name": "ib-t010-e2-baseline",
    "version": "1.0.0",
    "seed": 10010202
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
    "config_version": "admission-sane-v1b"
  },
  "client": {
    "location": "same-host (loopback)",
    "rtt_ms": 1.07423
  },
  "warm_up": {
    "policy": "discard-requests",
    "value": 50
  },
  "repetitions": 3,
  "hypothesis": "IB-T010 E2b (pre-declared G5-verifier follow-up to E2's REFUTED verdict): shrinking the admission queue cap from 3 (E2 admission-sane-v1) to 1 (E2b admission-sane-v1b), holding -admission-global-inflight-budget=6 and -admission-queue-deadline=500ms fixed, is expected to cut the queue-transit contribution to accepted-request TTFT roughly 3x, pulling the accepted-request TTFT p95 degradation at ~5x offered rate vs the SAME-config ~1x capacity-boundary baseline under the G5 <=20% criterion, at the cost of a higher shed rate than E2's cap=3 arm at the same offered rate. Sheds remain typed 503 overloaded + Retry-After.",
  "started_at": "2026-07-11T15:51:25Z",
  "contracts_bundle_version": "v0.2.0",
  "notes": "gateway.config_version=admission-sane-v1b encodes exactly: -admission-tenant-queue-cap 1 -admission-global-inflight-budget 6 -admission-global-queue-cap 1 -admission-queue-deadline 500ms (single changed variable vs E2's admission-sane-v1: queue cap 3->1, both tenant and global caps move together; budget and deadline unchanged; see scripts/ib-t010-e2b-queue-cap.sh and hypotheses/EXP-ib-t010-e2b-queue-cap.json)"
}
```

## Results

### Throughput (measured window)

| metric | value |
|---|---|
| ok-requests / second | 31.0122 |
| output tokens / second | 242.43 |
| total requests (all statuses) | 900 |
| total output tokens | 5863 |
| pooled events (post warm-up) | 900 |

### Latency — pooled percentiles

Method: `pooled-raw-events` — percentiles computed on the pooled raw per-request samples across repetitions (never averaged across runs). Seconds.

| signal | n | p50 | p90 | p95 | p99 | p999 | max | mean |
|---|---|---|---|---|---|---|---|---|
| `ttft_seconds` | 750 | 0.082959 | 0.104252 | 0.115553 | 0.140214 | — | 0.169913 | 0.088298 |
| `e2e_duration_seconds` | 750 | 0.157874 | 0.227074 | 0.245460 | 0.264353 | — | 0.278019 | 0.161210 |
| `itl_seconds` | 5113 | 0.010681 | 0.011029 | 0.011119 | 0.011270 | 0.011435 | 0.011503 | 0.010677 |
| `max_stall_seconds` | 698 | 0.011012 | 0.011235 | 0.011295 | 0.011393 | — | 0.011503 | 0.010985 |

(p999 is only resolved at n ≥ 1000 pooled samples; '—' means the pool cannot support it. The mean column is context for the percentiles, never a substitute.)

### Goodput @ SLO `ib-t010-e2-baseline@1.0.0` — with shed and stall rates adjacent

Shed and stall rates are part of the goodput figure, not footnotes: goodput can be gamed by shedding early, and a stream can meet its TTFT target and still stall mid-generation. All three are computed in one pass over the same measured window.

| goodput block | value |
|---|---|
| goodput ratio (meeting / ALL offered, incl. shed+canceled+errored) | 0.8333 |
| requests/second meeting SLO | 31.0122 |
| **shed rate (adjacent by rule)** | 0.1667 |
| **stall rate (adjacent by rule)** | 0.0000 at stall threshold 0.1s |

### Saturation knee

`knee_estimate: null` — no rate sweep contributed to this run set, so no saturation point was measured and **no capacity or saturation claim may be made from this report** (interpretation rule 4; also listed under threats to validity).

### Cost

`cost: null` — **why:** no cost profile applies to this run set — cost is null (cost figures are only computed from a declared, provenanced cost-profile file, never from assumed rates)

## Validity block (mandatory)

- **Warm-up handling:** manifest warm-up policy 'discard-requests' (50 requests per repetition, ordered by scheduled_send_ts): 150 events excluded, 900 kept (ib-t010-e2b-baseline-r1/rep1: 50 excluded; ib-t010-e2b-baseline-r2/rep2: 50 excluded; ib-t010-e2b-baseline-r3/rep3: 50 excluded)
- **Run count / pooling statement:** 3 repetition(s) pooled; all percentile tables above are computed on the pooled raw events of these repetitions (never on averaged per-run percentiles).
- **Declared error/shed gate:** the pinned result schema carries no gate fields; the gate disclosure, if tripped, appears under threats to validity.
- **Closed-loop flag:** no contributing workload declares closed-loop arrival.

### Threats to validity (mandatory)

- capacity-boundary point: offered rate equals E2's own probe-estimated capacity (37.8072 rps, reused verbatim for single-variable purity vs the queue-cap change), so a shed rate around the E2 baseline's ~10% (here elevated further by the shallower cap=1 queue) is expected boundary behavior, not a failure; the declared gate threshold is raised to 0.20 (from the 0.05 default) for exactly this point, disclosed here
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
python3 -m inferbench_analysis report --bundle /home/user/serving-contracts --result docs/evidence/ib-t010/results/ib-t010-e2b-baseline-1x-sane.benchmark-result.json --root . --out docs/evidence/ib-t010
```

Pinned versions: serving-contracts bundle v0.2.0; inferbench-analysis 0.2.0.

The benchmark-result file of record is `docs/evidence/ib-t010/results/ib-t010-e2b-baseline-1x-sane.benchmark-result.json`; it was emitted from the linked raw events by `python3 -m inferbench_analysis analyze` (IB-T005) and self-validates against the pinned schema.

## Provenance links

- run manifests: `docs/evidence/ib-t010/e2b-baseline/rep-1/manifest.json`, `docs/evidence/ib-t010/e2b-baseline/rep-2/manifest.json`, `docs/evidence/ib-t010/e2b-baseline/rep-3/manifest.json`
- raw events: `docs/evidence/ib-t010/e2b-baseline/rep-1/events.jsonl`, `docs/evidence/ib-t010/e2b-baseline/rep-2/events.jsonl`, `docs/evidence/ib-t010/e2b-baseline/rep-3/events.jsonl`
- benchmark-result file: `docs/evidence/ib-t010/results/ib-t010-e2b-baseline-1x-sane.benchmark-result.json`
