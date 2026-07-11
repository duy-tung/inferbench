# Interfaces — inferbench

All integration is via versioned contracts, released artifacts, files, or documented network
protocols. No shared application library with any repo. The dependency graph is acyclic.

## Consumed contracts (pinned bundle)

`inferbench` pins one released `serving-contracts` bundle version (SemVer tag) and validates both
golden fixtures and its own emitted artifacts against it in CI (`make contracts-verify` pattern;
this repo's arm of integration milestone I1).

> **Pin status:** pinned to `serving-contracts` **`v0.2.0` tag (commit `484b449`)**. Re-pinned at
> the IB-T002 CO-review fix (2026-07-10) from the released v0.1.0 tag because raw-event v0.2.0
> REQUIRES `scheduled_send_ts` (+ optional `send_slip_seconds`) with the normative rule that
> client-side TTFT/E2E are measured from the scheduled send time (coordinated-omission safety);
> re-pinned again at IB-T008 (2026-07-11) from the pre-tag commit `8d81492` to the now-cut
> `v0.2.0` tag (`git diff 8d81492..484b449 --stat` touches only `RELEASES.md`, no schema
> semantics). Emitted manifests carry `contracts_bundle_version: "v0.2.0"`. Pin history:
> `8c58863` (IB-T002) → `v0.1.0` (IB-T003) → `8d81492` (CO-review fix) → `v0.2.0`/`484b449`
> (IB-T008).

### Contract 1 — Inference API (OpenAI-compatible subset) — inferbench DRIVES it

- **Endpoints:** `POST /v1/chat/completions` (stream + non-stream), `GET /v1/models`,
  `GET /healthz`, `GET /readyz`, `GET /metrics`.
- **Supported request fields** (conformant servers reject all others, never ignore them): `model`,
  `messages`, `max_tokens`/`max_completion_tokens`, `temperature`, `top_p`, `stream`,
  `stream_options.include_usage`, `stop`, `seed`, `user`.
- **Streaming:** SSE `data: <json-chunk>` events, terminal `data: [DONE]`, every event flushed,
  usage in the final chunk when `stream_options.include_usage=true`, monotonically increasing
  chunk indices per stream, no cross-request interleaving.
- **Errors:** envelope `{"error": {"message", "type", "code", "param"}}` + request ID; taxonomy
  with retryability: `invalid_request`, `authentication`, `permission`, `not_found`,
  `rate_limited` (429 + `Retry-After`), `overloaded` (503 + `Retry-After`), `upstream_error`,
  `upstream_timeout`, `canceled`, `internal`. Mid-stream failures arrive as a standardized SSE
  error event then stream close.
- **Request ID:** `X-Request-Id` accepted or generated and echoed everywhere; inferbench records
  it in every raw event (it is the correlation key with gateway traces and queue-delay data).
- **Cancellation:** client disconnect / connection close MUST propagate upstream; tokens emitted
  before cancellation are billable. inferbench exercises all of this: it issues cancellations at
  declared points, parses error events, and classifies outcomes per the taxonomy.

### Contract 2 — Metrics vocabulary (client-side mirror)

Normative measurement points: gateway TTFT = first upstream body byte **at the gateway**; ITL =
inter-chunk gap; queue wait = admission-enqueue to dispatch (gateway-side). **Client-side TTFT
measured by inferbench is a separate, named series — never conflated with gateway TTFT.**
inferbench maintains mirror definitions with explicit names (`client_ttft`, `client_itl`,
`client_e2e`, `client_max_stall`) so gateway, benchmark, and simulation numbers are comparable and
their differences (network RTT, client scheduling) are explainable, not mysterious. See
`observability.md` for the naming rules.

### Contract 3 — Benchmark data schemas — inferbench EMITS, contracts OWN

| Schema | Role here |
|---|---|
| `workload.schema.json` | validates the 8 workloads authored in `workloads/` (name, version, seed; arrival process `open-loop-poisson` rate \| `closed-loop` + mandatory disclosure flag; input/output-length distributions; prefix-sharing ratio; cancellation-rate profile; slow-client profile; duration or request count) |
| `benchmark-run.schema.json` | run manifest: run ID; target topology (`engine-direct` \| `via-gateway` \| `gateway-mock`); engine name/version/commit + ALL runtime flags; model checkpoint + revision + tokenizer; hardware (GPU model, VRAM, driver, CUDA, instance type); gateway version + config version; client location/RTT; warm-up policy; repetition count; hypothesis statement. The generator refuses to run without a complete manifest |
| `raw-event.schema.json` | one JSONL record per request: request ID, workload item, send timestamp, TTFT, ITL series or summary + max stall, end timestamp, status, error class, input/output token counts, shed/retry flags, cancellation point |
| `benchmark-result.schema.json` | pooled-percentile tables, throughput, goodput with explicit SLO reference, shed rate (always adjacent), stall rate, saturation-knee estimate, cost per successful request and per 1M tokens (with cost-profile reference), validity block, links to raw events and manifest |

The analysis half also consumes the contracts-owned **cost-profile** and **SLO** schemas.

### Contract 4 — Backend capability — feature-gates workloads

Capability descriptors (engine name/version, streaming support, usage-in-stream support,
cancellation mechanism, metrics endpoint + name mapping, tokenizer identity, context limit, max
concurrency hints, prefix-cache support, quantization, priority support) drive:

- **Token-count source:** usage-in-stream support determines whether output token counts come from
  the stream's usage chunk or tokenizer-side estimation (the choice is declared in the manifest).
- **Workload gating:** prefix-cache support gates the `shared-prefix` experiments.
- **Metrics collection:** the metrics name mapping drives optional engine cache-info polling
  (vLLM gauge names vary by version — mapped, never hardcoded; as of 2026-07, re-verify at use
  time).

## Consumed runtime targets (network only)

| Target | Mechanism |
|---|---|
| `infergate`, llama.cpp, vLLM, any OpenAI-compatible endpoint | HTTP/SSE at run time only — never source |
| deterministic mock backend (owned by `infergate`) | released container image; the CI target |

## Provided artifacts

| Consumer | Mechanism | Content |
|---|---|---|
| `fleetlab` | benchmark-result + raw-event **files** (Contract 3) | goodput/memory/latency profiles input; I6 feedback loop |
| `inference-lab` | reports + result files | I2–I4, I6, I7 evidence; portfolio benchmark reports |
| `fleetlab` | the versioned workload files in `workloads/` | canonical arrival/length models (contracts ships only non-normative fixtures) |

## CLI surface (single binary: `inferbench`)

| Command | Purpose |
|---|---|
| `inferbench run` | one run: workload file + target + manifest inputs → run directory (manifest, raw events JSONL, run log) |
| `inferbench sweep` | rate sweep: ≥6 points 10%→120% of estimated capacity, ≥3 repetitions per point |
| `inferbench replay` | deterministic re-issue of a recorded workload (same seed → identical schedule) |
| `inferbench compare` | A/B across run sets; refuses comparisons violating the single-variable rule |
| `inferbench experiment` | governed experiment execution: requires a hypothesis file; rejects hypothesis-less runs and matrix sweeps |

Exact flags for `run`/`sweep`/`replay`/`compare`/`experiment {run,sweep,compare}` are fixed at
IB-T008/IB-T009 and documented in each subcommand's `-h` output (`cmd/inferbench/*.go`). `sweep`,
`compare`, and `experiment` share one execution path with `run` (`cmd/inferbench/common.go`'s
`runOnce`) — there is only one way this binary sends a request. `sweep --max-conns` and
`replay`/`compare`'s negative-control refusals (before any traffic) are demonstrated in
`docs/evidence/ib-t008/`; `experiment`'s hypothesis-file schema is `hypotheses/README.md`
(JSON, not the `docs/experiments.md` §5 YAML sketch — a recorded, reversible serialization
choice, `docs/implementation-notes.md`) and its refusal demos are
`docs/evidence/ib-t009/refusal-demo-transcript.md`.

Analysis is invoked as a Python package/CLI (`python3 -m inferbench_analysis ...`), consuming run
directories and emitting result files + reports. Exact flags are fixed at IB-T005 (`analyze`) and
IB-T006 (`report --result FILE | --run DIR ...` → Markdown report) and documented in the CLI
help; every report names the exact command that regenerates it (methodology rule 8). A valid run
whose latency is withheld (error/shed gate, or a zero-sample contract-required signal) has no
expressible result file at the pinned contracts version — `report --run` is its publishable
surface.

## Forbidden edges (checked at every review gate)

- `inferbench` → engine or gateway **source**. Network targets only; fixtures + released mock
  image for build/test.
- `inferbench` owns **no schema**; schema changes go through `serving-contracts` first.
- No shared statistics library with `fleetlab`; data exchange is files only.
- No capacity modeling, no dashboards, no Kubernetes manifests here.
