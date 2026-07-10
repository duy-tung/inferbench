# Architecture — inferbench

Two halves, one data flow: a Go load generator that emits raw JSONL events plus a run manifest, and
a Python analysis package that turns those files into schema-valid results and honest reports.
Everything between the halves is **files** — no database, no server, no shared library.

## Data flow

```text
workload file (versioned, seeded, schema-valid)
      │
      ▼
seeded arrival scheduler ──► SSE streaming client ──HTTP/SSE──► target
(send times fixed by seed,      │        │                (mock | llama.cpp | vLLM |
 never by responses)            │        │                 infergate in front | any
      │                         │        │                 OpenAI-compatible endpoint)
      │              cancellation      slow-client
      │              controller        emulator
      ▼                         │        │
raw-event recorder ◄────────────┴────────┘        manifest capturer ──► run manifest (JSON)
(JSONL, streaming writes)                          engine-metrics poller (side channel)
      │
      ▼
Python analysis: load/validate ─► pooled stats ─► knee/goodput/cost ─► report + result file
                                                                       (benchmark-result schema,
                                                                        consumed by fleetlab &
                                                                        inference-lab)
```

## Repository layout

```text
cmd/inferbench/          # single CLI: run, sweep, replay, compare, experiment
internal/schedule/       # seeded arrival processes (open-loop poisson, closed-loop flagged)
internal/client/         # SSE client, monotonic timing, cancellation, slow-client
internal/events/         # raw-event JSONL recorder
internal/sweep/          # rate-sweep orchestration
internal/replay/         # deterministic replay
internal/experiment/     # hypothesis-file governance
internal/manifest/       # manifest capture + refusal logic
workloads/               # the 8 versioned seeded workload files
analysis/                # Python package: stats, knee, goodput, cost, compare, report
docs/                    # this documentation set
```

## Go generator components

- **Workload loader/validator** — reads versioned workload files and validates them against the
  pinned `workload.schema.json`. Invalid or unversioned workloads refuse to run.
- **Seeded arrival scheduler** — the open-loop core. Send times are computed from the seed and the
  declared arrival process (open-loop Poisson at rate λ, or closed-loop with a mandatory disclosure
  flag) **before the run starts and independently of any response**. This is the
  coordinated-omission defense: a slow or saturated target can never delay subsequent sends, so
  queueing delay shows up in the measurements instead of being silently absorbed by the generator.
  See `adr/ADR-0001-open-loop-scheduler.md`.
- **SSE streaming client** — drives Contract 1 (`POST /v1/chat/completions` with `stream=true`).
  Uses **monotonic clocks** for all timing; records per-chunk timestamps, client TTFT (send to
  first body byte at the client), the full ITL series or its summary, and max stall per stream.
  Parses the standardized SSE error event, records request IDs, and classifies outcomes per the
  Contract 1 error taxonomy.
- **Cancellation controller** — issues deliberate cancellations at the three declared points —
  *queued*, *pre-first-token*, *mid-stream* — per the workload's cancellation profile, by closing
  the connection, and records the cancellation point in the raw event.
- **Slow-client emulator** — reads the response body at a bounded rate per the workload's
  slow-client profile, to exercise target-side backpressure and write-buffer behavior.
- **Raw-event recorder** — a single writer fed by a bounded channel; streaming JSONL writes with
  bounded memory; one record per request per `raw-event.schema.json`.
- **Sweep orchestrator** — rate sweeps of ≥6 points spanning 10% → 120% of estimated capacity,
  with repetition control (≥3 runs per point).
- **Replay runner** — re-issues a recorded workload deterministically (same seed → identical send
  schedule and request contents).
- **Manifest capturer** — collects target/engine/hardware/config facts (per
  `benchmark-run.schema.json`) *before* the run and **refuses to run** without a complete manifest.
- **Engine-metrics poller (optional)** — scrapes the target's `/metrics` using the Contract 4
  capability mapping (metric names are mapped, never hardcoded — vLLM gauge names vary by
  version). Side-channel only: it records an auxiliary series and never runs on, or blocks, the
  load path.

## Python analysis components

- **Loader/validator** — loads raw events + manifests, validates both against the pinned schemas,
  and **refuses undeclared or manifest-less data**.
- **Statistics core** — pooled percentiles (computed on pooled raw per-request data across
  repetitions — never averaged across runs, enforced in code), bootstrap confidence intervals,
  warm-up exclusion (first ≥50 requests or 60–120 s). See `adr/ADR-0002-statistics-choices.md`.
- **Knee detector** — estimates the saturation knee from sweep data; no extrapolation past it.
- **Goodput@SLO calculator** — requires an explicit pre-declared SLO reference; computes shed rate
  and stall rate **in the same pass**, so a goodput figure can never exist without them.
- **Cost calculator** — cost per successful request and per 1M tokens from cost-profile files
  (schema owned by contracts).
- **Comparison engine** — A/B across runs that share *all* controlled variables except exactly one
  declared experimental variable; refuses anything else.
- **Plotting + report generator** — templates embed the full manifest, interpretation rules,
  mandatory "threats to validity" and "unexplained anomalies" sections, the one-command
  reproduction line, and visible closed-loop flags; emits schema-valid `benchmark-result` files.

## Concurrency model (Go)

- **One scheduler goroutine owns the send timeline.** It fires request starts at the precomputed
  times; nothing downstream can push back on it.
- **Per-request goroutines own the stream lifecycle** — connect, read chunks, timestamp, cancel at
  the declared point if the workload says so, and emit exactly one raw event.
- **The recorder is a single writer** fed by a bounded channel; backpressure on the channel is a
  client-health signal, not a scheduling input.
- **The open-loop invariant:** the send schedule is never perturbed by slow responses, slow disk,
  or a saturated target. If the *client host itself* cannot keep the schedule (CPU, file
  descriptors, network limits), the run is **INVALID**: a schedule-slip watchdog plus client
  resource sampling detect this and abort with a typed reason recorded in the run log. Emitting
  misleading data is never an option.
- `go test -race ./...` clean is mandatory for all of this.

## Failure, cancellation, and retry behavior

- Target errors, timeouts, sheds, and mid-stream SSE error events are recorded as **classified raw
  events** — they never crash the run and are never silently dropped. Shed/error visibility is
  what makes goodput honest.
- **The generator NEVER retries.** A retry would corrupt the open-loop arrival process and hide
  errors. Retry behavior is the gateway's concern and is *observed*, not performed.
- Deliberate cancellation is a workload feature, executed by closing the connection at the
  declared point and recorded in the event.
- Runs have typed abort conditions — schedule slip, client saturation, target unreachable at start
  — that mark the run invalid rather than emitting misleading data.

## Data and state

Files only. Raw events are append-only JSONL per run; manifests are JSON; results are schema-valid
JSON; reports are Markdown/HTML with embedded graphs. Every run directory is self-describing:
manifest + workload reference + raw events + logs. Result files are shareable by construction
(see `security.md`).

## Boundary discipline

The target — gateway or engine — is a **black-box network endpoint**. `inferbench` never imports
target source, never asserts engine internals (batching, KV cache, prefix cache, placement), and
builds/tests against recorded fixtures and the released mock-backend image (owned by `infergate`).
All schemas are owned by `serving-contracts`; any schema-affecting change here is blocked until
contracts release it first.
