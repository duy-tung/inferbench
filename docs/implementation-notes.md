# Implementation notes — inferbench

Running log of decisions, surprises, measured facts (with provenance and dates), and evidence
links. Cross-repo evidence goes to `inference-lab` at integration time; this file holds the
repo-local trail.

## Log

### 2026-07-10 — IB-T001: docs bootstrap

- Created the full `docs/` set (15 files + `adr/` with 4 ADRs) per the repo plan. No code yet
  (IB-T002+).
- **Recorded assumption (reversible):** the `serving-contracts` bundle v0.1.x is not yet released
  — the sibling `serving-contracts` repo has no commits or tags as of today. Docs reference the
  design-level contract definitions; the concrete bundle pin will be recorded here when IB-T002
  starts. If the released schemas differ from the design-level summaries embedded in
  `interfaces.md`, the docs follow the released bundle (contracts own the schemas).
- **Recorded assumption (reversible):** the deterministic mock-backend image (owned by
  `infergate`) is not yet released; CI wiring against it is deferred to IB-T002 and its tag will
  be pinned here.
- Volatile facts carried with "as of 2026-07 — re-verify at use time" flags: `vllm bench serve`
  name/behavior, vLLM metric names, LiteLLM self-reported 8 ms p95 overhead baseline, GPU budget
  envelope (~$150–250, user-confirmable), OTel GenAI semconv status.

### 2026-07-10 — IB-T002: open-loop generator core + raw events

**Scope delivered:** Go module `github.com/duy-tung/inferbench` (stdlib only, Go 1.24):
`cmd/inferbench` (`run` subcommand), `internal/workload` (schema-mirroring loader/validator,
distribution sampling, deterministic synthetic prompts), `internal/schedule` (seeded open-loop
Poisson arrival plan, single-rate + phased, precomputed before any network I/O — ADR-0001),
`internal/client` (Contract 1 non-streaming + SSE streaming, outcome classification onto the
error taxonomy, no retries), `internal/events` (raw-event record + single-writer bounded JSONL
recorder), `internal/manifest` (facts-file load, completion, refusal validation),
`internal/run` (scheduler goroutine + per-request goroutines + schedule-slip watchdog).

**Contracts pin (reversible assumption):** `serving-contracts` @ commit `8c58863`
("release: prepare v0.1.0"); no tag exists yet. Emitted manifests carry
`contracts_bundle_version: "8c58863 (v0.1.0 tag pending)"`. Re-pin to the `v0.1.0` tag when cut.

**Mock/gateway pin:** live verification target built read-only from `infergate` @ commit
`a5a2c02` via `git archive` (the released mock image does not exist yet — same assumption as
IB-T001, image tag recorded here when released).

**PRNG determinism note:** arrival gaps, input lengths, output lengths, and prompt text each
draw from an independent `math/rand/v2` PCG stream derived from `(workload seed, purpose
constant)`, so changing one distribution never perturbs the others. PCG is a stable documented
algorithm; same seed → identical plan across builds (provenance: measured, 2026-07-10, unit +
live evidence below).

**Decisions (recorded, reversible):**
- **Client-observed transport failures** have no server-supplied taxonomy value; mapping chosen:
  connection-level failure → `upstream_error`, client-imposed request deadline →
  `upstream_timeout`, context cancel → `canceled`. From the measurement's perspective the whole
  target is the upstream. Revisit at IB-T004 if the contracts add a client-side vocabulary.
- **`retries` is constitutionally 0**: the generator never retries (ADR-0001); `Request.GetBody`
  is cleared so Go's transport cannot even replay a POST on a dead reused connection.
- **Token counts** come from the response `usage` payload (streaming requests always set
  `stream_options.include_usage: true`). If a 2xx response carries no usage, output falls back
  to the content-chunk count (streaming) or 0 (non-streaming) and the run summary prints a
  warning; declared token-source selection via Contract 4 capability descriptors is IB-T004.
- **TTFT for non-streaming responses** is send-complete → first response *body* byte, so for
  the non-streaming path it includes full-body serialization by the target (the mock serializes
  TTFT + (n−1)·ITL before writing). This is honest but a different quantity from streaming TTFT;
  never compare the two series. Shed/error responses carry `ttft_seconds: null` per schema.
- **`--seed` / `--rate` CLI overrides** mutate the effective workload before planning; the
  effective seed is recorded in `workload_ref.seed`. A rate override without a workload version
  bump breaks comparability keys — acceptable for smoke/sweep mechanics, and results intended
  for publication must use versioned workload files as-is (enforced review point, IB-T006).
- **Manifest facts file:** `--manifest` supplies everything the tool cannot know; the tool fills
  `run_id`, `workload_ref`, `started_at`, `contracts_bundle_version`, and measures `client.rtt_ms`
  at preflight (healthz round trip) when the facts file leaves it null. Validation refuses
  incomplete manifests before any request is sent.

