# Benchmark report #1b — IB-T010 E2b: queue-cap follow-up to E2's G5 verdict

| | |
|---|---|
| date | 2026-07-11 |
| repo | inferbench (this commit) |
| infergate pin | `6827d8c3d177464c17fae3b4dc6c2c475323333b` (same pin as E2; built read-only via `git archive`; gateway + mock-backend binaries) |
| serving-contracts pin | **v0.2.0** = `484b449` (both artifacts kit-validated at this pin — `e2b-kit-validate.log`, 9/9 PASS including the 7 E1/E2 results carried forward) |
| hardware | local dev container, linux/amd64, 4 vCPU, 15 GB RAM, **no GPU**; client, gateway, and engine co-located on this one host (loopback) — same box as E2 |
| governance | hypothesis file `hypotheses/EXP-ib-t010-e2b-queue-cap.json` (IB-T009 framework), written **before** any measured run in this addendum |
| provenance | this is the pre-declared follow-up prescribed by the fresh-context G5 gate verifier after E2's honest REFUTED verdict (`benchmark-report-1.md` §1, §3): +25.16% accepted-TTFT p95 degradation at 5×, root-caused to queue-transit (the shallow `-admission-tenant/global-queue-cap 3` queue was perpetually full at 5× offered rate) |

Every latency figure below is a **client-side series measured from the scheduled send
time** (coordinated-omission-safe basis, ADR-0001) unless explicitly labeled *gateway-side*.
Percentiles are pooled over raw per-request events across 3 repetitions per point — never
averaged across runs.

---

## 1. Verdict (headline)

| Hypothesis | Verdict | Key numbers |
|---|---|---|
| E2b: shrinking the admission queue cap 3→1 (tenant + global, paired; budget=6 and deadline=500ms held fixed) pulls accepted-request TTFT p95 degradation at ~5× under the G5 ≤20% criterion | **REFUTED on the strict ≤20% criterion, again** — degradation **+26.08%** (bootstrap 95% CI [+16.30%, +35.17%], P(≤20%) = 18.4%). Companion criteria **held**: sheds 100% typed `503 overloaded` + `Retry-After` (2259/2259 raw events + gateway counters + raw-HTTP spot check), no starvation within the declared single-tenant scope | See §2 |

**This is the second REFUTED result for the same underlying root cause (queue-transit
dominates accepted-request TTFT at the offered-rate/queue-cap regime this design explores).
Per the G5-verifier's explicit prescription, this task does NOT iterate to a third queue-cap
value — the result is recorded and stops here; gate review pauses to the user.**

The mechanism *partially* worked — both the 1× and 5× points got substantially **faster in
absolute terms** than E2's cap=3 numbers (§2.3) — but the G5 criterion is a *ratio*, and the
mechanism turned out to compress the numerator and the denominator by similar proportions,
leaving the ratio itself roughly unchanged (and, within bootstrap noise, very slightly worse).
§2.3 walks through why.

---

## 2. E2b — Queue cap 3→1 at ~5× offered rate (same offered rate as E2)

### 2.1 Design

**Single changed variable** (per the G5-verifier's prescription): `-admission-tenant-queue-cap`
and `-admission-global-queue-cap` move together, 3 → 1 — the prescription explicitly pairs
these two flags as one logical "queue cap" variable, and that pairing is documented in the
hypothesis file (`hypotheses/EXP-ib-t010-e2b-queue-cap.json`) rather than varied independently.
`-admission-global-inflight-budget 6` and `-admission-queue-deadline 500ms` are held fixed at
E2's values. cap=0 is degenerate (a pure admit-or-shed regime with zero queueing at all,
qualitatively different from "a shallow queue") and out of scope; 1 is the declared floor.

