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

## Deviations

> Program deviation policy: when repository evidence forces a deviation from the approved plan,
> choose the conservative reversible option, record the evidence, decision, consequences, and
> follow-up here, and continue. Pause for user input only when the deviation changes public
> contracts, repository ownership, security posture, or milestone scope.

*None recorded yet.*
