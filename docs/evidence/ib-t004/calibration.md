# IB-T004 — Mock calibration report (streaming client correctness)

**Date:** 2026-07-10 · **Status:** PASSED (all 28 checks within declared tolerance)
**Target:** gateway + mock-backend built **read-only** from `infergate` @
`5d69aeb11228b5dcfbaca10c2e57f0a24603b8f9` (current HEAD at run time, held stable via the
harness's pin argument; includes the IG-T003 SSE relay and IG-T006 observability) via
`git archive` — commit recorded in `infergate-pin.txt` and injected into every manifest by the
harness. Loopback, CPU-only dev container.
**Contracts pin:** `serving-contracts` @ `8d81492` (v0.2.0 tag pending); every emitted artifact
in this directory kit-validates (`kit-validate.log`, 53/53 PASS including the regenerated
`ib-t003` suite dry-runs).
**Reproduction (one command, servers included):**

```
scripts/calibrate-mock.sh <out-dir> [path-to-infergate] [pin]
```

This is calibration evidence for measurement correctness — NOT a benchmark. Single repetition,
no warm-up handling (mock timing is stateless), evidence-grade nearest-rank percentiles
(`scripts/eventstats.py`; the IB-T005 statistics engine supersedes it for published results).

## 1. Measurement points under test

Client-side mirror series per the metrics contract (`metrics/metrics.md` §4/§8), latency basis
per `raw-event.schema.json` v0.2.0:

- **client TTFT** = `scheduled_send_ts` → first response body byte at the client (the CO-safe
  basis: dispatch/marshal/connect/write delay included). Named `client_ttft_seconds`, never
  conflated with the gateway's `inference_ttft_seconds`.
- **client ITL** = gaps between content-bearing SSE chunks, stamped at client arrival before
  parsing; role/usage/`[DONE]` chunks define no gaps; max stall = largest gap.
- All stamps are `time.Now()` pairs → Go monotonic-clock arithmetic (wall-clock steps cannot
  corrupt a measurement).

## 2. Calibration: measured vs configured (the core)

Workload per point: open-loop Poisson 4 rps, 120 streaming requests, inputs uniform 64–256
tokens, `max_tokens` 128 (mock generates 1–128 tokens per its seeded PRNG), seed 1004001.

**Declared tolerance** (loopback + Go timer overshoot + scheduled-send-basis overhead, which
adds send-slip ≈ 1 ms locally):

- TTFT p50 − configured ∈ **[−2 ms, +15 ms]**; TTFT p95 ≤ configured + 50 ms
- ITL p50 − configured ∈ **[−2 ms, +5 ms]**; pooled ITL p95 ≤ configured + 15 ms

| Point | Series | Configured | Measured p50 | Δ p50 | Measured p95 | Δ p95 | n | Verdict |
|---|---|---|---|---|---|---|---|---|
| A | client TTFT | 100 ms | 102.84 ms | +2.84 ms | 104.33 ms | +4.33 ms | 120 | PASS |
| A | client ITL (pooled) | 20 ms | 20.81 ms | +0.81 ms | 21.23 ms | +1.23 ms | 7530 gaps | PASS |
| B | client TTFT | 300 ms | 302.97 ms | +2.97 ms | 304.94 ms | +4.94 ms | 120 | PASS |
| B | client ITL (pooled) | 5 ms | 5.47 ms | +0.47 ms | 5.91 ms | +0.91 ms | 7530 gaps | PASS |

Supporting observations (both points): 120/120 streams ok; max dispatch/send slip ≤ 2.2 ms
(≪ 100 ms watchdog threshold); send-slip p50 ≈ 1.0 ms — the measured overhead of the
scheduled-send basis on a healthy client. Max stall tracked the configured ITL (point A p50
21.5 ms; point B p50 6.1 ms). Two configuration points demonstrate the measurement follows the
configured latency, not a coincidence of one config.

Raw artifacts: `calib-A/`, `calib-B/` (manifest, events, run log, derived workload,
`stats.json`, mock `debug-state.json`).

## 3. Cancellation issuance at the three points

Mock at TTFT 300 ms / ITL 30 ms; 30 requests per scenario, cancellation rate 1.0; verification
is two-sided — client raw events AND the mock's abort observability (`GET /debug/state`,
snapshot per scenario in `debug-state.json`; counters are per-process, one fresh mock per
scenario).