**Workload reuse (single-variable purity).** E2's own workload files are reused **verbatim**,
unchanged: `e2-baseline-workload.json` (seed 10010202, rate 37.8072 rps, 350 req/rep) as the
~1× capacity-boundary reference, and `e2-overload-workload.json` (seed 10010201, rate
189.0362 rps, 900 req/rep) as the ~5× point. A fresh capacity probe was still run against the
new admission-sane-v1b gateway for structural/box-quiet parity with E2 and as a cross-check —
it landed at **36.8786 rps achieved** (`e2b-probe/sweep.json`), close to E2's 37.8072 rps
(−2.5%), consistent with capacity being **budget-bound** (`-admission-global-inflight-budget`,
unchanged at 6) rather than queue-cap-bound at steady state. This probe result is recorded but
**not** used to re-derive the measured-point rates — reusing E2's exact rate keeps the offered
load identical across E2 and E2b, so the *only* thing that differs between the two experiments'
sane-arm numbers is the queue-cap value, not a second re-probed rate that could drift for
unrelated reasons.

**No repeated admission-off control.** The variable under test here is the queue-cap *value*,
not admission on/off; E2's own admission-off-v1 5× point (0% shed, accepted p95 83.2 ms, the
no-queueing floor this hypothesis's mechanism argument references) remains the relevant
off-control reference on file. Nothing about the off-config changed, so it was not re-run.

3 repetitions per point, 50/rep warm-up excluded, `-auth-mode=none` (single implicit tenant,
same scope note as E2). Reproduction: `scripts/ib-t010-e2b-queue-cap.sh`.

### 2.2 Accepted-request TTFT (client-side pooled, seconds)

| point | n accepted (pooled) | shed rate | p50 | p90 | p95 | p99 | max |
|---|---|---|---|---|---|---|---|
| E2b baseline 1× (cap=1) | 750/900 | 0.1667 | 0.082959 | 0.104252 | **0.115553** | 0.140214 | 0.169913 |
| E2b overload 5× (cap=1) | 563/2550 | **0.7792** | 0.095607 | 0.134519 | **0.145690** | 0.164600 | 0.185400 |

- **G5 criterion:** p95 degradation at 5× vs baseline = (0.145690 − 0.115553) / 0.115553 =
  **+26.08%** — bootstrap 95% CI **[+16.30%, +35.17%]**, P(degradation ≤ 20%) = **0.184**
  (B=1000, seed 20260710, method identical to E2's `e2-degradation.json`;
  `compute_degradation_e2b.py` → `e2b-degradation.json`). **> 20% → REFUTED as configured,
  same as E2.**
- **Sheds typed (held):** all 2259/2259 measured raw events across both points (baseline +
  overload, all reps, pre-warm-up-exclusion count) carry `error_class="overloaded"`; gateway
  counter `inference_sheds_total{reason="queue_full"}` = 2591 on this process (cumulative
  across the capacity probe (400 sent) + baseline (1050 sent) + spot-check (20 sent) +
  overload (2700 sent) = 4170 total sent = 1579 accepted + 2591 shed — reconciles exactly;
  `e2b-gateway-metrics.txt`). Raw-HTTP spot check (`e2b-shed-spotcheck.log`): a 20-request
  concurrent burst against budget=6/queue-cap=1 yielded **7 accepted** (6 in-flight + 1
  queued) and **13 typed `HTTP/1.1 503 Service Unavailable` + `Retry-After: 1`** — matching
  the shallower cap exactly (E2's same burst against cap=3 yielded 9 accepted).
- **No starvation (single-tenant scope, held):** time-to-shed p50 1.09 ms, p99 2.02 ms, max
  3.42 ms across all 2259 shed events; 0 of 2259 aged past the 500 ms deadline (checked at a
  conservative 400 ms threshold, same as E2); max accepted TTFT 185.4 ms ≪ 500 ms deadline —
  nothing was dispatched stale. Scope is identical to E2: intra-tenant no-starvation under
  `-auth-mode=none` (one implicit tenant), not cross-tenant fairness (IG-T011, still deferred).

### 2.3 Why the mechanism didn't move the ratio — the honest root-cause update

The queue-transit-reduction mechanism **did** work in absolute terms, at **both** points:

| | E2 (cap=3) | E2b (cap=1) | Δ |
|---|---|---|---|
| baseline (1×) p95 | 0.161199 s | 0.115553 s | **−28.3%** |
| overload (5×) p95 | 0.201764 s | 0.145690 s | **−27.8%** |
| baseline shed rate | 10.11% | 16.67% | +6.6 pp |
| overload shed rate | 77.53% | 77.92% | +0.4 pp |
| **p95 degradation (5× vs 1×)** | **+25.16%** | **+26.08%** | **+0.9 pp (worse, within CI overlap)** |

Both the numerator (5×) and the denominator (1×) of the G5 ratio dropped by essentially the
**same proportion** (~28%) when the cap shrank. That is the mechanism working exactly as
predicted at the level of absolute latency — but a ratio-based criterion is insensitive to a
proportional shift applied to both its numerator and its denominator. The queue-cap parameter
governs queue-transit depth **uniformly across load levels**, including at the nominal "1×"
reference point, which is why the baseline's own shed rate rose materially too (10.11% →
16.67%): at cap=1, even offered load *at* the estimated capacity now sheds a meaningfully
larger fraction just from ordinary Poisson burstiness hitting a one-slot queue. The 1× point is
therefore *less* of a clean "uncontended" reference under cap=1 than it was under cap=3 — both
arms sit further into the shedding regime, which is consistent with why the ratio didn't
improve even though every absolute number did.

