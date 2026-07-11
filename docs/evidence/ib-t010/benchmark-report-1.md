# Benchmark report #1 — IB-T010, Experiment set 1 (CPU): gateway overhead + admission value

| | |
|---|---|
| date | 2026-07-11 |
| repo | inferbench (this commit) |
| infergate pin | `6827d8c3d177464c17fae3b4dc6c2c475323333b` (built read-only via `git archive`; gateway + mock-backend binaries) |
| serving-contracts pin | **v0.2.0** = `484b449` (all artifacts kit-validated at this pin — `kit-validate.log`, 60/60 PASS) |
| llama.cpp pin | `8f114a9b573b69035299f9b924047f53c1e22c7e` (`llama-server`, "version: 1 (8f114a9)", GCC 13.3.0, linux x86_64) |
| model | `qwen2.5-1.5b-instruct-q4_k_m.gguf`, sha256 `6a1a2eb6d15622bf3c96857206351ba97e1af16c30d7a74ee38970e434e9407e` (local GGUF file; no upstream registry revision) |
| hardware | local dev container, linux/amd64, 4 vCPU, 15 GB RAM, **no GPU**; client, gateway, and engine co-located on this one host (loopback) |
| governance | hypothesis files `hypotheses/EXP-ib-t010-e1-gateway-overhead.json`, `hypotheses/EXP-ib-t010-e2-admission-value.json` (IB-T009 framework; every load-generating invocation below ran through `inferbench experiment …`, which refuses hypothesis-less or multi-variable runs before any request is sent) |

This is the roll-up over six schema-valid per-point benchmark results (each with its own
generated report, full embedded manifests, validity block, and one-command reproduction line —
links in §5). Every latency figure below is a **client-side series measured from the scheduled
send time** (coordinated-omission-safe basis, ADR-0001) unless explicitly labeled
*gateway-side*. Percentiles are pooled over raw per-request events across ≥3 repetitions per
point — never averaged across runs.

---

## 1. Verdicts (headline)

| # | Hypothesis | Verdict | Key numbers |
|---|---|---|---|
| E1-mock | infergate adds non-queue overhead p95 <10 ms / p99 <20 ms vs direct (program SLO target) | **CONFIRMED** (mock arm) | paired per-request overhead p50 **+1.04 ms**, p95 **+2.21 ms**, p99 **+2.81 ms**, max +28.8 ms (n=630 pairs); pooled Δp95 **+1.15 ms**; pooled Δp99 −13.5 ms (negative — direct-arm tail anomaly, §4.2); gateway-side queue wait p95 **<1 ms** |
| E1-llama.cpp | same hypothesis against a real engine | **INCONCLUSIVE at the ms scale** | run-to-run engine variance (per-rep direct p95: 3.70 s → 2.32 s → 1.74 s) is 2–3 orders of magnitude larger than the 10 ms bound under test; paired median delta **−8.5 ms** (gateway arm *faster*, 57.7% of pairs negative — order/warming effect, §4.3). Data is *consistent with* no meaningful added overhead but cannot resolve the claim |
| E2 (G5) | admission ON at ~5× capacity keeps accepted-request TTFT p95 degradation ≤20% vs the ~1× baseline; sheds typed 429/503 + Retry-After; no starvation | **REFUTED on the strict ≤20% criterion** — degradation **+25.16%** (bootstrap 95% CI [+19.4%, +31.1%]; P(≤20%) = 3.2%). The two companion criteria **held**: sheds 100% typed `503 overloaded` + `Retry-After` (2067/2067 raw events + gateway counters + raw-HTTP spot check), and no starvation within the declared single-tenant scope |

The E2 refutation is reported as measured. We did not re-tune the admission configuration to
pass; the sane-arm config was declared in the hypothesis file before any load ran (§3.1).

---

## 2. E1 — Gateway overhead (direct vs via-gateway)

**Design.** Single declared variable `target_topology` (structurally enforced; the `gateway`
manifest block is its schema-implied companion). Same workload bytes, same seed, both arms, 3
repetitions each, warm-up excluded per manifest (`discard-requests`). Offered rate held far
below capacity so queueing is negligible and the delta approximates **non-queue** overhead.
Because both arms share the workload seed, workload item *i* is the same prompt at the same
schedule offset in both arms — enabling **paired per-request deltas**, the strongest available
basis for a "gateway adds ≤ X ms" claim.

### 2.1 Mock arm (`e1-mock-compare/`, mock TTFT=300 ms, ITL=15 ms, seed 42)

