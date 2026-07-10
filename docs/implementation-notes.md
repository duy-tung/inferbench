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

### 2026-07-10 — IB-T003: workload suite v1 (+ Wave-1 exit-review items)

**Scope delivered:** the canonical versioned workload suite in `workloads/` — `chat-short`,
`rag-long-in`, `gen-long-out`, `shared-prefix`, `mixed`, `bursty`, `cancel-storm`,
`slow-client` — all at suite version **1.0.0** with fixed distinct seeds `1003001`–`1003008`
(1003 = IB-T003, last digit = suite position) and per-file documented intent
(+ `workloads/README.md` design rules). The similarly named files in
`serving-contracts/examples/workloads/` remain NON-NORMATIVE fixtures; this suite is what
fleetlab and reports consume.

**Design decisions (recorded):**
- **Every length distribution is bounded** (anti-pattern rule 12): output distributions are
  capped/directed via max clamps (`chat-short` 384, `rag-long-in` 512, `gen-long-out` 2048 with
  a 512 floor so "long" is guaranteed, `bursty` 512, `cancel-storm`/`slow-client` 1536, `mixed`
  per-component); inputs are bounded too because prompts are generated to the sampled length.
  Enforced in code by `internal/workload/suite_test.go` (fails if anyone reintroduces an
  unbounded normal/lognormal without a max clamp).
- **`shared-prefix` controls its variable:** ratio 0.8, prefix 1024 tokens, group size 16;
  input floor 1152 > prefix length so every sharing request has a unique suffix (sharing is
  designed, not incidental). Ratio-sweep experiments (IB-T011) publish other levels as
  versioned variants.
- **`mixed` blends by declared proportions (60% chat / 25% RAG / 15% gen)** via mixture
  distributions with the same weights on both sides. Known limitation: the schema samples input
  and output independently, so proportions hold per dimension but requests do not carry
  correlated archetype pairs. If correlated pairs become necessary, that is a schema proposal to
  `serving-contracts`, not a local hack. `mixed` v1 keeps prefix/cancel/slow at 0 so it stays
  fully runnable today; adding low-rate cancellation later is a version bump.
- **`bursty` declares amplitude and period** (2 rps base, 10× burst for 15 s, period 75 s,
  repeating) instead of free-form phases, so queueing results can cite "the burst" precisely.
- **Canonical suite is open-loop only** (ADR-0001/0003); closed-loop stays a schema capability,
  not a suite member.
- **Deferral boundary corrected:** IB-T002's loader comment deferred prefix-sharing execution
  "to IB-T003"; the program plan groups all client-side traffic features (prefix-prompt
  construction, cancellation issuance, slow-client throttling) under the streaming-client work,
  so the typed refusal now says **deferred to IB-T004** and IB-T003 ships the suite definitions
  only. Conservative and reversible; no behavior existed to remove.

**Contracts re-pin:** `serving-contracts` **v0.1.0 tag is now cut** (commit `2df9f81`);
re-pinned from pre-release `8c58863` as promised at IB-T002. Diff `8c58863..v0.1.0` touches no
schema semantics (only the `$id` namespace → `github.com/duy-tung`, plus LICENSE/release docs).
`manifest.ContractsBundleVersion` now emits `"v0.1.0"`; `interfaces.md` updated.

**Verification (all commands run 2026-07-10, outputs under `docs/evidence/ib-t003/`):**

1. `gofmt -l .` clean; `go vet ./...` clean; `go test -race -count=1 ./...` green — includes the
   new suite tests (suite complete/versioned/seeded; bounded-distribution rule; open-loop-only;
   prefix-sharing-controlled; runnable-vs-typed-refusal split).
2. **Kit validation vs the released bundle** (extracted read-only via
   `git -C ../serving-contracts archive v0.1.0`): `contracts-validate.py validate --schema
   workload workloads/*.json` → **PASS 8/8**; the 8 derived dry-run variants and all emitted
   run artifacts (5× `events.jsonl` raw-event, 5× `manifest.json` benchmark-run) also PASS —
   26/26 total (`docs/evidence/ib-t003/kit-validate.log`).
