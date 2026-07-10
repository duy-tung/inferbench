# ADR-0001 — Open-loop seeded arrival scheduler

**Status:** Accepted (user review passed at the Wave-1 exit review, 2026-07-10) · **Date:** 2026-07-10 · **Owner task:** IB-T002

## Context

Latency benchmarks of queued systems suffer from **coordinated omission**: if the load generator
waits for (or is slowed by) responses before issuing the next request, a saturated target
throttles its own measurement — queueing delay disappears from the data exactly when it matters
most. Closed-loop generators (fixed concurrency, next request after previous completes) have this
failure mode built in. The program treats benchmark invalidity as its central methodology risk
(R4), and any latency or goodput claim from this repo must be immune to it.

## Decision

1. **Send times are precomputed** from `(workload version, seed, arrival process)` before the run
   starts. For `open-loop-poisson` at rate λ, inter-arrival gaps are drawn from a seeded PRNG;
   the resulting absolute schedule is fixed and, for the same seed, byte-identical across runs
   (this also gives deterministic replay for free).
2. **One scheduler goroutine owns the send timeline.** It fires request starts at the precomputed
   times using a monotonic clock. Nothing downstream — response latency, disk stalls, recorder
   backpressure, target saturation — can delay or reorder the schedule. Per-request goroutines own
   stream lifecycles; the recorder is a single writer behind a bounded channel.
3. **The client must prove it kept the schedule.** A schedule-slip watchdog compares intended vs
   actual send timestamps continuously; slip beyond the declared threshold aborts the run with
   typed reason `schedule_slip` and marks it INVALID. Client host resources (CPU, FDs, network)
   are sampled into the run log and cited in threats-to-validity.
4. **No client-side retries, ever.** A retry inserts an arrival the process didn't declare and
   hides an error; failed requests are recorded as classified events instead.
5. Connection setup is not allowed to serialize sends (e.g. a starved connection pool would be a
   covert closed loop); the client is provisioned so a send at time *t* starts at time *t*
   regardless of outstanding streams, or the watchdog trips.

## Alternatives considered

- **Closed-loop only** (simpler): rejected as the primary mode — it is the coordinated-omission
  anti-pattern. Retained as a flagged secondary mode for throughput-ceiling discovery only
  (ADR-0003).
- **HdrHistogram-style post-hoc CO correction** (record intended vs actual latency and back-fill):
  rejected — correction is an estimate; an open-loop process makes the problem structurally
  absent instead of statistically patched.
- **Response-adaptive rate control** (back off when the target errors): rejected — same
  coupling, same omission.

## Consequences

- The generator can (by design) drive the target past saturation; error/shed storms are recorded,
  not avoided — that is the honest picture.
- The client host becomes a validity dependency; hence the watchdog + resource sampling + typed
  aborts (see `observability.md`, `risks.md`).
- A CO-safety test is mandatory: a deliberately stalled mock target must not shift subsequent
  send times (see `testing.md` layer 3). This test guards the invariant against regression.
- Same seed → identical schedule becomes a first-class contract used by replay (IB-T008) and by
  determinism tests.