Mock TTFT was set to 300 ms per the IG-T011/D3 lesson (at tiny mock latencies, ±TTFT noise
swamps a millisecond-scale signal; at 300 ms the queueing fractions and the signal/noise ratio
are realistic). 260 requests/rep at 6 rps, 50/rep warm-up excluded → 630 pooled events/arm,
0 errors, 0 shed.

Client-side pooled TTFT (seconds):

| arm | n | p50 | p90 | p95 | p99 | max |
|---|---|---|---|---|---|---|
| direct | 630 | 0.301967 | 0.302630 | 0.302790 | 0.317944 | 0.364772 |
| gateway | 630 | 0.302965 | 0.303698 | 0.303939 | 0.304465 | 0.330679 |
| **pooled Δ (gw − direct)** | | **+1.00 ms** | +1.07 ms | **+1.15 ms** | **−13.5 ms** (§4.2) | |

Paired per-request delta (gw − direct, same item, same repetition; n=630 pairs):
**p50 +1.04 ms, p90 +1.96 ms, p95 +2.21 ms, p99 +2.81 ms, max +28.8 ms**; 10.3% of pairs
negative (run noise). Per-rep p95 range: direct 302.7–303.1 ms, gateway 303.8–304.0 ms
(median ± range across runs is a fraction of a millisecond — the effect is stable).

Gateway-side (Contract 2 mirror series, scraped from `/metrics` before teardown;
`e1-mock-gateway-side-percentiles.json`): `inference_queue_wait_seconds` p50 ≈ 0.5 ms,
**p95 ≈ 0.95 ms**, p99 ≈ 0.99 ms (n=780 including warm-up) — the admission dispatcher adds
sub-millisecond wait at this load, consistent with the client-observed ~1–2 ms total added
latency (remainder: extra TCP hop + proxy relay). `inference_ttft_seconds` p50 ≈ 0.30 s
agrees with the configured mock TTFT; its higher percentiles are not resolvable at this
histogram's bucket width (0.2–0.4 s bucket) and are reported only as a cross-check, never as
the primary basis.

**Verdict vs targets** (client-side paired basis AND pooled-delta basis): p95 +2.21 ms /
+1.15 ms < 10 ms; p99 +2.81 ms / −13.5 ms < 20 ms → **CONFIRMED** on both bases.
Reference baseline: LiteLLM self-reports "8 ms P95 latency at 1k RPS" (4-instance
configuration; overhead-duration table median 2 / p95 8 / p99 13 ms, self-timed via its
`x-litellm-overhead-duration-ms` header) — **source-reported, re-verified 2026-07-11** at
docs.litellm.ai/docs/benchmarks. Per the comparability rule (different hardware, load level,
and measurement basis — theirs is gateway-self-timed, ours is client-observed), this figure
frames the magnitude of the claim being tested; **no cross-tool superiority claim is made or
supported by this report.**

### 2.2 llama.cpp arm (`e1-llamacpp-compare/`, real engine, CPU)

45 requests/rep at 0.4 rps (single slot `-np 1 -c 4096 -t 4`), 8/rep warm-up excluded → 111
pooled events/arm, 0 errors. Same paired design.

| arm | n | p50 | p90 | p95 | p99 | max (s) |
|---|---|---|---|---|---|---|
| direct | 111 | 0.5208 | 2.3445 | 2.8461 | 3.9980 | 4.5010 |
| gateway | 111 | 0.4042 | 1.8437 | 1.9803 | 2.7094 | 2.9419 |

The gateway arm measures *lower* at every percentile — physically impossible as a gateway
effect and diagnostic of the real issue: llama.cpp CPU prompt-processing time dominates TTFT
(hundreds of ms to seconds, prompt-length- and cache-state-dependent) and drifts across the
run (direct per-rep p95 3.70 → 2.32 → 1.74 s while gateway per-rep p95 is stable at
1.93–2.09 s; the arms ran sequentially against one server instance, so the later gateway arm
benefited from warming). Paired median delta −8.5 ms, paired p95 +0.45 s, 57.7% of pairs
negative. **INCONCLUSIVE at the ≤10 ms scale**: the instrument noise floor here is ~2–3
orders of magnitude above the effect size measured on the mock. The honest positive statement
this arm supports: routing llama.cpp traffic through infergate produced **no detectable added
latency at the engine's own variance scale** — and the mock arm (deterministic engine, same
gateway binary and code path) is the resolving instrument for the ms-scale claim.
Gateway-side cross-check: queue wait p95 <1 ms here too (`e1-llamacpp-gateway-side-percentiles.json`).

