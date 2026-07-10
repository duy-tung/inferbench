# Non-goals — inferbench

Explicit exclusions. These are checked at every review gate; violating one is a boundary defect
even if the code works.

## Things this repository will never build

- **No gateway logic.** Admission, routing, quotas, retries, shedding, streaming relay — all owned
  by `infergate`. `inferbench` *measures* these behaviors from the outside; it never implements or
  re-implements them.
- **No engine logic and no engine-internal assertions.** Continuous batching, per-token
  scheduling, KV-cache internals, prefix-cache internals, GPU placement are engine-owned. The
  target is a black-box network endpoint. Engine behavior is observed via the API surface and the
  Contract 4 metrics mapping only.
- **No Kubernetes manifests, no deployment stack.** Owned by `inferops`. This repo ships a CLI and
  a Python package, not a service.
- **No capacity modeling.** `inferbench` produces the result files `fleetlab` consumes; it does
  not model autoscaling, placement, headroom, or cost projections. The boundary is the
  `benchmark-result` file.
- **No dashboards.** Grafana/observability deployment is `inferops`'s. This repo's observability
  is run logs and self-diagnostics (see `observability.md`).
- **No second load generator anywhere else, and no growth beyond load generation here.** The
  single-owner matrix is program law in both directions.

## Things this repository deliberately does NOT do

- **No schema ownership.** Workload, run-manifest, raw-event, result, cost-profile, and SLO
  schemas are owned by `serving-contracts`. `inferbench` emits artifacts that validate against the
  pinned bundle. Any schema-affecting change is *proposed to contracts* and blocked until contracts
  release it — never made locally.
- **No client-side retries, ever.** A retry corrupts the open-loop arrival process and hides
  errors. Failed requests are recorded as classified events. Retry behavior is the gateway's and
  is observed, not performed. This is a hard invariant, not a configuration default.
- **No shared statistics library with `fleetlab`.** The metric definitions both repos rely on live
  in the contracts metric vocabulary; data exchange is files only. No shared application library
  with any repo.
- **No brokers** (Kafka/NATS/Redis or similar) anywhere — program-wide rule for the synchronous
  inference path, and this repo has no need for one anywhere else either.
- **No database, no server.** Files only: JSONL events, JSON manifests/results, Markdown/HTML
  reports.
- **No importing target source.** Build and test against recorded fixtures and the released
  mock-backend image (owned by `infergate`).
- **No uncontrolled comparisons.** The comparison engine refuses cross-hardware, cross-tool, and
  multi-variable comparisons (methodology rule 10). This is a feature boundary, not just a review
  rule.
- **No full-matrix parameter sweeps.** Experiments are hypothesis-driven and single-variable; the
  experiment framework rejects combinatorial sweeps (IB-T009).
- **No publishing invalid runs.** Runs that trip abort conditions (schedule slip, client
  saturation, incomplete manifest) are invalidated, never published. There is no "publish with a
  caveat" path for invalid data.
- **No real user data.** Workload prompts are synthetic, generated from seeds. No PII, ever.
- **No GPU requirement for development or CI.** GPU sessions exist only behind gate G6 for
  IB-T007 (GPU variant), IB-T011, and IB-T012.
