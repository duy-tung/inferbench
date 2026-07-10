# Observability — inferbench

The generator's own health is part of benchmark validity. A load generator that cannot prove it
kept its schedule produces numbers that look fine and mean nothing. Observability here is
therefore self-diagnostic first, and it produces *files* (run logs), not dashboards — dashboards
belong to `inferops`.

## Structured run logs

Every run directory contains a structured (JSONL) run log alongside the manifest and raw events:

- run lifecycle: start, warm-up boundary, completion or typed abort;
- configuration echo: workload name+version+seed, target, arrival process, pinned bundle version;
- watchdog and resource-sampler records (below);
- **per-run summary printed at completion:** sent / completed / canceled / shed / error counts —
  the one-glance sanity check that the run did what the workload declared.

## Schedule-slip watchdog

The executable guardian of the open-loop invariant. It continuously compares intended send times
(fixed by the seed before the run) against actual send timestamps. If slip exceeds the declared
threshold, the run **aborts with a typed reason** (`abort_reason: schedule_slip`) and is marked
INVALID in the run log. Slip data is recorded even below the threshold so reports can cite
observed maximum slip in threats-to-validity.

## Client host resource sampling

CPU utilization, open file descriptors, and network throughput of the generator host are sampled
during the run and written into the run log. Purpose: distinguish "the target got slower" from
"the client got slower" — the classic silent-invalidity failure. These samples are cited in every
report's threats-to-validity section (methodology rule 9; risk "client host becomes the
bottleneck" in `risks.md`).

## Client-side metric series naming (mirror rule)

Per Contract 2, gateway-side and client-side measurements are **separate named series, never
conflated**:

| Client series (contract §8 names) | Definition (client-side measurement point) | Gateway counterpart |
|---|---|---|
| `client_ttft_seconds` | `scheduled_send_ts` (schedule-plan send time; raw-event v0.2.0 CO-safe basis) → first response body byte at the client | `inference_ttft_seconds` (first upstream body byte at the gateway) |
| `client_itl_seconds` | gap between successive content-bearing SSE chunks observed at the client (stamped at arrival, before parsing) | `inference_itl_seconds` |
| `client_e2e_duration_seconds` | `scheduled_send_ts` → stream close at the client | `inference_e2e_duration_seconds` |
| `client_max_stall` | maximum inter-chunk gap within one stream (`raw-event.itl.max_stall_seconds`) | — (client-only) |

The difference between a client series and its gateway counterpart (network RTT, client
scheduling, kernel buffering) is expected, measured, and **explainable — never mysterious**.
Queue delay is gateway-reported and correlated to client events by `X-Request-Id`; I2's
acceptance includes client-vs-gateway TTFT agreement within a declared tolerance, and the
program's failure-handling rule is: measurement disagreement → check measurement-point
definitions before touching code.

All timing uses **monotonic clocks**; wall-clock timestamps appear only as event metadata for
correlation, never as duration sources.

## Engine metrics side channel (optional)

When the target's Contract 4 capability descriptor exposes a metrics endpoint, an optional poller
scrapes `/metrics` using the descriptor's **name mapping** (vLLM gauge names vary by version —
mapped, never hardcoded; as of 2026-07, re-verify at use time). Records land as an auxiliary
time-series file in the run directory. Hard rule: the poller is side-channel only — it never runs
on, blocks, or shares resources with the load path in a way that could perturb the schedule.

## Typed abort reasons

Runs never fail vaguely. Abort taxonomy (recorded in the run log and manifest status):

- `schedule_slip` — watchdog threshold exceeded; client could not keep the open-loop schedule.
- `client_saturation` — resource sampler crossed declared limits (CPU/FD/network).
- `target_unreachable_at_start` — preflight failed; no data emitted.
- `manifest_incomplete` — refusal before start (see `architecture.md`, manifest capturer).

An aborted run is invalid, is never analyzed into a report, and its abort reason plus supporting
samples are preserved as evidence.
