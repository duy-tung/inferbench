# Task register — inferbench

Stable IDs IB-T001…IB-T012. Tasks stay narrow, independently reviewable, and evidence-producing;
sub-points may be split off but IDs never change. Critical path:
**IB-T002 → IB-T004 → IB-T005 → IB-T006 → IB-T010**.

Field legend: *Goal* (what/which repo) · *Requirement* (normative content) · *Dependencies* ·
*Expected files* · *Complexity* (S/M/L) · *Critical path* · *Parallel-safe* · *Priority*
(Required/Stretch) · *Review focus* · *Verification* · *Evidence* · *Integration impact* ·
*Stop condition*.

---

## IB-T001 — Planning docs bootstrap

- **Goal:** create the full `docs/` set per the plan, including the methodology doc skeleton
  (`experiments.md` with all methodology rules) — inferbench.
- **Requirement:** every file populated with repo-specific content, not templated boilerplate.
- **Dependencies:** repo prompt; contracts bundle v0.1.x available (see note in
  `interfaces.md` — pin recorded when released).
- **Expected files:** `docs/*`, `docs/adr/`.
- **Complexity:** M. **Critical path:** no. **Parallel-safe:** yes. **Priority:** Required.
- **Review focus:** methodology rules correctly and completely embedded; non-goals explicit.
- **Verification:** checklist against the required doc set; every methodology rule appears in
  `experiments.md`.
- **Evidence:** committed docs. **Integration impact:** none direct; unblocks everything.
- **Stop condition:** all docs exist with content and the plan is reviewed.

## IB-T002 — Open-loop generator core + raw events

- **Goal:** the load-generation engine — inferbench.
- **Requirement:** open-loop + Poisson arrivals, fixed seeds, concurrent SSE streams, JSONL raw
  events per `raw-event.schema.json`; send schedule computed independently of responses;
  schedule-slip watchdog; **no client-side retries ever**.
- **Dependencies:** contracts SC-T002/SC-T003 (API + benchmark schemas released); released
  mock-backend image.
- **Expected files:** `cmd/inferbench/`, `internal/schedule/`, `internal/client/`,
  `internal/events/`, tests.
- **Complexity:** L. **Critical path:** yes. **Parallel-safe:** no. **Priority:** Required.
- **Review focus:** coordinated-omission safety (send-schedule independence) — this review is
  mandatory.
- **Verification:** schema validation of emitted events against pinned fixtures; deterministic
  replay with same seed vs mock (identical schedules); `go test -race ./...` clean.
- **Evidence:** sample run artifacts vs the mock image; CI output.
- **Integration impact:** prerequisite for I2.
- **Stop condition:** CO-safety reviewed; emitted events validate.
- **Status:** implemented 2026-07-10 — `go test -race` clean incl. CO-safety + seed-determinism
  tests; live run vs the pinned gateway+mock pair (infergate @ `a5a2c02`) green; deterministic
  replay verified (same seed → identical schedule + identical mock bodies). Deferred behaviors +
  deviations recorded in `implementation-notes.md` (live streaming vs a *pinned* target waits on
  IG-T003).
  **CO-safety review (fresh-context, 2026-07-10): send-schedule half PASSED; measurement half
  FAILED** — latency clock started at wire-write, leaving dispatch/connect/write delay
  unmonitored and excluded (coordinated omission under a slow-to-accept target). **Fixed same
  day**: latency basis moved to `scheduled_send_ts` (contracts @ `8d81492`, raw-event v0.2.0),
  `send_slip_seconds` emitted, wire-stage watchdog added (typed `schedule_slip` abort), slow-dial
  CO tests added, evidence regenerated + kit-validated against the new pin. See
  `implementation-notes.md` "IB-T002 CO-review fix".

## IB-T003 — Workload suite v1

- **Goal:** the 8 named workloads as versioned seeded files per the workload schema — inferbench.
- **Requirement:** `chat-short`, `rag-long-in`, `gen-long-out`, `shared-prefix` (controlled
  prefix-sharing ratio), `mixed`, `bursty`, `cancel-storm`, `slow-client`; controlled
  input/output-length distributions (output length capped/directed — uncontrolled output length
  is an anti-pattern); cancellation and slow-client profiles encoded.