3. **Dry-runs vs the pinned mock pair** — gateway + mock-backend built read-only from
   `infergate` @ **`a5a2c02`** (`git -C ../infergate log --oneline -1` at run time; still the
   IG-T002 HEAD) via `git archive`; mock flags `-seed 42 -ttft 20ms -itl 5ms`, gateway in front
   on loopback. `scripts/dryrun-workloads.sh http://127.0.0.1:8180 <out>` derives short
   variants (stop shortened; bursty phases compressed 5×; seeds/distributions unchanged) and
   ran all eight:
   - `chat-short` sent=120 ok=120 errors=0 shed=0, max dispatch slip 43.3 ms, wall 16.6 s
   - `rag-long-in` sent=60 ok=60 errors=0, slip 3.0 ms, wall 31.0 s
   - `gen-long-out` sent=45 ok=45 errors=0, slip 1.1 ms, wall 25.8 s
   - `mixed` sent=100 ok=100 errors=0, slip 3.5 ms, wall 18.8 s
   - `bursty` sent=249 ok=249 errors=0 (three compressed 10× burst cycles), slip 39.0 ms,
     wall 45.8 s
   - `shared-prefix` / `cancel-storm` / `slow-client` → **typed refusal demonstrated** (the
     honest current behavior): `workload feature not implemented in this build:
     prefix_sharing.ratio > 0 | cancellation.rate > 0 | slow_client.fraction > 0 (deferred to
     IB-T004)` (`docs/evidence/ib-t003/*.refusal.log`). Full dry-runs of these three happen when
     IB-T004 lands.
   All runs non-streaming: infergate HEAD `a5a2c02` still rejects `stream: true` (IB-T002
   deviation stands; suite dry-runs rerun streaming once IG-T003 lands).

**Wave-1 exit-review items (same session):** the four ADR statuses annotated to
"Accepted (user review passed at the Wave-1 exit review, 2026-07-10)" — note the files already
carried status "Accepted" since IB-T001 (`f65f15d`), never "Proposed"; the annotation adds the
review provenance. Apache-2.0 `LICENSE` added (copied from `serving-contracts`; user selected
Apache-2.0 for all portfolio repos).

**Evidence:** `docs/evidence/ib-t003/` — per-workload run dirs (manifest, raw events, run log),
`derived/` dry-run variants, `*.refusal.log` typed-refusal transcripts, `kit-validate.log`.

### 2026-07-10 — IB-T002 CO-review fix: latency basis = scheduled send, wire-stage watchdog

**The mandatory fresh-context CO-safety review FAILED on the measurement half** (the
send-schedule half passed). Defect, demonstrated empirically by the reviewer: the latency clock
started at actual wire-write (`httptrace.WroteRequest`) and the slip watchdog measured before
the request goroutine started — so goroutine start, JSON marshal, DNS/TCP/TLS connect, and
blocked body writes were unmonitored, unbounded, excluded from TTFT/E2E, and unrecorded. A probe
with 2 s dial delays (a full accept queue at saturation) hid 2.002 s per request while the run
self-reported VALID — classic coordinated omission, contradicting ADR-0001 §3/§5 and
`testing.md` layer 3 as written.

**Contract amendment consumed:** `serving-contracts` @ `8d81492` (rides the untagged v0.2.0):
raw-event now REQUIRES `scheduled_send_ts` and optionally takes `send_slip_seconds`
(= `send_ts − scheduled_send_ts` ≥ 0), with the normative rule that client-side TTFT/E2E are
measured from `scheduled_send_ts`. **Pin moved v0.1.0 → `8d81492 (v0.2.0 tag pending)`**
(reversible: re-pin to the v0.2.0 tag when cut).

**Fix (all in this repo):**
- `internal/client`: `Request.ScheduledAt` threaded in; TTFT (stream + non-stream) now measured
  from the scheduled send time, never wire-write; `Outcome` carries `ScheduledAt` +
  `SendSlipSeconds` (clamped ≥ 0).
- `internal/events`: `scheduled_send_ts` (required) + `send_slip_seconds` emitted per event;
  `send_ts` stays as the wire-write diagnostic.
