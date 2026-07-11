# Benchmark report — ib-t006-cancel-queued

| | |
|---|---|
| result_id | `ib-t006-cancel-queued` |
| result created_at | 2026-07-11T00:07:45Z |
| report generated_at | 2026-07-11T00:07:45Z |
| contracts bundle pin | 8d81492 (v0.2.0 tag pending) |
| generator | inferbench-analysis 0.2.0 (IB-T006 honest-report machine) |
| repetitions pooled | 1 |

**Source of the numbers:** in-memory analysis of the linked raw events (no benchmark-result file exists for this run set: signal 'ttft_seconds' has zero pooled samples in the measured window (e.g. no request ever produced a first byte / no request completed ok) — the contract-required latency table cannot be computed; latency is withheld, the run remains valid)

## Hypothesis under test

Every run manifest declares the hypothesis it was executed for; a report is only interpretable against it (experiments.md rule 6).

> **ib-t004-cancel-queued:** A cancel profiled at 0s after the scheduled send is issued at dispatch: the client records status=canceled with 0 tokens and no TTFT; requests that never completed their send emit send_slip_seconds ABSENT.

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

### ib-t004-cancel-queued

- manifest: `docs/evidence/ib-t004/cancel-queued/manifest.json`
- workload file: `docs/evidence/ib-t004/cancel-queued/workload.json`
- arrival process: open-loop Poisson, rate 4 req/s; workload_ref = cancel-queued@1.0.0 seed 1004001
- target topology: `gateway-mock`

```json
{
  "run_id": "ib-t004-cancel-queued",
  "target_topology": "gateway-mock",
  "workload_ref": {
    "name": "cancel-queued",
    "version": "1.0.0",
    "seed": 1004001
  },
  "engine": {
    "name": "mock",
    "version": "dev",
    "commit": "5d69aeb11228b5dcfbaca10c2e57f0a24603b8f9",
    "flags": {
      "error_rate": 0,
      "itl": "30ms",
      "seed": 42,
      "ttft": "300ms"
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
    "rtt_ms": 0.782026
  },
  "warm_up": {
    "policy": "none"
  },
  "repetitions": 1,
  "hypothesis": "A cancel profiled at 0s after the scheduled send is issued at dispatch: the client records status=canceled with 0 tokens and no TTFT; requests that never completed their send emit send_slip_seconds ABSENT.",
  "started_at": "2026-07-10T14:45:09Z",
  "contracts_bundle_version": "8d81492 (v0.2.0 tag pending)",
  "notes": "IB-T004 calibration/verification template. scripts/calibrate-mock.sh derives per-scenario facts (engine ttft/itl flags, hypothesis, notes) from this file. Smoke/calibration evidence, not a benchmark: no warm-up handling, single repetition. rtt_ms measured at preflight when left null."
}
```

## Results

### Throughput (measured window)

| metric | value |
|---|---|
| ok-requests / second | 0.0000 |
| output tokens / second | 0.00 |
| total requests (all statuses) | 30 |
| total output tokens | 0 |
| measured window | 9.709 s |
| pooled events (post warm-up) | 30 |

### Latency percentiles — WITHHELD (no-samples)

No latency table is shown because none exists in this analysis; rendering one would fabricate meaning. **Why:**

> signal 'ttft_seconds' has zero pooled samples in the measured window (e.g. no request ever produced a first byte / no request completed ok) — the contract-required latency table cannot be computed; latency is withheld, the run remains valid

| gate accounting | value |
|---|---|
| error rate (measured window) | 0.0000 |
| shed rate (measured window) | 0.0000 |
| declared gate threshold (error+shed) | 0.05 |
| withholding kind | `no-samples` |

The run set itself remains VALID: its throughput, error/shed accounting, goodput-vs-SLO, and validity data above/below are real measurements. Only the latency percentile tables are meaningless and therefore absent. No benchmark-result file exists for this run set — the pinned contract requires numeric percentile tables with no null form, and emitting fabricated or gated numbers is forbidden; THIS REPORT is the publishable artifact (contracts observation recorded in docs/implementation-notes.md).

### Goodput @ SLO `mock-loopback-baseline@1.0.0` — with shed and stall rates adjacent

