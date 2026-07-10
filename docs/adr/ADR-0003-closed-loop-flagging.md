# ADR-0003 — Closed-loop mode exists but is flagged everywhere

**Status:** Accepted (user review passed at the Wave-1 exit review, 2026-07-10) · **Date:** 2026-07-10 · **Owner tasks:** IB-T002 (generator), IB-T006 (report)

## Context

Open-loop Poisson arrivals are mandatory for latency/goodput claims (ADR-0001, methodology
rule 1). But one legitimate question is closed-loop by nature: *what is the target's maximum
sustainable throughput?* A closed-loop run at fixed concurrency finds that ceiling directly and
also helps place the 10%→120% sweep range. The danger is that closed-loop numbers leak into
latency claims, where they hide queueing delay (coordinated omission).

## Decision

Closed-loop mode exists, narrowly, with mandatory disclosure at every layer:

1. **Workload level:** the arrival process is declared in the workload file
   (`open-loop-poisson` | `closed-loop`); Contract 3's workload schema makes the closed-loop
   disclosure flag mandatory. No CLI override can convert a run's declared process.
2. **Event/manifest level:** every run's artifacts carry the arrival process; a closed-loop run
   is identifiable from any single artifact in isolation.
3. **Analysis level:** the analysis propagates the flag; if any contributing run in a result set
   is closed-loop, the result is closed-loop-flagged as a whole.
4. **Report level (IB-T006):** closed-loop results are **visibly flagged** in every table and
   figure they appear in, with a standard caption: closed-loop hides queueing delay; valid only
   as a throughput-ceiling observation. The report generator refuses to render a latency or
   goodput claim sourced from closed-loop data.
5. **Permitted uses:** throughput-ceiling discovery and capacity estimation for sweep-range
   placement. Nothing else. Published latency/goodput claims must trace to open-loop runs.

## Alternatives considered

- **No closed-loop mode at all**: rejected — the throughput ceiling is a legitimate measurement,
  and teams without a sanctioned mode tend to improvise unsanctioned ones.
- **Flag as convention only (docs, not code)**: rejected — R4 mitigation must be structural;
  conventions decay.

## Consequences

- The schema-level flag means downstream consumers (`fleetlab`, `inference-lab`) can also refuse
  or segregate closed-loop data mechanically.
- The report generator needs a data-lineage check (which runs fed which table) — a small cost
  that also serves the one-command-reproduction rule.
- Tests: a closed-loop-sourced latency table must fail report generation (part of IB-T006's
  honesty-rule tests).