- **Dependencies:** SC-T003. **Expected files:** `workloads/*` (format per schema), dry-run
  scripts, tests.
- **Complexity:** M. **Critical path:** no. **Parallel-safe:** yes. **Priority:** Required.
- **Review focus:** length distributions controlled; prefix-sharing ratio actually controlled
  (measurable, not incidental).
- **Verification:** all 8 validate against the pinned schema; dry-run vs mock completes.
- **Evidence:** workload files + dry-run logs.
- **Integration impact:** shared with fleetlab arrival models (same versioned files).
- **Stop condition:** 8 workloads run.
- **Status:** implemented 2026-07-10 — canonical suite v1.0.0 authored in `workloads/` (8 files,
  fixed distinct seeds `1003001`–`1003008`, every input/output length distribution
  capped/directed, intent documented per file + `workloads/README.md`); all 8 kit-validate
  against the released contracts bundle **v0.1.0** (re-pinned from `8c58863`). Dry-runs vs the
  pinned gateway+mock pair (infergate @ `a5a2c02`, built read-only via `git archive`):
  `chat-short`/`rag-long-in`/`gen-long-out`/`mixed`/`bursty` complete end-to-end (0 errors, slip
  ≪ watchdog), `shared-prefix`/`cancel-storm`/`slow-client` demonstrate the typed
  `ErrNotImplemented` refusal (execution deferred to IB-T004 — recorded honest behavior; full
  dry-runs when those client features land). Suite rules enforced in code by
  `internal/workload/suite_test.go`; dry-run script `scripts/dryrun-workloads.sh`; evidence
  under `docs/evidence/ib-t003/`.

## IB-T004 — Streaming client correctness

- **Goal:** trustworthy client-side measurement — inferbench.
- **Requirement:** client-side TTFT/ITL capture with monotonic clocks; per-chunk timestamps + max
  stall; cancellation issuance at queued / pre-first-token / mid-stream; slow-client emulation
  (bounded read rate); client series named per the metrics-contract mirror rule (client TTFT ≠
  gateway TTFT — separate named series).
- **Dependencies:** IB-T002. **Expected files:** `internal/client/` extensions, calibration
  harness, tests.
- **Complexity:** M. **Critical path:** yes. **Parallel-safe:** no. **Priority:** Required.
- **Review focus:** measurement-point alignment with the metrics contract.
- **Verification:** run vs mock with known configured latencies — measured ≈ configured within
  declared tolerance.
- **Evidence:** calibration report vs mock. **Integration impact:** I2/I3 measurements depend on
  it.
- **Stop condition:** mock calibration within tolerance.
- **Status:** implemented 2026-07-10 — monotonic-clock audit clean (all latencies are
  `time.Now()`-pair subtractions; per-chunk stamps taken at arrival before parsing);
  cancellation issuance implemented for both schema triggers (elapsed-seconds from the
  scheduled-send plan basis, output-tokens from client-observed content deltas) with honest
  per-event `cancellation_point` + planned-vs-realized accounting; slow-client emulation
  implemented (paced body reads at `read_bytes_per_second` + initial read delay);
  prefix-sharing prompt construction implemented (shared prefix per group, unique suffix) —
  the IB-T003 typed refusals are replaced and the full 8/8 suite dry-runs green (streaming) vs
  the pinned pair (infergate @ `5d69aeb`). CO re-review residual fixed: `send_slip_seconds` is
  ABSENT when the send never completed; `send_ts` fallback documented. **Mock calibration
  PASSED within declared tolerance** (TTFT Δp50 ≤ +3.0 ms, ITL Δp50 ≤ +0.9 ms at two config
  points; 3 cancellation points verified two-sided against the mock's `/debug/state`;
  slow-client bounded-rate effects demonstrated vs control) — see
  `docs/evidence/ib-t004/calibration.md`; all emitted artifacts kit-validate at pin `8d81492`
  (53/53). `go test -race -count=1 ./...` green. Details + contracts observations in
  `implementation-notes.md`.

## IB-T005 — Analysis core (Python)