**Implication for further iteration (not pursued here, per the prescription):** a uniform
queue-cap shrink cannot, by this mechanism, fix a *ratio*-shaped G5 criterion, because it acts
symmetrically on the reference point and the overload point. A genuinely different structural
lever (e.g., a queue-cap that scales with a load estimate rather than a fixed absolute value,
priority/latency-aware shedding instead of FIFO-depth shedding, or redefining the G5 baseline
to a load level materially below the probe-estimated capacity so it isn't itself already
queue-contended) would be a *different* hypothesis, not a third value of the same variable —
out of scope for this task per the "do not iterate further" instruction.

### 2.4 Gateway-side cross-check (process-cumulative, coarse — not the primary basis)

`e2b-gateway-side-percentiles.json` (scraped once, before teardown, diffed against an all-zero
snapshot): `inference_ttft_seconds` p50 ≈ 83 ms / p95 ≈ 180 ms, `inference_queue_wait_seconds`
p50 ≈ 0.9 ms / p95 ≈ 58 ms. This single scrape is **cumulative over both the baseline and the
overload point** (one long-lived gateway process, same limitation as E2's own gateway-side
scrapes) — it is not decomposable into a per-point figure and is reported only as a coarse,
bucket-quantized cross-check that the queue-wait signal is present and roughly the expected
order of magnitude, never as the primary basis (per experiments.md rule; identical caveat to
E2 §4.1 item 6).

---

## 3. Threats to validity (this addendum)

1. **Single-host co-location** — same as E2 §4.1 item 1; box-quiet checks logged before the
   measured run (`e2b-run.log`: "no matching processes", load average 0.27/0.26/0.34).
2. **Mock backend cannot degrade organically** — same as E2 §4.1 item 2; admission control
   remains the only capacity mechanism in play.
3. **The 1× reference point is not fully comparable across E2 and E2b in queueing-regime
   terms**, even though the offered *rate* is identical: at cap=1 the baseline itself sheds
   16.67% (vs 10.11% at cap=3), i.e. the "1×" reference sits further into the shedding regime
   under the shallower cap. This is exactly the §2.3 finding, not a hidden defect — but it
   means "same offered rate" is not the same as "same queueing regime" once the cap changes,
   and any future work should not treat E2b's baseline as a clean uncontended floor.
4. **Elevated analysis gate thresholds, disclosed:** baseline 0.20 (vs E2's 0.15 — the shallower
   cap sheds more even at 1×), overload 5× 0.95 (same as E2). Typed shedding is the treatment;
   shed rate is reported adjacent to every latency table; latency tables cover accepted
   requests only.
5. **Gateway-side histogram is process-cumulative across both points** (§2.4) — cross-check
   only, never the primary basis.
6. **No rate sweep** — `knee_estimate: null` in both results; no saturation claim beyond the
   probe's declared cross-check purpose.
7. **Bootstrap CI is wider than E2's** ([16.30%, 35.17%], width 18.9 pp, vs E2's [19.38%,
   31.09%], width 11.7 pp) because the absolute p95 values are smaller here (a fixed-size
   sampling fluctuation in seconds is a larger fraction of a smaller p95); P(≤20%) = 18.4% is
   accordingly less confidently REFUTED than E2's 3.2%, but the point estimate (+26.08%) is
   not an improvement over E2's (+25.16%) — both the point estimate and the mechanism analysis
   in §2.3 support REFUTED, not just the p-value.

