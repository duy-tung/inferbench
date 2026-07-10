# Risks — inferbench

Risk register with triggers and mitigations. R-numbers follow the program register; the last two
are repo-local.

## R4 — Benchmark invalidity (owned by this repo)

The program's central methodology risk: coordinated omission, uncontrolled variables, missing
warm-up handling, version drift between compared runs, undisclosed hardware, single-run
conclusions, mean-only reporting, cherry-picked runs.

- **Trigger:** G4 audit failure; incomplete validity block in any produced result.
- **Mitigation:** methodology encoded in schemas + report template + experiment framework
  (IB-T006/IB-T009), not left to discipline. **Gate G4: methodology review before ANY published
  report.** "Invalidate, don't publish" — an invalid run is marked invalid with a typed reason and
  never appears in a report. Every published report gets a fresh-context audit against the
  validity checklist in `experiments.md`.
- **Residual:** subtle CO leaks (e.g. connection-pool exhaustion delaying sends) — covered by the
  CO-safety test (deliberately stalled mock must not shift subsequent send times) and the
  schedule-slip watchdog.

## R2 — GPU budget overrun / unavailability

- **Trigger:** budget alert fires (50% / 80% of the ~$150–250 program envelope, as of 2026-07,
  user-confirmable).
- **Mitigation:** gate **G6** per session — written hypothesis + full config manifest + auto-stop
  script + budget alert. Only IB-T007 (GPU variant), IB-T011, and IB-T012 may spend GPU time in
  this repo. Any hypothesis-less GPU run is stopped immediately. CPU fallbacks (llama.cpp-based)
  are documented as deviations in `implementation-notes.md`, and llama.cpp becomes the measured
  baseline (I4 fallback rule).

## R10 — Stretch experiments destabilize the baseline

- **Trigger:** >4 h SGLang setup without a running comparison; vLLM baseline unstable; GPU budget
  ≥80% consumed.
- **Mitigation:** kill order — **SGLang comparison drops first**; pre-armed fallback = vLLM
  prefix caching on/off (already in IB-T011); then speculative decoding/MTP and KV offloading
  drop. IB-T012 is entirely stretch; a documented kill note is an acceptable outcome.

## Client host becomes the bottleneck (silent invalidity)

If the generator host runs out of CPU, file descriptors, or network capacity, it silently stops
keeping the open-loop schedule and every number becomes meaningless while looking plausible.

- **Trigger:** schedule-slip watchdog fires; client resource sampling shows saturation.
- **Mitigation:** abort with a typed reason and **invalidate the run**; record client resource
  samples in the run log; cite client-host headroom in every report's threats-to-validity section.
  The hypothesis "the generator sustains all published rates without client-side schedule slip"
  is re-verified by the watchdog on every run — never assumed.

## Tool-calibration mismatch undermines credibility

If `inferbench`'s numbers disagree with reference tooling (`vllm bench serve`, or a
llama.cpp-based reference on CPU) and the deltas are unexplained, every published number is
suspect.

- **Trigger:** IB-T007 deltas outside stated tolerance without an explanation.
- **Mitigation:** calibration protocol (ADR-0004) requires enumerating measurement-point,
  warm-up, and arrival-process differences *before* comparing; **no comparative claims are
  published until deltas are explained**. Deltas may also be legitimate upstream findings (see
  `oss-opportunities.md`). Note: reference-tool behavior is volatile — as of 2026-07, re-verify
  the tool's name/flags/measurement points at use time.

## Contracts-bundle availability / drift

This repo owns no schema; it cannot start IB-T002+ until the contracts bundle (SC-T002/SC-T003)
is released, and a bundle MINOR may break during v0.x (documented program rule).

- **Trigger:** bundle not yet released (true as of 2026-07-10); or a pinned-bundle bump changes a
  consumed schema.
- **Mitigation:** pin recorded in `implementation-notes.md` at IB-T002 start; CI validates emitted
  artifacts against the pinned bundle so drift is caught mechanically (I1 arm); schema needs are
  proposed upstream, never patched locally.

## Never-cut list (this repo's slice)

Under any schedule or budget pressure: methodologically valid benchmarking and this repo's role in
the I6 loop are never cut. The I6 loop may shrink to mock/llama.cpp scale but must close.
Reducible: IB-T012 (entirely stretch).