---

## 3. E2 — Admission value at ~5× capacity (G5)

**Design.** Mock backend (TTFT=80 ms, ITL=10 ms, seed 43). Two gateway processes, single
declared variable `gateway.config_version`:

- **admission-sane-v1** (declared latency-protective *before* running, in the hypothesis
  file): `-admission-tenant-queue-cap 3 -admission-global-inflight-budget 6
  -admission-global-queue-cap 3 -admission-queue-deadline 500ms` — shallow bounded queue
  (a fraction of the in-flight budget) + short deadline backstop: shed early instead of
  queueing deep.
- **admission-off-v1** (control): all caps at 10⁶, deadline 300 s — admission effectively off.

Capacity estimated once by the ADR-0003-sanctioned open-loop overload probe against the sane
gateway (offered 200 rps, 400 requests): **37.66 rps achieved** (`e2-probe/sweep.json`).
Points: baseline ~1× (37.66 rps, 350 req/rep ×3, sane config only) and ~5× (188.3 rps,
900 req/rep ×3, both configs). 50/rep warm-up excluded everywhere; `-auth-mode=none`
(single implicit tenant — scope note below).

### 3.1 Accepted-request TTFT (client-side pooled, seconds)

| point | n accepted (pooled) | shed rate | p50 | p95 | p99 | max |
|---|---|---|---|---|---|---|
| baseline 1× (sane) | 809/900 | 0.101 | 0.0836 | **0.1612** | 0.1961 | 0.2188 |
| overload 5× (sane) | 573/2550 | **0.775** | 0.1510 | **0.2018** | 0.2193 | 0.2331 |
| overload 5× (off) | 2550/2550 | 0.000 | 0.0822 | 0.0832 | 0.0841 | 0.1013 |

- **G5 criterion:** p95 degradation at 5× vs baseline = (0.2018 − 0.1612)/0.1612 =
  **+25.16%** — bootstrap 95% CI [+19.38%, +31.09%], P(degradation ≤ 20%) = 0.032
  (B=1000, seed 20260710; `e2-degradation.json`). **> 20% → hypothesis (b) REFUTED as
  configured.** Root cause is structural and visible in the p50: at 5× the shallow queue
  (cap 3) is *perpetually* full, so nearly every accepted request carries one full-queue
  transit (~3 service slots ≈ +67 ms at p50, +80.6%), whereas at 1× the queue is only
  intermittently occupied. Gateway-side `inference_queue_wait_seconds` confirms: p50 33 ms /
  p95 134 ms at 5× vs <1 ms uncontended. An even shallower queue (cap 0–1) would trade more
  shed for less accepted-latency degradation; testing that is future work, **not** something
  this report retrofits.
- **Absolute protection story (what admission DID buy):** accepted p95 stayed within 202 ms
  (vs an 80 ms no-load floor), every accepted request finished, goodput at 5× was 37.9
  ok-req/s ≈ the measured capacity, max accepted TTFT 233 ms, zero errors, and the process
  stayed healthy throughout.
- **Sheds typed (held):** all 2067 measured-window sheds in the sane 5× arm are
  `error_class="overloaded"` in the raw events (HTTP 503); gateway counters:
  `inference_sheds_total{reason="queue_full"}` accounts for every shed
  (`e2-gateway-metrics-sane.txt`); raw-HTTP spot check shows literal
  `HTTP/1.1 503 Service Unavailable` + `Retry-After: 1` (`e2-shed-spotcheck.log` — a
  20-burst yielded exactly 9 accepted = 6 in-flight + 3 queued, 11 typed sheds). The
  contract permits 429 for rate-limit sheds; admission sheds are 503 by design (429 is
  IG-T009 quota territory, not exercised here).
- **No starvation (single-tenant scope):** time-to-shed p99 = 2.0 ms, max 2.4 ms — every
  rejected request was answered immediately, none aged out at the 500 ms deadline (0 of
  2067 past 0.4 s); max accepted TTFT 233 ms ≪ deadline — nothing was dispatched stale.
  **Scope recorded honestly:** this is intra-tenant no-starvation under `-auth-mode=none`
  (one implicit tenant). Cross-tenant fairness/aging (IG-T011) requires DB-backed tenancy
  and was not exercised — a multi-tenant arm was assessed as not cheap here (PostgreSQL
  control plane + key issuance) and is deferred, per the task's explicit scope allowance.