No unexplained anomalies: both runs 0 errors, all sheds typed, replay fingerprints written for
every rep.

---

## 4. Artifacts, reproduction, and per-point reports

| point | result file | report |
|---|---|---|
| E2b baseline 1× (cap=1) | `results/ib-t010-e2b-baseline-1x-sane.benchmark-result.json` | `ib-t010-e2b-baseline-1x-sane.report.md` |
| E2b overload 5× (cap=1) | `results/ib-t010-e2b-overload-5x-sane.benchmark-result.json` | `ib-t010-e2b-overload-5x-sane.report.md` |

Cross-point computation: `compute_degradation_e2b.py` → `e2b-degradation.json` (bootstrap,
identical method to E2's `e2-degradation.json`); gateway-side scrape
`e2b-gateway-side-percentiles.json`, `e2b-gateway-metrics.txt`; spot check
`e2b-shed-spotcheck.log`; full run transcript `e2b-run.log`; analyze transcripts
`e2b-analyze-baseline.log`, `e2b-analyze-overload.log`; kit validation `e2b-kit-validate.log`
(9/9 PASS, includes E1/E2's 7 results carried forward).

**Reproduce the runs** (pins above; box must be otherwise idle). Build the pinned binaries
first — build byproducts are not committed (same pin as E2, reuse the same build if still on
disk):

```sh
git -C ../infergate archive 6827d8c3d177464c17fae3b4dc6c2c475323333b | tar -x -C /tmp/infergate-src
(cd /tmp/infergate-src && go build -o docs/evidence/ib-t010/gateway-bin ./cmd/gateway \
                       && go build -o docs/evidence/ib-t010/mock-backend-bin ./cmd/mock-backend)
go build -o docs/evidence/ib-t010/inferbench-bin ./cmd/inferbench

scripts/ib-t010-e2b-queue-cap.sh
```

**Regenerate any per-point report from its result file:**

```sh
python3 -m inferbench_analysis report --bundle /path/to/serving-contracts \
  --result docs/evidence/ib-t010/results/<result-id>.benchmark-result.json --root . \
  --out docs/evidence/ib-t010
```

**Recompute the bootstrap degradation stat:**

```sh
python3 docs/evidence/ib-t010/compute_degradation_e2b.py
```

**Provenance of every number:** program SLO target (G5 ≤20% degradation) —
program-declared target (docs/tasks.md IB-T010, 05-execution-roadmap G5), same target as E2;
everything else in this addendum — measured in this addendum's linked runs, 2026-07-11.

---

## Program decision on gate G5 (recorded 2026-07-11, post-review)

After E2 (+25.16%) and E2b (+26.08%) both REFUTED the ≤20% accepted-TTFT-degradation
target, and a fresh-context gate verifier confirmed every number and the mechanism, the
program owner re-baselined gate **G5 to PASS** under assumption A9 (source-derived targets
may be re-baselined after first measurement if infeasible-in-principle). Rationale, on the
evidence in this report and its companion:

- The ≤20% **ratio** criterion is mis-shaped for an admission-by-queueing gateway: admitted
  requests carry queue transit that scales with the depth parameter **uniformly across load
  levels** (E2b demonstrated the queue-cap knob cuts absolute latency at the 1× reference
  point too), so no single-knob change can make the 5×-vs-1× ratio shrink by this mechanism.
- The **re-framed G5 criterion is MET**: at ~5× capacity, load shedding is 100% typed
  (`503 overloaded` + `Retry-After`, verified at three layers), accepted-request queue-wait is
  **bounded** (gateway-side `inference_queue_wait_seconds` p95 = 134 ms), and there is **no
  starvation** (single-tenant here; multi-tenant fairness p95 shift 0.0–4.6% < 15% at IG-T011).

This negative result and its mechanism analysis are retained, in full, as the published
finding — not superseded or hidden. The ≤20% ratio figure remains recorded as the original
source-derived target that measurement showed to be architecture-inappropriate.
