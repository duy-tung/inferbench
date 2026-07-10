# Testing strategy — inferbench

**GPU-free CI is a hard rule.** Everything below runs on CPU against fixtures and the released
mock-backend image. `go test -race ./...` clean is the floor for all Go work; Python tests run via
pytest.

## Test layers

### 1. Unit tests (Go generator)

- **Arrival-schedule determinism:** same seed → **byte-identical** send schedule. This is both a
  correctness test and the foundation of replay.
- **Poisson distribution sanity:** generated inter-arrival times statistically match the declared
  rate (e.g. mean/variance checks with tolerance bounds; not a visual eyeball).
- **SSE parser vs contract fixtures:** chunk framing, `data: [DONE]` terminator, usage-in-final-
  chunk, standardized mid-stream error events, monotonically increasing chunk indices — all
  driven by golden fixtures from the pinned contracts bundle.
- **Event serialization:** emitted raw events validate against the pinned
  `raw-event.schema.json`; field-level round-trip tests.
- **No-retry invariant:** a failing target produces exactly one classified event per scheduled
  request — never a second request.

### 2. Statistics known-answer tests (Python)

- Synthetic distributions with **analytically known** percentiles, CIs, and knees; the analysis
  core must reproduce them within declared numeric tolerance.
- **Pooled-vs-averaged-percentile guard:** a test constructed so that pooling and
  percentile-averaging give different answers, which FAILS if anyone reintroduces percentile
  averaging. This is the code-level enforcement of methodology rule 5.
- Warm-up exclusion tests: synthetic runs with a known warm-up transient; statistics must match
  the analytic steady-state values only when exclusion is applied.
- Goodput coupling test: the API cannot return a goodput figure without shed rate and stall rate
  from the same pass (constructed so omission is a type/shape error, not a silent gap).

### 3. Coordinated-omission safety test

A deliberately **stalled mock target** (long TTFT / mid-stream stalls / connection hangs) must not
shift subsequent send times: the test compares the intended schedule against actual send
timestamps and fails on drift beyond the watchdog threshold. This test is the executable form of
the open-loop invariant and blocks any regression that couples sends to responses.

### 4. Calibration tests vs the mock

The mock backend (owned by `infergate`) has *configured* TTFT/ITL/error rates. Runs against it
must measure ≈ configured values within a declared tolerance (tolerance stated in the calibration
harness, IB-T004). Cancellation issuance is verified at all three points (queued /
pre-first-token / mid-stream) and slow-client emulation is verified to hold its bounded read rate.

### 5. Contract compatibility (I1 arm)

CI validates **both directions** against the pinned contracts bundle:

- golden fixtures from the bundle parse/validate through this repo's loaders;
- this repo's emitted workload/run/event/result files validate against the bundle schemas
  (`make contracts-verify` pattern).

A bundle-version bump is a reviewed change; CI failure on bump is the drift detector.

### 6. Replay determinism

Replaying a recorded workload reproduces the identical send schedule and request contents
(seed-derived prompts included). Verified as an end-to-end test vs the mock.

### 7. Self-diagnostics and security tests

- Schedule-slip watchdog fires and aborts with a typed reason under induced client-side stall
  (e.g. artificially constrained scheduler).
- Manifest refusal: a run without a complete manifest does not start.
- **Secret-leak test:** run with a real-shaped API key in the environment; assert the key and any
  `Authorization` header appear in NO emitted artifact (manifest, raw events, logs, results,
  reports). See `security.md`.

## Performance hypotheses (tested, never assumed; provenance flagged)

| Hypothesis | Provenance | Where tested |
|---|---|---|
| infergate non-queue overhead ≤ low single-digit ms p95 (program SLO: p95 <10 ms / p99 <20 ms); LiteLLM self-reports 8 ms p95 — source-reported (as of 2026-07), to falsify, not a measured fact | source-reported / program target | IB-T010 |
| Admission control at ~5× overload keeps accepted-request TTFT p95 degradation ≤20% vs capacity-boundary baseline | program target (G5) | IB-T010 |
| Raising `max_num_batched_tokens` improves throughput but worsens ITL/TPOT tail (Sarathi-Serve trade-off) — reproduce with own numbers | source-reported (paper) | IB-T011 |
| Prefix caching improves TTFT proportionally to the controlled prefix-sharing ratio; near-zero effect at ratio ≈ 0 | assumed (RadixAttention reasoning) | IB-T011 |
| The generator sustains all published rates without client-side schedule slip | must be measured on the client host | every run (watchdog) |

## Evidence discipline

Never claim a test or benchmark succeeded without command output or artifacts to point at. Test
runs referenced from `implementation-notes.md` link the actual output. Failures are reported as
failures; skipped layers as skipped.
