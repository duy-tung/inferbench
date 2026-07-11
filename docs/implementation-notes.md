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

### 2026-07-10 — IB-T004: streaming client correctness (calibration PASSED)

**Scope delivered:** the client-side measurement features that IB-T003 refused with typed
errors are now real, plus the calibration evidence that makes them trustworthy.

- **Monotonic-clock audit (requirement 1):** every latency in the measurement path is a
  `time.Time` subtraction where both stamps come from `time.Now()` or `Add` on such stamps
  (epoch + plan offset), so Go's monotonic reading is always present and wall-clock steps
  cannot corrupt a measurement: `scheduled_send_ts` basis = `epoch.Add(SendOffset)` (run.go),
  wire stamp = `httptrace.WroteRequest` callback, first-byte stamp = `firstByteReader`,
  per-chunk stamps = taken in `readStream` **at line arrival, before any parsing** (IB-T004
  refinement: parse cost can no longer smear a gap), cancel timer = `time.AfterFunc(time.Until
  (scheduledAt.Add(point)))`. Serialized RFC 3339 strings (`events.Timestamp`) are never fed
  back into latency arithmetic (replay/tests parse them only for joining/verification).
  No wall-clock arithmetic was found in the measurement path; audit clean.
- **Cancellation issuance (`cancellation.rate > 0`):** the seeded schedule assigns cancels per
  item (independent PCG streams `CNLS1000`/`CNLP1000`; realized assignment rate asserted in
  tests) and samples the point per the schema trigger. *elapsed-seconds* arms a timer at
  `scheduled_send_ts + point` (plan basis — issuance is response-independent and
  deterministic in intent); *output-tokens* fires when the client has observed N content
  deltas. The cancel is the request-context cancel → connection close (Contract 1 propagation).
  Events record the honest `cancellation_point` (`elapsed_seconds` measured after `send_ts`
  per the raw-event schema, clamped ≥ 0 for pre-send cancels; `output_tokens_at_cancel`);
  TTFT/ITL/tokens measured before the cancel are kept (billable per Contract 1). A planned
  cancel that loses the race against stream completion stays an `ok` event —
  planned-vs-realized counts go to the run log/summary (`cancel_planned=` vs `canceled=`).
  `--stream` is required for the output-tokens trigger (typed CLI refusal otherwise).
- **Slow-client emulation (`slow_client.fraction > 0`):** seeded per-item assignment (stream
  `SLOW1000`); throttled body reads paced at `read_bytes_per_second` (read chunk = bps/10 →
  ~100 ms pacing granularity) with optional `initial_read_delay_seconds`, context-aware so
  cancels/timeouts interrupt slow streams. The client-side mirror series honestly include the
  self-imposed pacing (TTFT includes the initial delay). Caveat recorded in calibration.md:
  throttling is above kernel/transport buffers (~4 KB + socket), so loopback servers run a few
  KB ahead before feeling backpressure.
- **Prefix-sharing prompts (`prefix_sharing.ratio > 0`):** seeded assignment (stream
  `PRFX1000`), sequential group fill of `group_size`, prompt = byte-identical shared prefix per
  group (`workload.SharedPrompt`, PRNG from (seed, group)) + unique per-item suffix; enforced
  in tests (identical prefix within group, no fully duplicated prompt, realized ratio ≈
  declared). Was grouped into IB-T004 by the IB-T003 deferral-boundary note.
- **CO re-review residual fixed:** `Outcome.SendCompleted` tracks whether
  `httptrace.WroteRequest` fired; when it never fired (connect failure, pre-send cancel or
  timeout) the event emits `send_slip_seconds` **ABSENT** (not a fabricated ~0) and `send_ts`
  falls back to the request-start instant — semantics documented in `internal/events/event.go`
  and the client package comment. The wire-stage watchdog skips send-incomplete requests (they
  are classified failure events with full scheduled-send latency, not schedule-keeping
  violations). Contracts proposal recorded below.
- **Also:** completed streams without usage still warn (`UsageMissing` now set only for
  completed 2xx streams — canceled/failed streams legitimately lack usage and fall back to
  client-observed chunk counts, which for cancels is the honest billable count).

**Verification (all commands run 2026-07-10):**

1. `gofmt -l .` clean; `go vet ./...` clean; `go test -race -count=1 ./...` → 6 packages ok.
   New tests: deliberate cancel pre-first-token / mid-stream (elapsed + token triggers, server
   observes the disconnect), cancel-vs-completion race stays `ok`, slow-client bounded read
   rate (+ body integrity), send-slip-absent on connect failure, cancellation/slow/prefix
   planning determinism + realized-rate + group-structure, run-level profile execution
   (schema-shaped canceled events, fast neighbors unaffected by a slow item), send-slip-absent
   marshaling.
2. **Calibration vs the pinned pair PASSED** (`scripts/calibrate-mock.sh`; gateway+mock built
   read-only from infergate @ **`5d69aeb`** — infergate's HEAD advanced twice during this task
   (IG-T003 → IG-T006 → IG-T004 work), so the harness takes an explicit pin argument and
   injects the built commit into every manifest, keeping binaries and manifests desync-proof;
   final evidence pin held at `5d69aeb`): TTFT p50 +2.84 ms / +2.97 ms over configured
   (100 ms / 300 ms points), pooled ITL p50 +0.81 ms / +0.47 ms (20 ms / 5 ms points), all
   within the declared tolerance (TTFT p50 [−2, +15] ms, ITL p50 [−2, +5] ms; p95 bounds also
   met). Full tables + tolerance statement: `docs/evidence/ib-t004/calibration.md`.
3. **Cancellation verified two-sided at all 3 points** (client events AND mock
   `/debug/state`): queued (cancel at 0 s → 30/30 canceled, 0 tokens, no TTFT, 20/30 sends
   never completed → slip ABSENT, mock `requests_total: 0`), pre-first-token (0.15 s < TTFT
   0.3 s → 30/30 canceled, mock 30× `phase=pre_first_token` `chunks_sent=0`), mid-stream
   (8 output tokens → 29/30 canceled with exactly 8 tokens at cancel + kept TTFT/ITL, mock
   29× `phase=mid_stream` `chunks_sent=9`; 1 short stream completed ok — honest realized
   accounting).
4. **Slow-client bounded rate demonstrated:** e2e p50 2.113 s at 4096 B/s vs 0.230 s
   full-speed control (9.2×), TTFT p50 0.223 s includes the 0.2 s first-read delay, ITL
   bimodal at the 100 ms pacing granularity.
5. **IB-T003 evidence regenerated** (deviation follow-up closed): full 8/8 suite dry-run
   green, STREAMING, vs the pair @ `5d69aeb` (`scripts/dryrun-workloads.sh`; shared-prefix
   60/60 ok, cancel-storm 90 sent / 7 canceled of 39 planned — most sampled points 0.2–3.0 s
   land after the mock's short streams end (mock caps completions at 256 tokens), honest
   planned-vs-realized; slow-client 40/40 ok, wall 50 s dominated by throttled reads).