**Deferred (typed `ErrNotImplemented` refusals, never silent):** prefix-sharing ratio > 0
(IB-T003), cancellation rate > 0 and slow-client fraction > 0 (IB-T004), closed-loop arrival
execution (IB-T008; the mandatory disclosure flag is already validated at parse time per
ADR-0003). Multi-repetition orchestration is IB-T008 (`--repetition` records the index; one
execution = one repetition).

**Verification (all commands run 2026-07-10, outputs archived under `docs/evidence/ib-t002/`):**

1. `gofmt -l .` clean; `go vet ./...` clean; `go test -race -count=1 ./...` → **6 packages ok**
   (client 3.2s, events 1.1s, manifest 1.0s, run 4.3s, schedule 1.0s, workload 1.0s; cmd has no
   tests yet). Includes:
   - **CO-safety test** (`TestSendScheduleIndependentOfResponseLatency`): target delays every
     response 2 s; 20 sends 50 ms apart all dispatched within 150 ms of the precomputed times,
     wall ≈ schedule span + one response delay (a response-coupled generator would need ≥ 40 s),
     and recorded `send_ts` values match the plan offsets.
   - **Seed determinism** (`TestSeedDeterminism`): same seed → identical plan and byte-identical
     request bodies across two executions; different seed → different plan.
   - **Watchdog** (`TestScheduleSlipWatchdog`): forced slip aborts with typed
     `schedule_slip`, ABORT recorded in the run log — run INVALID, no misleading data.
   - **Never-retry** (`TestErrorsAreRecordedNotRetried`): a 100% 503 shed storm yields exactly
     one send per request and schema-shaped shed events (`ttft`/`itl` null, `shed: true`).
   - Streaming client verified against contract-shaped SSE fixtures (chunk framing, `[DONE]`,
     usage-in-final-chunk, mid-stream error event, truncated-stream classification).
2. **Live run vs the pinned gateway+mock pair** (`mock-backend -seed 42 -ttft 20ms -itl 5ms`,
   `gateway` in front, both @ `a5a2c02`): `inferbench run --workload
   testdata/live/mock-smoke.workload.json --manifest testdata/live/gateway-mock.manifest.json
   --target http://127.0.0.1:8180 --out <dir>` → **sent=200 ok=200 errors=0 shed=0**, max
   dispatch slip 26.6 ms / 7.6 ms (runs A/B), wall 9.6 s at 20 rps.
3. **Kit validation green** for all emitted artifacts:
   `contracts-validate.py validate --schema raw-event events.jsonl` and
   `--schema benchmark-run manifest.json` → PASS for every run; the test workload passes
   `--schema workload`.
4. **Deterministic replay:** same seed twice → the 200-line precomputed schedule dumps are
   byte-identical and all 200 mock response-body SHA-256 hashes match run-to-run; `--seed 99`
   produces a different schedule. Streaming and non-streaming runs of the same seed share the
   identical send schedule.
5. **Live streaming smoke (informational, unpinned):** infergate's *uncommitted working tree*
   already implements the IG-T003 SSE relay; a streaming run against a build of that tree
   (labeled `a5a2c02+uncommitted-worktree` in its manifest) gave 200/200 ok, client TTFT
   p50 21.3 ms (configured 20 ms), ITL max-stall p50 5.9 ms (configured 5 ms), schema-valid
   events, and identical body hashes across two same-seed runs. Not pinned evidence — the
   pinned streaming verification reruns when IG-T003 lands; formal calibration is IB-T004.

**Evidence:** `docs/evidence/ib-t002/smoke-A/` (pinned non-streaming run: manifest, 200 raw
events, run log with schedule dump + body hashes) and `docs/evidence/ib-t002/stream-SA/`
(informational streaming run, same layout).

## Deviations

> Program deviation policy: when repository evidence forces a deviation from the approved plan,
> choose the conservative reversible option, record the evidence, decision, consequences, and
> follow-up here, and continue. Pause for user input only when the deviation changes public
> contracts, repository ownership, security posture, or milestone scope.

### 2026-07-10 — IB-T002: live streaming verification against the pinned pair is not possible yet

- **Evidence:** infergate's committed HEAD (`a5a2c02`, IG-T002 scope) rejects `stream: true`
  with a typed `invalid_request`/`unsupported_value` error in both the gateway and the mock
  (SSE relay is IG-T003); the plan's "released mock-backend image" does not exist yet.
- **Decision (conservative, reversible):** streaming client behavior is verified by unit tests
  against contract-shaped SSE fixtures (mirroring `serving-contracts/examples/api/*.sse`), the
  pinned live run is non-streaming, and an *informational* streaming smoke against infergate's
  uncommitted working tree is recorded but clearly labeled unpinned.
- **Consequences:** M2's "dry-run vs the released mock image" arm remains open until infergate
  releases the image; no published claim depends on streaming yet.
- **Follow-up:** rerun the live streaming verification against the pinned/released mock when
  IG-T003 lands, before IB-T004 calibration starts.