Shed and stall rates are part of the goodput figure, not footnotes: goodput can be gamed by shedding early, and a stream can meet its TTFT target and still stall mid-generation. All three are computed in one pass over the same measured window.

| goodput block | value |
|---|---|
| goodput ratio (meeting / ALL offered, incl. shed+canceled+errored) | 0.0000 (0/30 offered) |
| requests/second meeting SLO | 0.0000 |
| **shed rate (adjacent by rule)** | 0.0000 |
| **stall rate (adjacent by rule)** | 0.0000 (0/0 streaming requests) at stall threshold 0.25s |

### Saturation knee

`knee_estimate: null` — no rate sweep contributed to this run set, so no saturation point was measured and **no capacity or saturation claim may be made from this report** (interpretation rule 4; also listed under threats to validity).

### Cost

`cost: null` — **why:** no cost profile applies to this run set — cost is null (cost figures are only computed from a declared, provenanced cost-profile file, never from assumed rates)

## Validity block (mandatory)

- **Warm-up handling:** manifest warm-up policy 'none': no warm-up exclusion applied; 0 of 30 events excluded
- **Run count / pooling statement:** 1 repetition(s) pooled; all percentile tables above are computed on the pooled raw events of these repetitions (never on averaged per-run percentiles).
- **Declared error/shed gate:** latency percentiles are withheld above error+shed rate 0.05; this run set measured error rate 0.0000 + shed rate 0.0000.
- **Closed-loop flag:** no contributing workload declares closed-loop arrival.

### Threats to validity (mandatory)

- latency percentiles withheld: signal 'ttft_seconds' has zero pooled samples in the measured window (e.g. no request ever produced a first byte / no request completed ok) — the contract-required latency table cannot be computed; latency is withheld, the run remains valid
- run_count=1 is below the >=3-repetitions methodology minimum (experiments.md rule 4); cross-run dispersion is not assessable
- no rate sweep in this run set — knee_estimate is null; no claim is made about saturation behavior
- no cost profile applies to this run set — cost is null (cost figures are only computed from a declared, provenanced cost-profile file, never from assumed rates)
- warm-up policy 'none' declared in the manifest: no warm-up exclusion applied; results include any cold-start effects (experiments.md rule 2 requires exclusion for published benchmark claims)
- no ITL-bearing requests in the measured window: itl_seconds/max_stall_seconds tables absent; stall_rate is 0.0 computed over 0 streaming requests (vacuous, not evidence of stall-freedom)
- 30 deliberately canceled request(s) (workload cancellation profile) count in the offered denominator and never meet the SLO; goodput ratio reflects that by design

### Unexplained anomalies (mandatory — never silently empty)

**None observed.** We looked and found none; an anomaly-free claim is only honest next to the checks that were run:

- manifest(s) and every raw event schema-validated against the pinned contracts bundle (the loader refuses manifest-less or schema-invalid data outright)
- run_id/repetition consistency between events and manifest enforced by the loader
- comparability keys (target_topology, workload_ref, engine, model, hardware, gateway, warm_up) verified identical across all pooled runs; duplicate run_ids refused (double-count/cherry-pick guard)
- warm-up exclusion counted per repetition in scheduled-send order and reconciled into the validity block
- declared error/shed gate evaluated over the measured window: error rate 0.0000 + shed rate 0.0000 vs declared threshold 0.05 — below threshold
- zero-sample check on the contract-required latency signals (ttft_seconds, e2e_duration_seconds)
- goodput ratio, shed rate, and stall rate computed in one pass over the same measured window (9.709s post-warm-up window, 30 events, 1 repetition(s))
- per-run p50 dispersion (median ± range) computed beside the pooled tables where run_count > 1

## Reproduction — one command

This report regenerates from the linked artifacts with exactly:

```sh
python3 -m inferbench_analysis report --bundle /home/user/serving-contracts --run docs/evidence/ib-t004/cancel-queued --slo docs/evidence/ib-t005/mock-loopback.slo.json --result-id ib-t006-cancel-queued --out docs/evidence/ib-t006
```

Pinned versions: serving-contracts bundle 8d81492 (v0.2.0 tag pending); inferbench-analysis 0.2.0.

## Provenance links

- run manifests: `docs/evidence/ib-t004/cancel-queued/manifest.json`
- raw events: `docs/evidence/ib-t004/cancel-queued/events.jsonl`