6. **Kit validation green at pin `8d81492`**: 53/53 PASS — canonical suite (8 workloads),
   regenerated ib-t003 artifacts (8 derived workloads + 8 events.jsonl + 8 manifests), ib-t004
   scenarios (7 workloads + 7 events.jsonl + 7 manifests). `docs/evidence/ib-t004/
   kit-validate.log`.

**Contracts observations (proposals for the orchestrator — NO local schema/contract edits):**

1. **`metrics/metrics.md` §4 "Client-side TTFT" start point is stale** relative to the
   raw-event v0.2.0 amendment (`8d81492`): metrics.md still says start = "client completes
   writing the request body", while raw-event.schema.json (same repo, same pin) normatively
   defines client TTFT from `scheduled_send_ts` (CO safety). The schema is what this repo
   emits against; metrics.md §4 should be re-worded to match at the next contracts release.
2. **`send_ts` should arguably be nullable** for requests whose send never completed; today it
   is required non-null, so this repo emits the documented request-start fallback with
   `send_slip_seconds` absent. If contracts agree, a nullable `send_ts` (null exactly when the
   body write never finished) states the truth directly.
3. **Workload `cancellation.point` trigger `elapsed-seconds` says "since send"** — ambiguous
   between scheduled send and wire send. Implemented as *scheduled* send (deterministic,
   response-independent issuance), while the raw-event `cancellation_point.elapsed_seconds` is
   recorded relative to `send_ts` per its schema text. Suggest the workload schema say
   "since the scheduled send time" explicitly.

**Evidence:** `docs/evidence/ib-t004/` (calibration.md, per-scenario run dirs with manifest /
events / run log / derived workload / stats / mock debug-state, infergate-pin.txt,
kit-validate.log) and regenerated `docs/evidence/ib-t003/`.

### 2026-07-10 — IB-T005: analysis core (Python)

**Contracts pin:** serving-contracts @ `8d81492` (raw-event v0.2.0 basis: latency from
`scheduled_send_ts`). **Scope:** `analysis/` Python package (src layout,
`inferbench_analysis`) + known-answer test suite + end-to-end analysis of the real
IB-T002/IB-T004 evidence runs into kit-valid benchmark-result files.

**Dependency decision:** numpy (percentiles, vectorized bootstrap) + jsonschema (pinned-bundle
validation of everything consumed and emitted) only. pandas rejected — the unit of work is a
list of typed per-request events, no relational operations exist; scipy rejected — percentile
bootstrap and the knee method are elementary numpy; PyYAML rejected — every artifact is JSON.

**Honesty rules made structural (G4 will attack these; they are code, not convention):**

1. **Pooling, never averaging (rule 5):** `PercentileTable` is constructible ONLY via
   `pooled_table(samples_by_run)` which concatenates raw samples before computing anything;
   direct construction (the path an averager would need) raises `PoolingGuardError`; no API
   accepts two tables; the emitted `pooled_percentiles.method` is the schema const
   `pooled-raw-events`. Guard test constructs runs where averaging per-run p99s yields 2.0 but
   the pooled p99 is 3.0 and pins the pooled answer. Cross-run dispersion is a separate
   `RunDispersion` type (median ± range of per-run summaries, rule 4) that cannot occupy a
   table slot. Known residual: Python cannot stop a *deliberate* bypass (e.g.
   `dataclasses.replace` on a real table); the guard blocks the accidental path and the
   known-answer test catches the deliberate one.
2. **Goodput adjacency (rule 7):** `evaluate_goodput()` is a single pass returning one frozen
   `Goodput` object carrying ratio + `shed_rate` + `stall_rate` + `slo_ref` + stall threshold;
   no bare-goodput API exists; an SLO without a `max_stall_seconds` upper bound is a typed
   refusal (no stall threshold → no stall rate → no goodput at all). Mirrors the contract's
   goodput block exactly. Per-request evaluation is DistServe-style attainment; canceled/shed/
   errored requests count in the denominator and never meet.
3. **Error/shed gate (CO re-review requirement):** when measured-window error+shed rate
   exceeds the DECLARED threshold (default 0.05, echoed everywhere), the result's latency
   field becomes `WithheldLatency` (kind + reason + rates) — the percentile tables do not
   exist on the object, and the reason is appended to `threats_to_validity`. A 100%-timeout
   run is analyzed as a VALID run (throughput 0 ok/s, error accounting, goodput 0 with rates
   visible) whose latency table is absent. Deliberate cancellations do not count toward the
   gate (workload features, not failures). A second withholding kind (`no-samples`) covers
   contract-required signals with zero samples (e.g. every request canceled pre-first-token).
4. **Warm-up (rule 2):** policy read ONLY from the manifest (no override parameter exists),
   applied per repetition in scheduled-send order; exclusions counted into
   `validity.warm_up_handling`; excluding every event is a typed refusal; policy `none` always
   yields a threats entry.
5. **Comparability (rule 10):** pooling refuses manifests differing on any comparability key
   (topology/workload/engine/model/hardware/gateway/warm-up) and duplicate run_ids
   (double-count/cherry-pick guard).
6. **Self-validating emission:** `to_benchmark_result_dict()` validates against the pinned
   schema before anything is written; a withheld-latency result is a typed
   `ResultNotExpressibleError` (CLI exit 3) — no schema-invalid or number-fabricating artifact
   can be produced.

**Finalized statistics parameters** (recorded in ADR-0002 changelog): percentiles = linear
interpolation (Hyndman–Fan 7) on the pool; p999 only at n ≥ 1000; bootstrap = percentile
method, B=1000, 95% interval, seed 20260710 (CLI-overridable), measured coverage on Exp(1)
0.913 (p50) / 0.958 (p90) at nominal 95%; knee = plateau-departure (median of lowest ⌊n/3⌋
rates, 1.5× factor, sustained departure) + kneedle cross-check (agree → confidence 0.8, else
0.5, edge knee unbracketed and capped 0.5), ≥6 points required, limitations emitted in the
method string; gate threshold default 0.05.

**Verification (all commands run 2026-07-10):**