- **Admission-off control:** zero sheds and *better* raw latency than the sane arm at 5× —
  exactly as the hypothesis file predicted, and the honest scope finding of this experiment:
  the deterministic mock serves unbounded concurrency without slowdown, so admission-off
  shows no organic collapse against it. The off arm demonstrates the *absence of a
  backpressure signal*, not that admission is unnecessary; the value of admission at 5×
  against a real, capacity-limited engine is bounded-latency + typed sheds versus unbounded
  queue growth — a claim that needs a saturable engine and is **not** made from this data
  (see threats, §4.1).

---

## 4. Threats to validity and anomalies

### 4.1 Threats to validity (whole report)

1. **Single-host co-location.** Client (inferbench), gateway, and engine (mock or
   llama-server) all share one 4-vCPU container over loopback. Client scheduling delay and
   engine CPU contention land in the same measurements; the E1-mock direct-arm tail cluster
   (§4.2) is the visible instance. Box-quiet checks (no stray processes, load average) were
   run and logged before each measured run (`e1-llamacpp-run.log`, `e2-run.log`,
   session log for the mock arm), and `send_slip_seconds` is recorded per event; max
   dispatch slip stayed in single-digit ms except inside the deliberate overload points.
2. **Mock backend cannot degrade organically** (verified at source at the pin: no
   concurrency limiter, constant configured latencies). In E2, admission control is the
   *only* capacity mechanism in play; the off arm therefore cannot show overload collapse.
   The E2 comparison isolates admission mechanics — it does not simulate engine saturation.
3. **Capacity-estimate bias.** The probe's achieved-rate method (ok/elapsed at 5.3×
   overload offered) yielded 37.66 rps; the ~10% typed shed rate at the "1×" baseline shows
   this slightly exceeds the steady admission-bounded capacity (edge effects of a short
   burst). Both the baseline and 5× points derive from the *same* estimate, so the 5×/1×
   ratio design is internally consistent; the "1×" point is honestly a capacity-*boundary*
   point, as the G5 criterion intends.
4. **Sequential arms in E1-llama.cpp** against a single server instance (order/warming
   effect) — this is what makes that sub-experiment inconclusive at ms scale (§2.2).
5. **Elevated analysis gate thresholds, disclosed:** the E2 baseline (0.15) and sane-5×
   (0.95) points raise the error+shed latency-gate threshold above the 0.05 default because
   typed shedding *is the treatment*; the shed rate is reported adjacent everywhere, and the
   latency tables cover accepted requests only (shed requests produce no first byte).
6. **Gateway-side histograms are bucket-quantized** (Prometheus buckets per the metrics
   vocabulary); they are cross-checks, not the primary basis. Client-side pooled/paired
   percentiles from raw events are the published basis.
7. **A host reboot occurred between the E1-mock and E1-llama.cpp/E2 runs** (session
   interruption). No compared arms span the reboot: each sub-experiment's arms ran within
   one boot, interleaved-free, against identically pinned binaries.
8. **No rate sweep in this report** — `knee_estimate: null` in every result; no saturation
   claim is made beyond the E2 capacity probe's declared purpose (point placement).

### 4.2 Unexplained anomalies (E1-mock)

