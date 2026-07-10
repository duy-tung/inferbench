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