1. `cd analysis && CONTRACTS_BUNDLE=/path/to/serving-contracts python -m pytest -q` →
   **70 passed** (exact percentiles incl. 1..100 known answers; pooled≠averaged guard;
   bootstrap reproducibility + bracketing + coverage on Exp(1) with analytic p50/p90;
   warm-up counts for all three policies incl. shuffled-input invariance; gating trip/pass/
   100%-timeout/cancel-exemption/no-samples; goodput known ratios + structural refusals;
   synthetic knees (placed knee at rate 7 found; spike-then-recover not a knee; flat sweep →
   None; edge knee unbracketed; <6 points refused); cost known answers + provenance honesty;
   loader refusals against the pinned schemas incl. a pre-v0.2.0 event (missing
   `scheduled_send_ts`) being refused; emission schema-validity). Schema-dependent tests skip
   loudly if `CONTRACTS_BUNDLE` is unset — CI must fetch the pinned bundle per the kit README
   wiring (follow-up noted below).
2. **End-to-end on real evidence** (`scripts/analyze-evidence.sh /path/to/serving-contracts`,
   log: `docs/evidence/ib-t005/analyze.log`): 9 runs analyzed against the measured-basis SLO
   `docs/evidence/ib-t005/mock-loopback.slo.json` (model-serving SLOs must be
   measurement-derived; thresholds = measured suite maxima rounded up: TTFT ≤ 0.75 s from max
   0.634 s, e2e ≤ 5.0 s from max 3.112 s, stall ≤ 0.25 s from max 0.227 s, sources in the
   file). **7 results emitted** (smoke-A, stream-SA, calib-A/B, slow-control, slow-on,
   cancel-mid-stream), each with pooled tables + bootstrap CIs on stdout, goodput with
   shed/stall adjacent, knee null + threat (no sweep), **cost null + validity note (mock runs
   have no cost profile — honest)**, warm-up 'none' disclosed as a threat, run_count=1
   below-minimum threat. **2 typed refusals as designed** (cancel-queued,
   cancel-pre-first-token: valid runs, zero TTFT samples, exit 3, no file).
3. **Kit validation green** (`docs/evidence/ib-t005/kit-validate.log`): selftest GREEN,
   SLO instance 1/1 PASS, `check docs/evidence/ib-t005/results` → **7/7 benchmark-result
   PASS** at pin `8d81492`.

Sample pooled numbers (calib-A, 120 events, mock configured TTFT 100 ms / ITL 20 ms): client
TTFT p50 0.1028 s / p99 0.1050 s (95% CI [0.1045, 0.1188]); pooled ITL n=7530, p50 0.0208 s,
p999 0.0488 s — consistent with the IB-T004 calibration deltas. The `unexplained_anomalies: []`
claims in the emitted results rest on the IB-T004 calibration review of these same events
(deltas explained there) plus the per-run summaries in `analyze.log`.

**Contracts observations (proposals for the orchestrator — NO local schema edits):**

4. **benchmark-result has no expressible form for a valid run whose latency table is
   withheld/empty.** `pooled_percentiles.tables` requires `ttft_seconds` and
   `e2e_duration_seconds` as numeric percentileTables (n ≥ 1), so (a) a 100%-shed/timeout or
   all-canceled-pre-first-token run and (b) an error/shed-GATED run cannot emit a schema-valid
   result even though they are valid runs with meaningful throughput/goodput/validity data.
   IB-T005 handles this honestly (typed `ResultNotExpressibleError`, exit 3, reason in the
   in-memory validity block) but the run's non-latency aggregates are then file-less. Proposal:
   allow each table (or the tables block) to be `null` with a required sibling reason string,
   mirroring the knee/cost null-with-validity-note pattern.
5. **percentileTable has no CI fields** (`additionalProperties: false`), so the bootstrap CIs
   this repo computes cannot ride in the result file and live only in reports/logs. Proposal:
   optional `p50_ci`/`p90_ci`/`p95_ci`/`p99_ci` `[lo, hi]` pairs plus a `ci_method` string —
   additive MINOR.

**Follow-up:** wire `analysis` pytest + the kit sweep into CI with the pinned-bundle fetch
(kit README §"Wiring") so the loud skips can never pass silently in CI; IB-T006 renders the
dispersion objects and bootstrap CIs that the contract file cannot carry.

**Evidence:** `analysis/` (package + tests), `docs/evidence/ib-t005/` (mock-loopback.slo.json,
results/ ×7, analyze.log, kit-validate.log), `scripts/analyze-evidence.sh`.

### 2026-07-11 — IB-T006: report generator + validity block

**Contracts pin:** serving-contracts @ `8d81492` (unchanged). **Scope:** `report` subcommand in
the analysis CLI + `inferbench_analysis/report.py` (renderer), report test suite, end-to-end
sample reports from the real IB-T004/IB-T005 evidence. Package version 0.1.0 → 0.2.0.

**Design decision — the "template" is code, not a template file.** The task's attack surface
(G4) is "render a report without the validity sections". A jinja2/file template is exactly the
artifact those sections would get quietly stripped from, so the template is one renderer
(`report._render`) with a FIXED section order baked into code and no parameter to skip a
section; jinja2 was justified out in `analysis/pyproject.toml` (no new dependencies — stdlib
string building only).

**Honesty rules made structural (all typed `ReportInputError` refusals, tested):**

1. **No validity block → no report.** Both builders (`report_from_analysis` from the in-memory
   `AnalysisResult`; `report_from_result_dict` from an emitted benchmark-result file) refuse
   inputs lacking warm_up_handling / run_count / threats_to_validity / unexplained_anomalies —
   the file path re-checks structurally even though the CLI schema-validates first, so a
   validation bypass still refuses. `validity.warm_up_handling` is cross-checked against the
   manifest's declared warm-up policy (inconsistent artifacts refused).
2. **Hypothesis displayed prominently.** First section under the title, blockquoted per run;
   a manifest without a hypothesis is not reportable.
3. **Goodput never without shed + stall.** Rendered as one table: ratio, req/s meeting SLO,
   shed rate, stall rate (with threshold and stalled/streaming counts) — missing `shed_rate`,
   `stall_rate`, or the SLO reference refuses. Stall-rate-beside-goodput is the study-track
   artifact of the goodput-critique paper (arXiv 2410.14257), now encoded in the template.
4. **Withheld latency renders WHY, never a blank table.** Both kinds (`error-shed-gate`,
   `no-samples`) render the reason, the measured error/shed rates, the declared threshold, the
   statement that the run remains VALID, and that the report is the publishable artifact (the
   pinned contract has no null-table form — IB-T005 contracts observation). Exactly one of
   tables/withheld must be present; a blank latency section is unconstructible.
5. **Anomalies never silently empty.** Either enumerated, or "**None observed.**" beside the
   list of checks that were run (analysis mode fills the gate/window slots with the run set's
   actual numbers); empty anomalies + empty checks list refuses.
6. **Comparability rule VERBATIM.** Embedded from `compatibility-policy.md` §7 at the pin and
   printed in every report (the benchmark-result schema requires this); when the bundle is
   supplied at render time the embedded copy is checked against the bundle's policy file and
   drift is a typed refusal (re-pin updates the constant + pin note together).