- **Goal:** the statistics engine — inferbench.
- **Requirement:** pooled-percentile computation (never average percentiles across runs — enforced
  in code, not convention); bootstrap CIs; warm-up exclusion (≥50 requests or 60–120 s);
  saturation-knee detection; goodput@SLO with shed rate adjacent and stall rate computed in the
  same pass; cost per successful request / per 1M tokens using cost-profile files.
- **Dependencies:** IB-T002 outputs (sample raw events). **Expected files:** `analysis/` Python
  package, unit tests with synthetic distributions.
- **Complexity:** L. **Critical path:** yes. **Parallel-safe:** no. **Priority:** Required.
- **Review focus:** statistics choices (bootstrap parameters, knee method, pooling correctness).
- **Verification:** unit tests on synthetic distributions with analytically known answers, all
  green.
- **Evidence:** test suite output. **Integration impact:** results feed fleetlab.
- **Stop condition:** synthetic-data tests green.
- **Status:** implemented 2026-07-10 — `analysis/` Python package (numpy + jsonschema only;
  pandas/scipy justified out). **70 pytest tests green**, all known-answer: exact percentiles on
  constructed data (Hyndman–Fan 7), pooled-vs-averaged guard (data constructed so averaging
  gives the wrong answer + `PercentileTable` construction is structurally locked to raw pooled
  samples), bootstrap CI coverage on Exp(1) (measured 0.913/0.958 for p50/p90 at nominal 95%),
  warm-up exclusion counts per policy, synthetic-knee detection (placed knee found, spike ≠
  knee, edge knee flagged unbracketed), error/shed gating (10% errors → latency withheld;
  100%-timeout run stays valid with goodput/error accounting; deliberate cancels don't trip),
  goodput single-pass with shed+stall structurally adjacent (SLO without a stall objective is
  refused), provenance-aware cost (null + validity note without a profile). ADR-0002 numeric
  parameters finalized (B=1000/95%/seed 20260710; knee 1.5× plateau sustained-departure +
  kneedle cross-check; gate threshold 0.05 declared). End-to-end: 9 real evidence runs from
  `docs/evidence/ib-t002` + `ib-t004` analyzed against a measured-basis SLO
  (`docs/evidence/ib-t005/mock-loopback.slo.json`) — 7 benchmark-result files emitted and
  **kit-valid 7/7** at pin `8d81492`, 2 runs (cancel-queued/pre-first-token) correctly refused
  as valid-but-latency-inexpressible (typed exit 3, no TTFT samples). Evidence:
  `docs/evidence/ib-t005/`. Contract gap (no null form for gated latency tables) recorded as a
  contracts observation in `implementation-notes.md`.

## IB-T006 — Report generator + validity block

- **Goal:** the honest-report machine — inferbench.
- **Requirement:** report template embedding the full manifest, interpretation rules, mandatory
  "threats to validity" + "unexplained anomalies" sections, the one-command reproduction line,
  and the comparability rule; results emitted per `benchmark-result.schema.json`; closed-loop
  results visibly flagged.
- **Dependencies:** IB-T005. **Expected files:** `analysis/report/`, templates, sample report.
- **Complexity:** M. **Critical path:** yes. **Parallel-safe:** no. **Priority:** Required.
- **Review focus:** honesty rules encoded (goodput never rendered without shed + stall rates; no
  mean-only tables).
- **Verification:** end-to-end report from a mock run; result file validates against the pinned
  schema.
- **Evidence:** sample report. **Integration impact:** the evidence format for I3 and everything
  after.
- **Stop condition:** sample report approved — this is gate **G4** together with IB-T009.
- **Status:** implemented 2026-07-11 — `report` subcommand added to the analysis CLI
  (`python3 -m inferbench_analysis report --result FILE | --run DIR ... [--out DIR]`); the
  "template" is deliberately code, not an editable template file: one renderer with a fixed
  section order and typed refusals (`ReportInputError`) for any missing honesty element — no
  validity block, no hypothesis, goodput without shed/stall, blank latency section, or "no
  anomalies" without the checks run. Reports embed the full manifest (hypothesis rendered
  prominently under the title), the comparability rule VERBATIM from the pinned contracts
  (drift vs the bundle's policy file is a typed refusal), interpretation rules, mandatory
  threats-to-validity + unexplained-anomalies sections ("none observed" always ships beside
  the checks that were run), goodput with shed+stall structurally in the same table,
  closed-loop flagging from the workload file (a missing workload file explicitly does NOT
  imply open-loop), `cost: null` always with its reason, the withheld-latency WHY block
  (never a blank table), and the rule-8 one-command reproduction line. Result emission stays
  IB-T005's `analyze` — the report generator only consumes results. **103 pytest tests
  green** (70 IB-T005 + 33 report: refusals, section order, withheld rendering, goodput-shed
  adjacency, anomalies-never-silent, closed-loop flag, verbatim-drift guard, CLI round trip).
  End-to-end evidence in `docs/evidence/ib-t006/`: primary G4 sample
  `ib-t006-calib-A.report.md` (from raw events, bootstrap CIs), `ib-t005-calib-A.report.md`
  (from the emitted result file), `ib-t006-cancel-queued.report.md` (withheld latency, kind
  `no-samples` — the report is the publishable artifact); consumed result files
  re-kit-validated 7/7 at pin `8d81492` (none re-emitted). G4 stays open until IB-T009 joins
  it (per stop condition).

## IB-T007 — Calibration vs reference tooling

- **Goal:** prove the generator's numbers are not an artifact of the generator — inferbench.
- **Requirement:** cross-check against `vllm bench serve` (GPU session, behind G6) or a
  llama.cpp-based reference (CPU fallback); identical workload/target; document deltas and their
  causes.
- **Dependencies:** IB-T004; G6 for the GPU variant. **Expected files:** calibration report under
  `docs/`, comparison scripts.
- **Complexity:** M. **Critical path:** no. **Parallel-safe:** yes. **Priority:** Required.
- **Review focus:** calibration protocol — same measurement points? same warm-up? same arrival
  process? Differences must be enumerated before comparing (see
  `adr/ADR-0004-tool-calibration-protocol.md`).
- **Verification:** comparison table within stated tolerance or every delta explained.
- **Evidence:** calibration report. **Integration impact:** credibility of all published numbers;
  possible upstream findings (`oss-opportunities.md`).
- **Stop condition:** deltas explained.

## IB-T008 — Sweeps, replay, comparison mode

- **Goal:** the experiment mechanics — inferbench.
- **Requirement:** rate sweeps (≥6 points, 10% → 120% of estimated capacity); replay of recorded
  workloads (deterministic); A/B comparison runner that refuses comparisons violating the
  single-variable rule.
- **Dependencies:** IB-T004. **Expected files:** `internal/sweep/`, `internal/replay/`,
  comparison CLI, tests.
- **Complexity:** M. **Critical path:** no. **Parallel-safe:** yes. **Priority:** Required.
- **Review focus:** sweep design (point placement around the estimated knee).
- **Verification:** sweep vs mock produces a detectable knee; replay determinism test green.
- **Evidence:** sweep artifacts. **Integration impact:** saturation/knee inputs for fleetlab;
  I4 gateway-overhead sweeps.
- **Stop condition:** sweep produces a knee on mock.
- **Status:** implemented 2026-07-11 — `internal/sweep` (pure mechanics: rate-point placement,
  single-variable workload derivation, capacity-estimate math), `internal/replay` (schedule
  fingerprint + reference verification), `internal/structdiff` + `internal/manifest.Diff` (the
  shared structural single-variable-rule primitive), and four new `cmd/inferbench` subcommands
  (`sweep`, `replay`, `compare`, plus the shared `runOnce`/manifest-facts refactor of `run`).
  **Capacity-estimate procedure (documented):** a short open-loop overload probe (offered rate
  far above any plausible capacity, bounded request count) measures achieved sustained
  throughput; ADR-0003 sanctions exactly this ("closed-loop... narrowly, for throughput-ceiling
  discovery and sweep-range placement") — implemented as an open-loop overload probe rather
  than closed-loop dispatch (closed-loop execution stays deferred; see Deviations). **Mock
  saturation is client-modeled and documented as such**: the pinned mock/gateway pair has no
  concurrency limiter or admission control of its own (`cmd/mock-backend/main.go`'s flags are
  addr/seed/ttft/itl/error-rate/created/stream-fail-after-chunks only — verified by reading the
  source), so `sweep --max-conns` holds a client-transport `MaxConnsPerHost` cap fixed across
  the probe and every point to model a capacity-limited target; Go's `http.Transport` blocks
  (queues) requests past the cap rather than failing them, so the resulting queueing delay is
  real client-observed latency, counted via the scheduled-send basis (ADR-0001), never
  fabricated — a verification-harness technique, not a production claim. **Replay**: every
  `run`/`replay`/point/arm execution now writes a `reference.json` sidecar
  (`schedule.Fingerprint` over every planned item field); `replay` refuses (typed
  `replay.ErrMismatch`) before sending any request if the rebuilt schedule disagrees with a
  named reference. **Compare**: `--arm id=facts@target` (>= 2, generalizes past A/B), refuses
  (before any traffic) an arm set whose manifests differ in anything beyond the declared
  `--variable` and its schema-implied companions (`manifest.ImpliedFields`, e.g.
  `target_topology` implies the `gateway` block's presence).
  **Verification:** `go test -race -count=1 ./...` green (11 packages, incl. new
  `internal/sweep`, `internal/replay`, `internal/structdiff`, and `cmd/inferbench` regression
  tests). End-to-end vs the pinned mock+gateway pair (infergate @ `74f2372`, built read-only via
  `git archive`; contracts @ **v0.2.0 tag = `484b449`**, re-pinned from `8d81492` — diff is
  release notes only): **sweep produced a bracketed knee** — 6 points, 3 repetitions/point,
  10%→120% of a probe-estimated capacity (27.79 rps); pooled TTFT p99 per point 0.167s / 0.168s
  / 0.214s / 0.341s / 0.659s / 1.189s; `analysis/knee.py` `detect_knee` found
  `arrival_rate_rps=21.12, confidence=0.8, bracketed=true`, kneedle cross-check agrees — **this
  is the stop condition**. Replay determinism verified two ways (schedule-fingerprint match +
  byte-identical response-body SHA-256 sets across 60 requests) plus a negative control (a
  changed seed is refused before any request). A/B compare smoke (direct vs via-gateway,
  single declared variable `target_topology`) plus a negative control (a second undeclared
  variable is refused before any request). 56/56 emitted artifacts (23 manifests + 23 raw-events
  + 10 workloads) kit-valid at the pinned bundle. Evidence: `docs/evidence/ib-t008/`
  (`sweep/`, `knee-result.json`, `knee-detection.log`, `replay-*`, `compare/`, `kit-validate.log`).
  Reproduce: `scripts/sweep-mock.sh docs/evidence/ib-t008 ../infergate 74f2372`.
  **Bug found and fixed during this task** (regression test added,
  `cmd/inferbench/common_test.go`): the original `onceParams.SeedOverride` used an `int64`
  sentinel (`< 0` = no override); every call site outside `cmdRun` forgot to set it, so the Go
  zero value silently overrode every sweep/compare run's seed to 0. Fixed by switching to a
  `*int64` (nil = no override) — a pointer's zero value IS "no override", so the bug class is
  now unreachable by construction.

## IB-T009 — Controlled-experiment framework

- **Goal:** experiment governance in code — inferbench.
- **Requirement:** a hypothesis file is required per experiment (hypothesis, single declared
  variable, expected direction, stop condition, repeat policy); the framework rejects
  hypothesis-less runs and combinatorial/full-matrix sweeps; GPU experiments additionally require
  the session manifest + auto-stop reference (G6 enforcement).
- **Dependencies:** IB-T006. **Expected files:** `internal/experiment/`, hypothesis template,
  governance docs.
- **Complexity:** M. **Critical path:** no. **Parallel-safe:** yes. **Priority:** Required.
- **Review focus:** experiment governance completeness.
- **Verification:** demo showing a hypothesis-less run is rejected and a matrix sweep is rejected.
- **Evidence:** framework docs + dry-run transcript. **Integration impact:** G6 enforcement arm;
  makes IB-T010/T011 auditable.
- **Stop condition:** governance demo done. (With IB-T006, closes gate **G4**.)

## IB-T010 — Experiment set 1 (CPU): gateway overhead + admission value

- **Goal:** the first published benchmark report — inferbench.
- **Hypotheses:** (a) infergate adds ≤ low single-digit ms p95 non-queue overhead vs direct —
  falsify against LiteLLM's self-reported 8 ms p95 as a source-reported baseline (as of 2026-07 —
  re-verify); (b) with admission control ON at ~5× estimated capacity, accepted-request TTFT p95
  degrades ≤20% vs the capacity-boundary baseline while sheds are typed 429/503 + `Retry-After`
  (admission OFF as control).
- **Requirement:** direct-vs-gateway on mock + llama.cpp; admission on/off overload runs; full
  methodology (≥3 runs/point, pooled stats, validity block, one-command repro).
- **Dependencies:** IB-T006; infergate IG-T010 (admission control) released. **Expected files:**
  hypothesis files, run artifacts, benchmark report #1.
- **Complexity:** M. **Critical path:** yes. **Parallel-safe:** no. **Priority:** Required.
- **Review focus:** hypothesis + design BEFORE running; fresh-context report audit against the
  validity checklist AFTER.
- **Verification:** report regenerates from raw events via the one command; G5 evidence criteria
  met.
- **Evidence:** benchmark report #1. **Integration impact:** gate G5; portfolio claim #1.
- **Stop condition:** report published.

## IB-T011 — Experiment set 2 (GPU): vLLM behavior

- **Goal:** controlled vLLM engine-behavior evidence — inferbench.
- **Hypotheses/experiments (each single-variable, hypothesis-first):** `max_num_batched_tokens`
  TTFT/ITL trade-off (reproduce the Sarathi-Serve trade-off with own numbers); `max_num_seqs`;
  `gpu_memory_utilization` including preemption onset; prefix caching on/off with controlled
  prefix-sharing ratio (`shared-prefix` workload); chunked prefill; quantization (AWQ/GPTQ vs
  FP16 where budget allows); KV-cache dtype if feasible. NO full-matrix sweeps. Engine metrics
  collected via the capability mapping.
- **Dependencies:** IB-T009; infergate IG-T014 (vLLM adapter); gate G6 per session (written
  hypothesis + manifest + auto-stop + budget alert). **Expected files:** hypothesis files, session
  manifests, run artifacts, benchmark report #2.
- **Complexity:** L. **Critical path:** no. **Parallel-safe:** no. **Priority:** Required (GPU;
  documented CPU fallback allowed per program rules).
- **Review focus:** GPU session plans reviewed BEFORE spend.
- **Verification:** manifests complete; ≥3 repeats per point; engine metrics present; report
  schema-valid.
- **Evidence:** benchmark report #2 + session logs. **Integration impact:** fleetlab profiles
  (FL-T004); I4 and I6 inputs.
- **Stop condition:** budget cap reached or hypotheses answered.

## IB-T012 — Experiment set 3 (stretch): speculative decoding/MTP, KV offloading, SGLang comparison

- **Goal:** stretch depth only if the baseline is safe — inferbench.
- **Requirement:** same governance as IB-T011; SGLang comparison uses the RadixAttention-informed
  shared-prefix design.
- **Dependencies:** IB-T011 complete + GPU budget remaining + baseline stable. **Expected files:**
  hypothesis files, report addendum.
- **Complexity:** L. **Critical path:** no. **Parallel-safe:** no. **Priority:** Stretch.
- **Review focus:** kill-rule adherence.
- **Verification:** same as IB-T011.
- **Evidence:** report addendum or a documented kill note. **Integration impact:** portfolio depth
  only.
- **Stop condition:** any kill trigger — SGLang is first to drop (killed if >4 h setup without a
  running comparison, GPU budget ≥80% consumed, or the vLLM baseline is unstable; pre-armed
  fallback = vLLM prefix caching on/off, which is already in IB-T011).