- `internal/run`: two-stage watchdog — dispatch stage (as before) plus **wire stage**: after each
  request completes its send, `send_ts − scheduled_send_ts > MaxSlip` ⇒ typed `ErrScheduleSlip`
  run-invalidating abort that also stops the scheduler mid-run. Run **epoch** persisted in the
  run log (`epoch=<RFC3339>`; `scheduled_send_ts(item) = epoch + SendOffset`) so events join
  exactly to the plan. `Result.MaxSlip` renamed to `MaxDispatchSlip` (it shadowed
  `Options.MaxSlip`), `MaxSendSlip` added; canceled events' `elapsed_seconds` uses the same
  scheduled-send basis.
- `internal/schedule`: `Build` now refuses a repeating phase schedule with no positive-rate phase
  (previously an infinite loop).
- ADR-0001 §3 + consequences rewritten to the implemented two-stage semantics and the
  scheduled-send measurement basis (including the at-saturation caveat: sub-threshold target
  connect delay is measured, not absorbed).

**New tests (in `internal/run`):** `TestSlowDialDelayCountsAgainstLatency` — transport with 2 s
dial delay + keep-alives off (accept-queue-full model): every recorded TTFT ≥ 2 s and
`send_slip_seconds` ≈ 2 s (the delay is *included*, run completes under a generous threshold);
`TestSlowDialTripsWireWatchdog` — same transport under a 500 ms threshold: typed
`ErrScheduleSlip (stage=wire)`, scheduler stopped mid-run (sent < planned), ABORT in the run
log; `TestEpochJoinsEventsToPlan` — `scheduled_send_ts == epoch + SendOffset` within 1 ms;
plus `TestLatencyBasisIsScheduledSend` (client), `TestEventMarshalScheduledSend` (events),
`TestAllZeroRateRepeatingPhasesRefused` (schedule). Full suite:
`go test -race -count=1 ./...` → 6 packages ok (run 10.7 s incl. the slow-dial cases).

**Evidence regenerated** (`docs/evidence/ib-t002/` delete-and-replace; the pre-fix artifacts
correctly FAIL the amended schema — verified — and were pre-publication smoke evidence):
- Pinned pair (infergate @ `a5a2c02`), 200 req @ 20 rps: runs A/B/C → 200/200 ok, max dispatch
  slip 4.6/63.2/70.9 ms, max send slip 4.8/63.6/71.1 ms (all ≪ 100 ms threshold).
- Kit FROM `serving-contracts@8d81492` (git archive, read-only; kit selftest GREEN 52/52 + 29/29):
  raw-event + benchmark-run PASS for all five regenerated runs; workload file PASS.
- Determinism unchanged: schedule dumps A==B byte-identical, A≠C (seed 99); response-body
  SHA-256 sets identical A==B and SA==SB (200 each).
- Streaming smoke (informational, unpinned worktree as before): SA/SB 200/200 ok, TTFT p50
  22.6 ms (basis now scheduled-send; mock ttft 20 ms), send-slip p50 0.88 ms.
- `send_slip_seconds` on healthy local runs: p50 ≈ 0.9 ms — the newly measured segment is small
  when the client keeps up, which is exactly what makes it safe to assert on and abort over.

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

### 2026-07-10 — IB-T003 dry-run evidence predates the raw-event v0.2.0 amendment

- **Evidence:** `docs/evidence/ib-t003/*/events.jsonl` were emitted by the pre-CO-fix binary and
  FAIL the amended raw-event schema at pin `8d81492` (missing `scheduled_send_ts`; verified with
  the kit). They passed the pin in force when they were produced (v0.1.0) and carry manifests
  saying so.
- **Decision (conservative, reversible):** left in place for now, scoped only per the directive
  to regenerate the *IB-T002* artifacts; they are dry-run completion evidence (did the workload
  run end-to-end), not latency evidence, and no published claim reads their latency fields.
- **Consequences:** a repo-wide `contracts-verify` sweep at pin `8d81492` would flag them.
- **Follow-up:** regenerate `docs/evidence/ib-t003/` with the fixed binary via
  `scripts/dryrun-workloads.sh` at the next IB-T003-touching change (or IB-T004 start,
  whichever comes first).