- **Direct-arm rep-2 tail cluster:** items 207–212 (a ~0.5 s window at 11:06:50–51Z) show
  correlated TTFT elevation up to +65 ms with elevated send slip (up to 56 ms) on the same
  events; rep-3 has zero such events. Consistent with a transient host scheduling stall
  (co-location, threat #1) hitting the direct arm's window; not attributable to either
  topology. Consequence: pooled direct p99 (0.3179 s) exceeds pooled gateway p99
  (0.3045 s), making the pooled Δp99 negative (−13.5 ms). The paired-delta p99 (+2.81 ms),
  which is robust to a one-arm window artifact affecting both tails equally only if paired,
  is the more faithful tail-overhead statistic. We looked for a within-gateway or
  within-mock cause (logs, error counters, GC pauses would show as slip on both arms) and
  found none — recorded as unexplained at the host level.
- No other anomalies: all runs 0 errors (E1) / 0 non-shed errors (E2), 0 retries anywhere
  (the client never retries by design), replay fingerprints written for every run.

### 4.3 Deviation note

The first E1-llama.cpp execution (2026-07-11 ~11:21Z) completed but was **discarded
wholesale, unanalyzed**, because its gateway process was torn down before the gateway-side
histogram was scraped (the task requires both bases). The re-run replaced both arms in
full; no per-run selection occurred. The discarded log is retained at
`e1-llamacpp-run.attempt1-discarded.log`.

---

## 5. Artifacts, reproduction, and per-point reports

Six schema-valid benchmark-result files (kit 60/60 PASS at pin v0.2.0, `kit-validate.log`),
each with a generated report embedding full manifests, interpretation rules, validity block,
and its own one-command reproduction line:

| point | result file | report |
|---|---|---|
| E1 mock direct | `results/ib-t010-e1-mock-direct.benchmark-result.json` | `ib-t010-e1-mock-direct.report.md` |
| E1 mock gateway | `results/ib-t010-e1-mock-gateway.benchmark-result.json` | `ib-t010-e1-mock-gateway.report.md` |
| E1 llama.cpp direct | `results/ib-t010-e1-llamacpp-direct.benchmark-result.json` | `ib-t010-e1-llamacpp-direct.report.md` |
| E1 llama.cpp gateway | `results/ib-t010-e1-llamacpp-gateway.benchmark-result.json` | `ib-t010-e1-llamacpp-gateway.report.md` |
| E2 baseline 1× sane | `results/ib-t010-e2-baseline-1x-sane.benchmark-result.json` | `ib-t010-e2-baseline-1x-sane.report.md` |
| E2 5× sane | `results/ib-t010-e2-overload-5x-sane.benchmark-result.json` | `ib-t010-e2-overload-5x-sane.report.md` |
| E2 5× off | `results/ib-t010-e2-overload-5x-off.benchmark-result.json` | `ib-t010-e2-overload-5x-off.report.md` |

Cross-arm computations: `e1-mock-overhead.json`, `e1-llamacpp-overhead.json`
(`compute_overhead.py` — pooled deltas + paired per-request deltas), `e2-summary-points.json`,
`e2-degradation.json` (bootstrap), gateway-side scrapes `e1-mock-gateway-side-percentiles.json`,
`e1-llamacpp-gateway-side-percentiles.json`, `e2-gateway-side-percentiles-{sane,off}.json`,
`e2-gateway-metrics-{sane,off}.txt`, spot checks `e2-shed-spotcheck{,-off}.log`.

**Reproduce the runs** (pins above; box must be otherwise idle). Build the pinned binaries
first — build byproducts are not committed:

```sh
git -C ../infergate archive 6827d8c3d177464c17fae3b4dc6c2c475323333b | tar -x -C /tmp/infergate-src
(cd /tmp/infergate-src && go build -o docs/evidence/ib-t010/gateway-bin ./cmd/gateway \
                       && go build -o docs/evidence/ib-t010/mock-backend-bin ./cmd/mock-backend)
go build -o docs/evidence/ib-t010/inferbench-bin ./cmd/inferbench

# E1 mock arm (mock: -addr :19281 -seed 42 -ttft 300ms -itl 15ms; gateway: -addr :19280 \
#   -backend-url http://127.0.0.1:19281 -upstream-timeout 60s -stream-write-timeout 30s), then:
docs/evidence/ib-t010/inferbench-bin experiment compare \
  --hypothesis hypotheses/EXP-ib-t010-e1-gateway-overhead.json \
  --workload docs/evidence/ib-t010/e1-mock-workload.json --variable target_topology \
  --arm direct=docs/evidence/ib-t010/e1-mock-facts-direct.json@http://127.0.0.1:19281 \
  --arm gateway=docs/evidence/ib-t010/e1-mock-facts-gateway.json@http://127.0.0.1:19280 \
  --out docs/evidence/ib-t010/e1-mock-compare --stream --repetitions 3 --max-slip 200ms --request-timeout 30s
# E1 llama.cpp arm:
scripts/ib-t010-e1-llamacpp.sh
# E2 (probe + baseline + 5x on/off + spot checks + scrapes):
scripts/ib-t010-e2-admission-value.sh
```

**Regenerate any per-point report from its result file** (rule-8 one command; each report
also carries its own line):

```sh
python3 -m inferbench_analysis report --bundle /path/to/serving-contracts \
  --result docs/evidence/ib-t010/results/<result-id>.benchmark-result.json --root . \
  --out docs/evidence/ib-t010
```

**Provenance of every external number:** LiteLLM figures — source-reported, re-verified
2026-07-11 (docs.litellm.ai/docs/benchmarks); program SLO targets (overhead p95<10 ms /
p99<20 ms; G5 ≤20% degradation) — program-declared targets (docs/tasks.md IB-T010,
05-execution-roadmap G5); everything else — measured in this report's linked runs, 2026-07-11.
