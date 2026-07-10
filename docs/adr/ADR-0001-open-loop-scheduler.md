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
3. **The client must prove it kept the schedule — all the way to the wire.** A two-stage
   schedule-slip watchdog enforces this (semantics fixed after the 2026-07-10 CO-safety review
   found the wire segment unmonitored): *dispatch stage* — intended vs actual dispatch time,
   checked in the scheduler loop; *wire stage* — scheduled send time vs actual wire-write time
   (`send_ts − scheduled_send_ts`), checked when each request completes its send, covering
   goroutine start, marshal, DNS/TCP/TLS connect, and blocked body writes. Slip beyond the
   declared threshold at either stage aborts the run with typed reason `schedule_slip` and marks
   it INVALID. Client host resources (CPU, FDs, network) are sampled into the run log and cited
   in threats-to-validity.
   **Measurement basis:** recorded client-side TTFT and end-to-end latency are measured from the
   *scheduled* send time (`scheduled_send_ts`, required by raw-event v0.2.0), never from the
   actual wire-write time, so sub-threshold slip still counts against latency instead of
   vanishing; `send_ts` is kept as a diagnostic and `send_slip_seconds` makes the slip visible
   per event.
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
- CO-safety tests are mandatory and two-sided (see `testing.md` layer 3): a deliberately stalled
  mock target must not shift subsequent send times (send-schedule independence), and a
  deliberately slow-to-connect target (accept-queue-full model) must have its connect delay
  included in recorded latency and must trip the wire-stage watchdog beyond threshold
  (measurement completeness). Both guard the invariant against regression.
- At-saturation caveat: connect delay caused by the *target* is client-visible queueing and is
  measured (it counts against TTFT via the scheduled-send basis); the wire-stage watchdog
  threshold bounds how much of it a *valid* run may absorb before the run must instead be
  declared saturated-beyond-measurement and INVALID. Raising `--max-slip` for overload studies is
  legitimate only because the slip is recorded per event either way.
- Same seed → identical schedule becomes a first-class contract used by replay (IB-T008) and by
  determinism tests.
