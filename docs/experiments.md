# Experiments and methodology — inferbench

This document is normative. The rules in §1 are the repo's reason to exist; they are encoded in
code, schemas, templates, and review checklists — violating them is program risk **R4**
(benchmark invalidity). Gate **G4** (methodology review) must pass before ANY report is published.

## 1. Methodology rules (normative)

1. **Open-loop Poisson arrivals are mandatory for any latency or goodput claim.** Send times are
   computed from the seed and the arrival process before the run, independent of responses — this
   is the coordinated-omission defense. Closed-loop mode exists only to find a throughput ceiling,
   and every closed-loop result is flagged as such (closed-loop hides queueing delay —
   coordinated omission). See ADR-0001 and ADR-0003.
2. **Warm-up exclusion.** The first ≥50 requests or 60–120 s are excluded from statistics; every
   run declares cache state (cold/warm) in the manifest.
3. **Rate sweeps** cover ≥6 points spanning 10% → 120% of estimated capacity.
4. **Repetition.** ≥3 runs per point; report median ± range across runs.
5. **Pooled percentiles.** Percentiles are computed on pooled raw per-request data across
   repetitions. **NEVER average percentiles across runs.** Enforced in code (IB-T005) and guarded
   by a test that fails if percentile averaging is reintroduced.
6. **Full manifest per run:** GPU model + VRAM, driver/CUDA, instance type, model checkpoint +
   revision + quantization + tokenizer, engine version + commit + all flags, gateway commit +
   config version, client location/RTT, warm-up policy, repetition count, hypothesis. The
   generator refuses to run without it.
7. **Goodput@SLO** requires an explicit, pre-declared SLO; the **shed rate is ALWAYS reported
   adjacent** (goodput can be gamed by shedding early), and **stall rate beside it** (a stream can
   meet the TTFT SLO and still stall mid-generation). Computed in one pass so no goodput figure
   can exist without them.
8. **One-command reproduction** per report: the report names the exact command + pinned versions
   that regenerate it from the raw events.
9. Every report has mandatory **"threats to validity"** and **"unexplained anomalies"** sections.
   An empty anomalies section means "we looked and found none", stated explicitly.
10. **No comparisons across uncontrolled variables.** Results are comparable only when model
    revision, quantization, tokenizer, engine version+flags, hardware, driver/CUDA, workload
    version+seed, and warm-up policy all match, or the difference is the single declared
    experimental variable. No cross-hardware or cross-tool comparisons. Enforced by the
    comparison engine.
11. **No extrapolation past the saturation knee.** Relative claims (A vs B under identical
    conditions) are more durable than absolute numbers; prefer them.
12. **Anti-patterns to detect and refuse:** coordinated omission, uncontrolled output lengths
    (cap or direct them per workload), missing warm-up handling, version drift between compared
    runs, undisclosed hardware, single-run conclusions, mean-only reporting, cherry-picking runs.

## 2. Metrics measured

TTFT; ITL/TPOT (full distribution + max stall + stall rate); end-to-end latency; throughput
(tokens in/out per second); goodput at declared SLO (shed rate adjacent); stream completion rate;
queue delay (gateway-reported, correlated by request ID); shed/retry/error rates; engine cache
info when the capability descriptor exposes it; cost per successful request and per 1M tokens.
Client-side series follow the mirror-naming rule (`observability.md`): client TTFT is never
conflated with gateway TTFT.

## 3. Workload suite (versioned, seeded, per the workload schema)

| Workload | Intent | Key controlled parameters |
|---|---|---|
| `chat-short` | interactive chat baseline | short input/output length distributions |
| `rag-long-in` | prefill-heavy (long context in) | long input, short output |
| `gen-long-out` | decode-heavy (long generation) | short input, long directed output |
| `shared-prefix` | prefix-cache behavior | **controlled prefix-sharing ratio** (measurable, tunable) |
| `mixed` | realistic blend | declared mix proportions of the above |
| `bursty` | queueing/admission behavior | burst amplitude + period over a base Poisson rate |
| `cancel-storm` | cancellation correctness under load | cancellation-rate profile with declared cancel points |
| `slow-client` | backpressure/write-buffer behavior | bounded client read rate profile |

All eight declare: name, version, seed, arrival process, input/output-length distributions
(output length capped/directed — never uncontrolled), duration or request count.

## 4. Validity block (mandatory in every result and report)

Per `benchmark-result.schema.json`, every published result carries a validity block:

- warm-up handling (policy + how many requests/seconds excluded);
- run count per point and pooling statement;
- closed-loop flag if any contributing run was closed-loop;
- client-host health summary (max observed schedule slip, resource headroom);
- **threats to validity** — enumerated, including client-host limits and any environmental
  factors;
- **unexplained anomalies** — enumerated, or the explicit statement "we looked and found none";
- links to raw events and the full manifest.

A result without a complete validity block does not validate against the schema and is not
publishable. Invalid runs are invalidated with a typed reason, never published.

## 5. Experiment governance (enforced by IB-T009)

Every experiment requires a **hypothesis file** before any load is generated. The framework
rejects hypothesis-less runs and combinatorial/full-matrix sweeps. GPU experiments additionally
require the G6 session artifacts: session manifest + auto-stop script reference + budget alert
confirmation.