7. **Closed-loop visibly flagged.** Arrival process read from `workload.json` beside the
   manifest: closed-loop → banner at the top + per-manifest flag + latency-section reminder;
   NO workload file → "arrival process NOT inspectable … open-loop arrivals are NOT implied"
   (absence of evidence is not open-loop evidence).
8. **`cost: null` always says why.** Analysis mode carries `CostUnavailable.reason`; file mode
   recovers the reason from threats_to_validity, and a null cost with no recorded reason is
   rendered as itself a validity gap.
9. **No mean-only tables.** Mean is one column beside n/p50/p90/p95/p99/p999/max, with the
   explicit anti-mean-only note; bootstrap CIs and cross-run dispersion (median ± range)
   render in analysis mode with the statement that the pinned result schema cannot carry them.
10. **Rule-8 one-command repro.** The CLI reconstructs its own invocation (`shlex.join`) into
    the report, plus pins (contracts bundle version from the manifests, analysis version);
    an empty repro command refuses.

**Emission not duplicated:** `report` only consumes results. `analyze` (IB-T005) remains the
only emitter; for valid runs whose latency is withheld (analyze exit 3, no file possible),
`report --run` is the publishable surface — CLI help and `docs/interfaces.md` updated to say so.

**Verification (all commands run 2026-07-11):**

1. `cd analysis && CONTRACTS_BUNDLE=/path/to/serving-contracts python3 -m pytest -q` →
   **103 passed** (70 IB-T005 + 33 new report tests: mandatory-section presence AND fixed
   order; hypothesis-before-results; validity-absent/incomplete refusals; warm-up
   inconsistency refusal; withheld rendering for both kinds incl. goodput still shown with
   rates; goodput-shed-stall adjacency in one table + refusals for missing
   shed_rate/stall_rate/slo_ref; anomalies-never-silent (none-observed + checks; listed
   anomalies; structural refusal when checks stripped); cost-null WHY in both modes + the
   no-recorded-reason callout; closed-loop flag / open-loop description / missing-workload
   non-implication; full-percentile-table + CI rendering; verbatim-rule drift guard against
   the real pinned bundle; result-file round trip + CLI report from a result file).
2. **End-to-end on real evidence** (`scripts/report-evidence.sh /path/to/serving-contracts`,
   log: `docs/evidence/ib-t006/report.log`): 3 reports rendered, exit 0 each —
   `ib-t006-calib-A.report.md` (**the primary G4 sample**: from raw events, full manifest +
   hypothesis, pooled tables + bootstrap CIs, goodput/shed/stall table, null knee + null cost
   with reasons, validity block, none-observed anomalies with 8 checks, one-command repro);
   `ib-t005-calib-A.report.md` (from the emitted result file — consumption path, CI absence
   stated); `ib-t006-cancel-queued.report.md` (**withheld case**, kind `no-samples`: 30/30
   deliberately-canceled-at-dispatch requests, zero TTFT samples — WHY block rendered, gate
   accounting shown, goodput 0 with rates visible). The ib-t004/ib-t005 evidence contains no
   error/shed-GATED run (mock error_rate 0 everywhere); the `error-shed-gate` rendering is
   exercised by known-answer unit tests instead (10%-timeout synthetic run), and cancel-queued
   covers the real withheld path.
3. **Kit validation green** (`docs/evidence/ib-t006/kit-validate.log`): consumed result files
   re-checked at pin `8d81492` → **7/7 PASS**. No result files were (re)emitted by IB-T006 —
   the report generator consumes them (IB-T005 owns emission), so there was nothing new to
   kit-validate.

**Evidence:** `analysis/src/inferbench_analysis/report.py`, `analysis/tests/test_report.py`,
`docs/evidence/ib-t006/` (3 reports, report.log, kit-validate.log), `scripts/report-evidence.sh`.

### 2026-07-11 — IB-T008: sweeps, replay, comparison mode

**Contracts pin:** re-pinned `8d81492` → **`v0.2.0` tag (commit `484b449`)** — `git diff
8d81492..484b449 --stat` touches only `RELEASES.md`, no schema semantics.
`manifest.ContractsBundleVersion` now emits `"v0.2.0"`.

**Scope delivered:** `internal/structdiff` (one JSON-round-trip structural diff primitive),
`internal/manifest.Diff`/`ImpliedFields` (the bookkeeping-exempt, schema-coupling-aware wrapper
used everywhere the single-variable rule is checked), `internal/replay` (schedule fingerprint +
reference verification), `internal/sweep` (pure mechanics: rate-point placement, capacity-probe
math, single-rate-variable workload derivation), and four `cmd/inferbench` files replacing the
single-subcommand `main.go`: `common.go` (shared `runOnce` — every subcommand now executes a
request through the exact same path `run` always did), `run.go`, `sweep.go`, `replay.go`,
`compare.go`, `experiment.go` (IB-T009, same session).