| Point | Profile | Client events | Mock `/debug/state` | Verdict |
|---|---|---|---|---|
| queued (pre-send) | elapsed-seconds, constant 0 s | 30/30 `canceled`, 0 tokens at cancel, no TTFT, cancel issued ≤ 0.08 ms after the send basis; **20/30 sends never completed → `send_slip_seconds` ABSENT** | `requests_total: 0` — canceled before the request left the client | PASS |
| pre-first-token | elapsed-seconds, constant 0.15 s (< TTFT) | 30/30 `canceled`, 0 tokens, no TTFT, elapsed p50 149.4 ms | 30 aborts, all `phase=pre_first_token`, `chunks_sent=0` | PASS |
| mid-stream | output-tokens, constant 8 | 29/30 `canceled` with **exactly 8 tokens at cancel**, TTFT kept (p50 303.2 ms), ITL series kept (p50 30.6 ms); 1 stream completed `ok` (mock generated ≤ 8 tokens — honest realized-cancel accounting, planned vs realized in the run log) | 29 aborts, all `phase=mid_stream`, `chunks_sent=9` (role + 8 content) | PASS |

Cancellation propagates as Contract 1 requires (client context cancel → connection close →
gateway → mock observes the abort). `cancellation_point.elapsed_seconds` is recorded per the
raw-event schema: seconds after `send_ts` at which the client closed the connection
(clamped ≥ 0 for pre-send cancels, where `send_ts` is the request-start fallback).

## 4. Slow-client emulation: bounded read rate

Mock at TTFT 20 ms / ITL 5 ms; 30 streaming requests at 3 rps, `max_tokens` 64
(≈ 5–11 KB of SSE payload per stream); control = full-speed readers, slow = every reader
throttled to **4096 B/s** with a **200 ms** first-read delay.

| Run | e2e p50 | e2e p95 | TTFT p50 | pooled ITL p50 / p95 | max stall p50 |
|---|---|---|---|---|---|
| slow-control (full speed) | 0.230 s | 0.353 s | 22.6 ms | 5.4 ms / 5.7 ms | 5.8 ms |
| slow-on (4096 B/s + 0.2 s delay) | **2.113 s** (9.2× control) | 3.012 s | **223.2 ms** (includes the self-imposed delay) | 0.06 ms / 100.4 ms | 100.6 ms |

The bounded read rate dominates the client-observed stream: e2e ≈ bytes ÷ 4096 B/s (+ delay),
TTFT honestly includes the initial read delay (client-side series measures the client's own
experience), and the ITL distribution becomes bimodal (chunks within one paced read ≈ 0 ms;
pacing gaps ≈ 100 ms = read-chunk size `bps/10` ÷ rate) — the signature of a client-bound
stream. All 30/30 streams completed; the co-scheduled control run confirms the throttle, not
the target, is the bottleneck.

Caveat (recorded): the emulation throttles at the client's HTTP-body reader. Kernel socket
buffers and the transport's ~4 KB read buffer sit below it, so on loopback the server can run
several KB ahead before feeling backpressure; gateway-side write-buffer/deadline behavior under
slow clients is infergate's fault-scenario-4 territory and is exercised, not asserted, here.

## 5. Threats to validity / anomalies

- Loopback-only: network RTT ≈ 0, so the measured overhead (+2.9 ms TTFT, +0.8 ms ITL) is
  timer/scheduler/stack cost, not a network model. Cross-host calibration is IB-T007 territory.
- Evidence-grade percentiles (nearest-rank, no CIs); tolerances are wide relative to observed
  deltas, so the verdicts do not hinge on estimator choice.
- Scheduler-pause outliers on the shared dev container: calib-A max ITL gap 59.4 ms (1 of
  7530), calib-B max gap 227.3 ms and max TTFT 439.6 ms (each 1 of their samples; p95 columns
  unaffected). Consistent with CPU contention from concurrent builds on this host; recorded,
  within tolerance, no further anomalies found.
- The mock caps completions at 256 tokens regardless of `max_tokens` (mockengine property);
  calibration workloads were sized under the cap so this does not affect the tables above.
  The same property explains the suite dry-run's cancel-storm realizing 7 of 39 planned
  cancels (sampled points 0.2–3.0 s mostly land after these short streams end) — planned vs
  realized is logged per run.