### Hypothesis file template

```yaml
id:                  # e.g. EXP-mnbt-001
hypothesis: >        # falsifiable statement with expected direction
  Raising max_num_batched_tokens from A to B increases output-token throughput
  and worsens p99 ITL on gen-long-out.
variable: max_num_batched_tokens   # exactly ONE declared variable
levels: [A, B]                     # the levels tested; no cross-products
expected_direction: throughput up, ITL tail worse
workload: gen-long-out@vX (seed S)
constants: >         # everything held fixed (engine version+flags, model+revision,
                     # hardware, workload version+seed, warm-up policy)
stop_condition: >    # when to stop, incl. abort criteria and budget cap for GPU
repeat_policy: 3 runs per point, pooled percentiles
slo_reference:       # required if goodput is claimed
provenance_notes:    # source-reported baselines being falsified, with dates
```

### Review protocol

- Hypothesis + design reviewed **before** running (for GPU: before any spend — G6).
- Report audited **after**, by a fresh-context reviewer, against the checklist in §6.
- Single-variable rule enforced twice: at hypothesis intake and again by the comparison engine.

## 6. G4 methodology review checklist

Used for every report (fresh-context audit). Derived from the rules above plus the Systems
Performance (Gregg) methodology/latency chapters (study-track artifact: this checklist).

- [ ] Arrival process open-loop Poisson, or every closed-loop artifact visibly flagged?
- [ ] Send-schedule independence evidenced (watchdog data present, no schedule slip)?
- [ ] Warm-up excluded per rule 2 and declared, incl. cache state?
- [ ] ≥6 sweep points (where a sweep is claimed), ≥3 runs per point?
- [ ] Percentiles pooled, never averaged? Median ± range across runs reported?
- [ ] Manifest complete (rule 6 fields, all of them)?
- [ ] Goodput shown with SLO reference, shed rate adjacent, stall rate beside?
- [ ] One-command reproduction line present and correct (pinned versions)?
- [ ] Threats-to-validity and unexplained-anomalies sections present and honest?
- [ ] All compared runs controlled per rule 10 (single declared variable)?
- [ ] No claims past the knee; relative claims preferred?
- [ ] No anti-pattern from rule 12 present?
- [ ] Every external number carries provenance (measured / source-reported / assumed) + date?

## 7. Experiment catalog

Hypothesis-driven, single-variable, stop-conditioned. **No full-matrix sweeps.**

### Set 1 — CPU (IB-T010, report #1)

| Experiment | Variable | Hypothesis sketch | Provenance |
|---|---|---|---|
| Gateway overhead | direct vs via-gateway (mock + llama.cpp) | infergate adds ≤ low single-digit ms p95 non-queue overhead; falsify against LiteLLM's self-reported 8 ms p95 (source-reported, as of 2026-07 — re-verify) | source-reported / program SLO (p95 <10 ms, p99 <20 ms) |
| Admission value | admission on vs off at ~5× estimated capacity | with admission ON, accepted-request TTFT p95 degrades ≤20% vs capacity-boundary baseline; sheds typed 429/503 + `Retry-After` | program target (G5) |

### Set 2 — GPU (IB-T011, report #2; each behind G6)

| Experiment | Variable | Note |
|---|---|---|
| Batching trade-off | `max_num_batched_tokens` | reproduce the Sarathi-Serve TTFT/ITL trade-off with own numbers |
| Concurrency limit | `max_num_seqs` | |
| Memory pressure | `gpu_memory_utilization` | include preemption onset |
| Context length | context length | |
| Prefix caching | on/off at controlled prefix-sharing ratios | `shared-prefix` workload; expect effect ∝ ratio, ≈0 at ratio ≈ 0 (assumed, RadixAttention reasoning) |
| Chunked prefill | on/off | |
| Quantization | AWQ/GPTQ vs FP16 | where budget allows |
| KV-cache dtype | dtype | if feasible |

Engine metrics collected via the Contract 4 capability mapping for all Set 2 experiments.

### Set 3 — Stretch (IB-T012; kill rules in `risks.md`)

Speculative decoding/MTP; KV offloading; SGLang comparison (RadixAttention-informed shared-prefix
design). Only if IB-T011 is complete, GPU budget remains, and the baseline is stable.

## 8. Study-track artifacts feeding this doc

| Resource | Artifact | Task |
|---|---|---|
| Sarathi-Serve (OSDI'24) | `max_num_batched_tokens` trade-off hypothesis file | IB-T011 |
| DistServe (OSDI'24) | goodput@SLO definition (encoded in contracts; implemented faithfully here) | IB-T005/T006 |
| Goodput-critique paper (arXiv 2410.14257) | stall-rate-beside-goodput reporting rule in the template | IB-T006 |
| SGLang / RadixAttention (NeurIPS'24) | shared-prefix workload design | IB-T003, IB-T012 |
| Systems Performance (Gregg), methodology + latency chapters | §6 checklist additions used at G4 | G4 |

Rule: artifact-or-drop — a resource with no artifact after two sessions is dropped.