**Capacity-estimate procedure (documented, experiments.md rule 3):** a short **open-loop
overload probe** — one run at a rate declared to comfortably exceed any plausible capacity, for
a bounded request count — measures achieved sustained throughput (`ok_count / elapsed_seconds`).
`sweep` refuses (typed `sweep.ErrProbeDidNotSaturate`) an estimate where achieved throughput
stayed within 85% of the offered probe rate — the probe never fell behind, so the estimate would
be unreliable. ADR-0003 sanctions exactly this kind of run ("closed-loop... narrowly, for
throughput-ceiling discovery and capacity estimation for sweep-range placement"); this repo used
an open-loop overload probe instead of implementing closed-loop dispatch (see Deviations).

**Mock saturation is client-modeled, and that is disclosed, not hidden.** Reading
`infergate/cmd/mock-backend/main.go` at the pinned commit confirms its flag set is
`addr/seed/ttft/itl/error-rate/created/stream-fail-after-chunks` only — no concurrency limiter,
no admission control, no queueing model; every request is served in constant configured time
regardless of concurrent load, and the gateway at this pin has none either (admission control is
IG-T010, not yet released). A first attempt at a plain rate sweep against this pair, therefore,
showed essentially flat TTFT across the whole 10%–120% range (measured: p95 0.313s → 0.335s,
~7% growth — nowhere near a 1.5x departure). `sweep --max-conns N` holds a client-transport
`MaxConnsPerHost` cap fixed across the probe and every point, modeling a capacity-limited target
for verification purposes: Go's `http.Transport` **blocks** (queues) new requests once the cap
is reached rather than failing them (confirmed via the Go stdlib docs), so the resulting
queueing delay is genuine client-observed latency — measured via the scheduled-send basis
(ADR-0001's "at-saturation caveat": "connect delay caused by the target is client-visible
queueing and is measured... Raising `--max-slip` for overload studies is legitimate only because
the slip is recorded per event either way" — the same reasoning applies to a connection-pool
wait). This is a **verification-harness technique for a mock target that does not model its own
capacity, never a production claim**; a real target's real admission control (infergate IG-T010)
will saturate on its own once released, and IB-T010's published sweeps will not need this knob.
Documented in the `sweep` CLI help, `internal/sweep`'s package doc, and
`scripts/sweep-mock.sh`'s header comment.

**Tuning to a decisive knee (empirical, recorded for reproducibility):** `max-conns=2`,
mock `ttft=20ms itl=5ms`, output tokens `uniform(8,32)`, `150 requests/point/repetition`, 6
points 10%→120% of a probe-estimated capacity. Little's-law sanity check: per-connection
throughput ≈13.7 rps (measured from an earlier max-conns=5 trial achieving ~68.5 rps), so
capacity ≈ 2 × 13.7 ≈ 27.4 rps — matches the measured probe (27.79–28.28 rps across trials). The
key tuning insight: for a FIXED percentage overload (e.g. 120% of capacity), the absolute
queueing delay accumulated over N requests is `N × (1 − capacity/rate) / capacity` — inversely
proportional to capacity — so a SMALL absolute concurrency cap turns the same 20% overload into
a much larger, more clearly measurable absolute delay than a large cap would. Two earlier trials
(max-conns=5 at 100%–200% fractions, and max-conns=3–4 with larger fractions) either timed out
(low-rate points at a fixed high request count take a long time when the rate is a small
fraction of a large capacity) or showed only marginal p99 growth (~1.03x, short of the 1.5x
departure factor) — both recorded here so the final parameters are not presented as the first
thing tried.

**Verification (all commands run 2026-07-11):**

1. `gofmt -l .` clean; `go vet ./...` clean; `go test -race -count=1 ./...` → **11 packages ok**
   (adds `internal/structdiff`, `internal/replay`, `internal/sweep`, `cmd/inferbench` to the
   IB-T002–T006 set). New tests: structural diff (nested leaf, map field, presence mismatch);
   manifest Diff (bookkeeping-exempt, declared-variable-found, engine-flag leaf, identical→empty);
   schedule Fingerprint (deterministic, seed-sensitive, rate-only-affects-offsets — the executable
   proof that a rate sweep varies nothing but arrival timing); replay Reference (round trip,
   same-seed passes, seed/name-mismatch refused); sweep math (rate-point spacing/count/refusals,
   capacity estimate saturated/unsaturated/zero, derive-rate-workload + single-variable pass/catch).
2. **Bug found BY this task's own tests, fixed same day:** `onceParams.SeedOverride` was an
   `int64` with a `< 0` = "no override" sentinel; `cmdRun` set it explicitly (`-1` default), but
   `sweep`'s point/probe calls and `compare`'s arm calls did not set it at all, so the Go zero
   value (`0`) silently overrode every derived run's seed to 0 — discovered while smoke-testing
   `compare` (`comparison.json` showed `"seed": 0` instead of the workload's declared
   `8008001`). **Fixed** by changing the field to `*int64` (nil = no override — a pointer's zero
   value IS the safe default, so the bug class is now unreachable by construction, not just
   patched at the two call sites that had it). Added `cmd/inferbench/common_test.go`
   (`TestRunOnceNilSeedOverridePreservesWorkloadSeed`, `TestRunOnceSeedOverrideApplies`) as a
   permanent regression test — this package had zero tests before this task.
3. **End-to-end vs the pinned mock+gateway pair** (infergate @ **`74f2372`**, built read-only via
   `git archive`; `scripts/sweep-mock.sh docs/evidence/ib-t008 ../infergate 74f2372`):
   - **Sweep**: capacity estimate 27.79 rps; 6 points (2.78/8.89/15.01/21.12/27.24/33.35 rps =
     10/32/54/76/98/120% of capacity), 3 repetitions/point, 450 requests/point, 0 errors/shed.
     Pooled TTFT p99 per point: 0.167s, 0.168s, 0.214s, 0.341s, 0.659s, 1.189s.
   - **Knee detection** (`analysis/knee.py` `detect_knee`, unmodified from IB-T005):
     `arrival_rate_rps=21.12 rps, confidence=0.8, bracketed=true`; kneedle cross-check agrees.
     **This is the IB-T008 stop condition.**
   - **Replay**: an original run (60 streaming requests, seed 8008030) and a `replay` re-issue
     against the same target produced identical `schedule_fingerprint` (both
     `9400a741...b4e2e1`) AND identical response-body SHA-256 sets (60/60). A mutated-seed
     negative control was refused (`replay.ErrMismatch`, exit 1) before any request was sent.
   - **A/B compare**: direct-vs-gateway (`target_topology`, single declared variable), 40
     req/arm, 0 errors both arms, `comparison.json` records `diff_fields: [gateway,
     target_topology]` (gateway is the schema-implied companion of target_topology, not a
     second variable). A second negative control (an extra `model.checkpoint` difference)
     was refused before any request was sent.
   - **Kit validation**: 56/56 emitted artifacts (23 `benchmark-run`, 23 `raw-event`, 10
     `workload`) PASS at the `v0.2.0` pin.

**Evidence:** `docs/evidence/ib-t008/` (`sweep/` incl. `sweep.json` + 19 run dirs,
`knee-result.json`, `knee-detection.log`, `replay-original/`, `replay-reissue/`,
`replay-mismatch.log`, `compare/`, `compare-refused.log`, `kit-validate.log`),
`scripts/sweep-mock.sh`.

### 2026-07-11 — IB-T009: controlled-experiment framework

**Scope delivered:** `internal/experiment` (`Hypothesis` load/validate, `CheckArms` reusing
`internal/manifest.Diff`, `CheckGPUSession`), `hypotheses/` (`README.md`, `TEMPLATE.json`, the
compliant demo hypothesis), `cmd/inferbench/experiment.go` (`experiment {run,sweep,compare}`).

**Hypothesis file format is JSON, not the `docs/experiments.md` §5 YAML sketch.** Same field
set (`id`, `hypothesis`, `variable`, `levels`, `expected_direction`, `workload`, `constants`,
`stop_condition`, `repeat_policy`, optional `slo_reference`/`provenance_notes`, optional
`gpu_session` for G6), different serialization. The Go module is stdlib-only end to end
(IB-T002 note above); every other artifact it reads or writes is JSON; a YAML dependency for one
file type was judged not worth it. Unknown fields are rejected
(`encoding/json.Decoder.DisallowUnknownFields`), so a mistyped plural `"variables": [...]`
(a matrix-shaped mistake) is a decode-time refusal, not a silently accepted second variable.
Recorded as a reversible decision (Deviations below); revisit if a future task needs to consume
hypothesis files written by a human against the literal §5 YAML template.

**One mechanism, two call sites.** `Hypothesis.CheckArms` does not reimplement the
single-variable check — it calls `internal/manifest.Diff` (the primitive `compare` and `sweep`
already use, built same-session for IB-T008) and requires the result to be a subset of
`{declared variable} ∪ manifest.ImpliedFields(variable)`. "Enforce the single-variable rule" and
"guard against combinatorial/full-matrix sweeps" are explicitly the same requirement in
`docs/experiments.md` §5 ("Single-variable rule enforced twice: at hypothesis intake and again
by the comparison engine") — implementing them as one function call from two places (`compare`'s
own pre-check, and `experiment compare`'s governance check) means they cannot silently drift
apart the way two independently-written checks could.

**GPU session (G6), demonstrated without a live GPU.** `CheckGPUSession` runs on the loaded
manifest facts BEFORE any network I/O (reachability, request), so the refusal is structural and
testable on CPU: a facts file with `hardware.gpu_count=1`/`gpu_model` set and no `gpu_session`
block in the hypothesis is refused (`experiment.ErrGPUSessionRequired`); the same manifest with
a complete `gpu_session` block passes. IB-T011 (real GPU sessions) is unaffected in scope — this
only proves the gate exists and fires.

**Verification (all commands run 2026-07-11):**

1. `go test -race -count=1 ./...` green, `internal/experiment` tests: empty-path and
   missing-file refusal (both `ErrHypothesisRequired`); complete-hypothesis accept; 8
   incomplete-field cases (each `ErrIncompleteHypothesis`); unknown-field (`variables` plural)
   refusal; `CheckArms` single-variable pass (`target_topology`, `gateway` correctly treated as
   implied, not a second variable); `CheckArms` combinatorial refusal (`ErrCombinatorial`);
   `CheckArms` degenerate/non-varying refusal (`ErrNotVarying`); `CheckGPUSession`
   required-and-missing / complete / not-required-for-CPU.
2. **Refusal-demo transcript** (`scripts/experiment-demo.sh
   docs/evidence/ib-t009 ../infergate 74f2372`, vs the pinned mock+gateway pair):
   6 refusals, **every one before any request left the process** —
   `experiment run|sweep|compare` with no `--hypothesis` (3 separate invocations); an incomplete
   hypothesis file (`stop_condition` deleted); a combinatorial 2-arm set (`target_topology`
   declared, `model.checkpoint` also differs); a `--variable`/`hypothesis.variable` mismatch; a
   GPU-declaring manifest with no `gpu_session` block. **Then one compliant hypothesis-gated
   `experiment compare` run to completion**: direct-vs-gateway, `target_topology`, 40
   requests/arm, 0 errors — `docs/evidence/ib-t009/compliant-compare/comparison.json`.
   Transcript: `docs/evidence/ib-t009/refusal-demo-transcript.md`.
   - **Fixture bug caught by the demo itself, fixed in the script (not the framework):** the
     first version of the "compliant" demo gave the two arms' manifests *different* freeform
     `hypothesis` text ("gateway arm" vs "direct arm" descriptions), which `CheckArms` correctly
     flagged as a second differing field (`hypothesis`) beyond the declared `target_topology` —
     because `manifest.Hypothesis` restates the SAME experiment-level falsifiable statement on
     every arm (methodology rule 6), not a per-arm label. This is the guard working as intended
     against sloppy fixture authoring, not a framework defect; fixed by giving both arms the
     identical hypothesis text.
3. **Kit validation**: the compliant run's emitted artifacts (2 `benchmark-run`, 2 `raw-event`,
   1 `workload`) PASS at the `v0.2.0` pin.

**Evidence:** `docs/evidence/ib-t009/` (`refusal-demo-transcript.md`, `compliant-compare/`,
`kit-validate.log`), `hypotheses/`, `scripts/experiment-demo.sh`.

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
- **RESOLVED 2026-07-10 (IB-T004):** evidence regenerated with the IB-T004 binary as a full
  8/8 streaming suite dry-run vs infergate @ `5d69aeb`; everything under
  `docs/evidence/ib-t003/` now kit-validates at pin `8d81492` (see the IB-T004 log entry and
  `docs/evidence/ib-t004/kit-validate.log`). This also closes the IB-T002 deviation above:
  the pinned live streaming verification happened (IG-T003 landed at infergate HEAD).

### 2026-07-11 — IB-T008: closed-loop arrival execution stays deferred; capacity estimation uses an open-loop overload probe instead

- **Evidence:** `internal/workload.Workload.CheckRunnable` has returned a typed
  `ErrNotImplemented` for `arrival_process.type == "closed-loop"` since IB-T002, with a comment
  pointing at IB-T008 as the eventual owner. `docs/tasks.md`'s IB-T008 requirement text, however,
  is scoped to "rate sweeps... replay... A/B comparison" — it does not require closed-loop
  dispatch. ADR-0003 sanctions closed-loop *only* for "throughput-ceiling discovery and capacity
  estimation for sweep-range placement", which is exactly what the sweep's capacity probe needs
  — and that need is met by an OPEN-LOOP run at an overload rate (see the IB-T008 log entry):
  at an offered rate far above the true ceiling, achieved throughput saturates at the same
  ceiling either way (Little's law), so the open-loop probe measures the same quantity a
  closed-loop probe would, without adding a second dispatch mode (closed-loop's send times are
  response-coupled, which does not fit the precomputed-`schedule.Plan` model the rest of the
  generator relies on for determinism/replay) to this task's scope.
- **Decision (conservative, reversible):** implement the capacity-estimate procedure with an
  open-loop overload probe; leave `CheckRunnable`'s typed refusal for `closed-loop` unchanged
  and update its comment to stop pointing at IB-T008.
- **Consequences:** none of the eight canonical workloads declare `closed-loop` arrival (IB-T003
  design decision: "canonical suite is open-loop only"), so no suite workload is affected. A
  future task that specifically wants closed-loop-mode throughput-ceiling numbers (as opposed to
  sweep-range placement, which is served today) would need to implement response-coupled
  dispatch as new scope.
- **Follow-up:** none planned; re-open only if a specific downstream need for literal
  closed-loop execution (not just ceiling estimation) appears.

### 2026-07-11 — IB-T009: hypothesis files are JSON, not the `docs/experiments.md` §5 YAML template

- **Evidence:** `docs/experiments.md` §5 sketches the hypothesis-file fields as a YAML block.
  The Go module (`go.mod`) has been stdlib-only since IB-T002 by deliberate choice; every
  artifact the generator reads or writes (workload, manifest, raw-event) is JSON; the Python
  analysis side rejected PyYAML for the same reason at IB-T005 ("every artifact is JSON").
- **Decision (conservative, reversible):** `internal/experiment.Hypothesis` is JSON
  (`encoding/json`, `DisallowUnknownFields`), field set identical to the §5 template. No new
  dependency added to `go.mod`.
- **Consequences:** a hypothesis file hand-authored from the literal §5 YAML template needs a
  one-time JSON conversion (mechanical: same field names, YAML→JSON). `hypotheses/README.md`
  and `hypotheses/TEMPLATE.json` document the JSON shape directly so this is a non-issue for new
  hypothesis files going forward.
- **Follow-up:** none planned; revisit only if a cross-repo consumer specifically needs to parse
  hypothesis files as YAML (none currently does — hypothesis files are inferbench-local
  governance artifacts, not a contracts-owned schema).

## IB-T010 — Experiment set 1 (CPU): gateway overhead + admission value (2026-07-11)

Benchmark report #1: `docs/evidence/ib-t010/benchmark-report-1.md` (roll-up), 7 per-point
generated reports beside it, 7 kit-valid benchmark-result files (60/60 artifacts PASS at
contracts pin v0.2.0 = `484b449`). Pins: infergate `6827d8c` (built read-only via
`git archive`), llama.cpp `8f114a9`, model qwen2.5-1.5b-instruct-q4_k_m
(sha256 `6a1a2eb6…407e`). Hypothesis files authored BEFORE any load
(`hypotheses/EXP-ib-t010-e1-gateway-overhead.json`, `EXP-ib-t010-e2-admission-value.json`);
every load-generating invocation went through `inferbench experiment {run,compare}` (IB-T009
gating) except the E2 capacity probe, which used `sweep` probe-only per ADR-0003.

Measured facts (all 2026-07-11, this host, loopback, 4 vCPU — full context in the report):

- **E1 mock (CONFIRMED):** paired per-request gateway overhead p50 +1.04 ms / p95 +2.21 ms /
  p99 +2.81 ms (630 pairs, warm-up excluded); pooled Δp95 +1.15 ms; gateway-side
  `inference_queue_wait_seconds` p95 <1 ms. Program SLO p95<10 ms / p99<20 ms met on both
  declared bases (paired AND pooled-delta). LiteLLM baseline re-verified 2026-07-11
  (docs.litellm.ai/docs/benchmarks: "8ms P95 at 1k RPS", 4-instance overhead table
  2/8/13 ms via its own `x-litellm-overhead-duration-ms` header) — magnitude framing only,
  no cross-tool claim (different basis + hardware; comparability rule).
- **E1 llama.cpp (INCONCLUSIVE at ms scale):** real-engine TTFT variance (direct per-rep p95
  3.70→2.32→1.74 s across reps; sequential arms against one warming server instance)
  is 2–3 orders of magnitude above the 10 ms bound; paired median −8.5 ms. Honest reading:
  no detectable added overhead at the engine's own noise scale; the mock arm is the
  resolving instrument for the ms claim.
- **E2 (G5) — REFUTED on the strict ≤20% criterion, companions held:** capacity probe
  37.66 rps (sane admission: budget 6, queue cap 3, deadline 500 ms — declared in the
  hypothesis before running); accepted-request TTFT p95 0.1612 s at 1× vs 0.2018 s at 5× =
  **+25.16%** (bootstrap 95% CI [+19.4, +31.1]%, P(≤20%)=3.2%, B=1000 seed 20260710).
  Not tuned to pass. Root cause: at 5× the shallow queue is perpetually full, so accepted
  requests carry a full-queue transit (+80.6% at p50; gateway queue-wait p95 134 ms vs
  <1 ms uncontended). Sheds: 2067/2067 typed `overloaded` 503 + `Retry-After: 1`
  (raw events + `inference_sheds_total{reason="queue_full"}` + raw-HTTP spot check
  showing 9 accepted = budget 6 + queue 3 from a 20-burst). No starvation
  (single-tenant scope): time-to-shed p99 2.0 ms / max 2.4 ms, 0 deadline-aged sheds,
  max accepted TTFT 233 ms. Admission-off control at 5×: 0 sheds, p95 83 ms — the mock
  serves unbounded concurrency without slowdown, so the off arm demonstrates absence of a
  backpressure signal, not that admission is unnecessary (scope stated in the hypothesis
  file and the report's threats).

Decisions/notes:

- **Elevated analysis gate thresholds, disclosed per point** (E2 baseline 0.15, E2 5×-sane
  0.95): typed shedding is the treatment under test; shed rate is structurally adjacent to
  every goodput figure, and latency tables cover accepted requests only. The default 0.05
  gate stays untouched in code.
- **Model-serving SLO files are measured-basis** per slo.schema.json's normative rule
  (`make_slo_from_events.py` derives thresholds from the runs' own maxima). The
  gateway-overhead p95<10 ms/p99<20 ms target is a gateway-scope program target evaluated
  directly against the cross-arm deltas, not through a goodput ratio (the raw-event schema
  has no per-request overhead signal — it is a cross-arm derived quantity).
- **Paired per-request deltas** (same workload seed ⇒ same item in both arms) are reported
  beside pooled-percentile deltas in `compute_overhead.py` output; the paired basis is
  robust to one-arm window artifacts (exactly what the E1-mock direct rep-2 tail cluster
  produced — pooled Δp99 came out −13.5 ms because of it; anomaly §4.2 of the report).
- **Multi-tenant starvation arm deferred** (task allowance "optional if cheap"): needs
  `-auth-mode=db` (PostgreSQL tenancy + key issuance) — not cheap in this session; the
  no-starvation claim is scoped to intra-tenant behavior and says so everywhere.

### Deviations (IB-T010)

- **First E1-llama.cpp execution discarded wholesale (2026-07-11).** The run completed
  client-side, but the session was interrupted (host reboot) and the gateway process was
  torn down before its `/metrics` TTFT/queue-wait histograms were scraped; the task requires
  reporting BOTH the client-side scheduled-send basis and gateway-side histograms. Rather
  than pair client data with unmatched gateway data, the re-run replaced both arms in full
  (no per-run selection; the first attempt was never analyzed). Discarded log retained:
  `docs/evidence/ib-t010/e1-llamacpp-run.attempt1-discarded.log`. The runner script now
  scrapes before teardown, and its llama-server health wait was raised 60→180 s
  (cold-page-cache model load after reboot exceeded 60 s).
- **`sweep` used probe-only for E2 capacity estimation.** `sweep` refuses <6 points AFTER
  writing `sweep.json` with the probe block; E2 needs exactly two hand-placed points
  (1× and 5×), so the script tolerates the typed points-refusal and asserts the probe
  result exists (`scripts/ib-t010-e2-admission-value.sh`). No generator code changed.
- **Host reboot mid-task** between the E1-mock and E1-llama.cpp/E2 runs. No compared arms
  span the reboot; box-quiet checks logged before each measured run.

### IB-T010 E2b — queue-cap follow-up (2026-07-11)

Prescribed by the fresh-context G5 gate verifier after E2's honest REFUTED verdict (+25.16%
accepted-TTFT p95 degradation at 5×, root-caused to queue-transit: the shallow cap=3 queue was
perpetually full at 5×). Hypothesis file `hypotheses/EXP-ib-t010-e2b-queue-cap.json` written
BEFORE any measured run. Single changed variable: `-admission-tenant-queue-cap` and
`-admission-global-queue-cap` move together, 3→1 (paired and documented as such — the
prescription explicitly names this pairing); `-admission-global-inflight-budget=6` and
`-admission-queue-deadline=500ms` held fixed at E2's values. cap=0 is degenerate (out of
scope by construction); cap=1 is the declared floor. Same infergate pin `6827d8c` (built
read-only via `git archive`, same binaries reused). Report:
`docs/evidence/ib-t010/benchmark-report-1b.md`.

**Design decision — workload reuse for single-variable purity.** E2's own baseline/overload
workload files (`e2-baseline-workload.json` rate 37.8072 rps seed 10010202,
`e2-overload-workload.json` rate 189.0362 rps seed 10010201) are reused **verbatim** rather
than re-derived from a fresh probe against the new cap=1 gateway. A fresh probe was still run
for structural/box-quiet parity and as a cross-check (36.8786 rps achieved — close to E2's
37.8072 rps, consistent with capacity being budget-bound, not queue-cap-bound, at steady
state) but its result was NOT used to re-derive the measured-point rates: holding the offered
rate byte-identical to E2's own points means the ONLY thing differing between E2's and E2b's
sane-arm numbers is the queue-cap value, not a second re-probed rate that could drift for
unrelated reasons.

**Design decision — no repeated admission-off control, no `experiment compare`.** The
variable under test is the queue-cap *value*, not admission on/off, so both measured points
(1× baseline, 5× overload) used `experiment run` (not `compare`) with 2 declared hypothesis
levels (`admission-sane-v1b`, `admission-sane-v1` — the E2 reference config the mechanism
argument is stated against), exactly mirroring how E2 itself ran its own baseline via `run`
rather than `compare`. E2's own admission-off-v1 5× point remains the on-file off-control
reference; nothing about that config changed, so it was not re-run.

**Measured facts (2026-07-11, same host, loopback, 4 vCPU):**

- Baseline 1× (cap=1): n=750/900 accepted, shed rate 16.67% (vs E2's cap=3 baseline 10.11%),
  p95 **0.115553 s** (vs E2's 0.161199 s — 28.3% lower in absolute terms).
- Overload 5× (cap=1): n=563/2550 accepted, shed rate 77.92% (vs E2's cap=3 5× 77.53% —
  barely moved, since at 5× nearly everything sheds regardless of cap depth), p95
  **0.145690 s** (vs E2's 0.201764 s — 27.8% lower in absolute terms).
- **Degradation p95 (5× vs 1×) = +26.08%** (bootstrap 95% CI [+16.30%, +35.17%],
  P(≤20%)=18.4%, B=1000 seed 20260710, method identical to E2's `e2-degradation.json` —
  `compute_degradation_e2b.py` → `e2b-degradation.json`). **> 20% → REFUTED, same as E2, and
  NOT an improvement over E2's +25.16% point estimate.**
- **Root-cause update (the honest finding this follow-up adds):** the queue-cap shrink cut
  absolute p95 by ~28% at BOTH the 1× and 5× points — a roughly proportional effect, not a
  ratio-shaped one — because the queue-cap parameter governs queue-transit depth uniformly
  across load levels, including at the nominal "1×" reference (whose own shed rate rose
  10.11%→16.67% under the shallower cap, i.e. the baseline itself sits further into the
  shedding regime too). A uniform queue-cap shrink therefore cannot fix a *ratio*-shaped G5
  criterion by this mechanism; a genuinely different structural lever (load-adaptive queue
  cap, priority-aware shedding, or a baseline redefinition further below capacity) would be a
  different hypothesis, out of scope for this task.
- Sheds: 2259/2259 raw events typed `overloaded`; gateway counters reconcile exactly
  (4170 total sent = 1579 accepted + 2591 shed, process-cumulative across probe + baseline +
  spot-check + overload traffic on the one long-lived process — same limitation as E2,
  disclosed the same way). Raw-HTTP spot check: 20-burst → 7 accepted (6 in-flight + 1
  queued) + 13 typed 503 + `Retry-After: 1` (E2's same burst against cap=3 gave 9 accepted).
- No starvation (single-tenant scope, same as E2): time-to-shed p99 2.02 ms, max 3.42 ms,
  0/2259 deadline-aged, max accepted TTFT 185.4 ms ≪ 500 ms deadline.

**Stop condition honored:** per the G5-verifier's explicit prescription, E2b failing the
≤20% criterion is the SECOND REFUTED result for the same underlying cause (queue-transit at
this offered-rate regime) — this task does not iterate to a third queue-cap value. Recorded
and stopped; gate review pauses to the user per the roadmap.

**Verification:** `go test -race -count=1 ./...` still green (all 11 packages); 90 pytest /
13 skipped still green (analysis package unchanged); kit validation 9/9 PASS
(`e2b-kit-validate.log`, carries E1's 4 + E2's 3 original results forward alongside E2b's 2
new ones).

**One-line erratum (non-substantive):** `benchmark-report-1.md` §3 prose cites "37.66 rps"
(×3) — a transcription rounding error; the measured value on file is **37.807 rps**
(`e2-capacity-estimate-rps.txt`: 37.80724254139287). The workload files themselves already
used the correct rate; only the prose rounded it wrong. A one-line erratum callout was added
to `benchmark-report-1.md` immediately before §3 rather than rewriting its published numbers
(per the prescription: "correct in the E2b addendum; add a one-line erratum note... rather
than rewriting its published numbers").

Files added: `hypotheses/EXP-ib-t010-e2b-queue-cap.json`,
`scripts/ib-t010-e2b-queue-cap.sh`, `docs/evidence/ib-t010/e2b-facts-sane.json`,
`docs/evidence/ib-t010/compute_degradation_e2b.py`,
`docs/evidence/ib-t010/benchmark-report-1b.md`, per-point reports
`ib-t010-e2b-baseline-1x-sane.report.md` / `ib-t010-e2b-overload-5x-sane.report.md`, result
files under `docs/evidence/ib-t010/results/`, raw-event run directories `e2b-baseline/`,
`e2b-overload/`, `e2b-probe/`, plus logs/scrapes (`e2b-run.log`, `e2b-analyze-*.log`,
`e2b-gateway-*`, `e2b-shed-spotcheck.log`, `e2b-kit-validate.log`,
`e2b-capacity-estimate-rps.txt`).
