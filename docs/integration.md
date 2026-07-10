# Integration — inferbench

This repo integrates only via the pinned contracts bundle, network targets, and files. Its result
files are the evidence backbone of the portfolio. Roles and acceptance criteria per integration
milestone:

## I1 — Contract compatibility (re-entrant)

**Role:** consumer + emitter validation. inferbench CI validates the bundle's golden fixtures
through its loaders AND validates its own emitted artifacts (workloads, manifests, raw events,
results) against the pinned bundle (`make contracts-verify` pattern).

**Acceptance:** green CI run linked in the `inference-lab` pins file. Re-entrant: re-run on every
bundle pin bump.

## I2 — Local request path (`inferbench → infergate → mock`)

**Role:** driver and measurer of the first end-to-end path.

**Acceptance:**
- seeded workload with 100 concurrent streams, zero frame mixing;
- 3-point cancellation verified — mock abort observed within bound for queued /
  pre-first-token / mid-stream cancels;
- raw events schema-valid;
- **client-vs-gateway TTFT agreement within declared tolerance** (the mirror-series comparison);
- traces/metrics visible on the gateway side.

**Failure handling:** measurement disagreement → check measurement-point definitions (Contract 2)
before touching code.

## I3 — Local inference (`inferbench → infergate → llama.cpp`)

**Role:** first real-engine benchmark and first published report.

**Acceptance:**
- `chat-short` and `shared-prefix` complete on CPU;
- **first schema-valid benchmark report** generated (full manifest + validity block);
- cancellation verified against llama.cpp.

**Failure handling:** invalid report → G4 review before proceeding.

## I4 — GPU inference (`inferbench → infergate → vLLM`)

**Role:** gateway-overhead measurement on real GPU serving.

**Acceptance:**
- streaming + cancellation verified via engine metrics;
- **gateway-overhead comparison (direct vs via-gateway) measured with ≥3 runs/point**;
- session auto-stopped (G6);
- all artifacts carry the full manifest.

**Fallback:** CPU fallback = documented deviation in `implementation-notes.md`; llama.cpp becomes
the measured baseline.

## I6 — Capacity feedback (the central story)

**Role:** both ends of the loop. inferbench's benchmark corpus (IB-T010/T011) feeds `fleetlab` →
capacity recommendation (Contract 7) → `inferops` applies it → **inferbench re-benchmarks the
outcome**. Predicted vs measured is published, including where the prediction was wrong.

**Acceptance:** the loop closes with schema-valid result files on both sides. Requires contracts
v1.0.0 (Contract 1–3 shapes frozen). Never-cut: this loop may shrink to mock/llama.cpp scale but
must close.

## I7 — Failure campaign

**Role:** client-impact measurement during fault injection (faults injected by `inferops`;
scenario vocabulary is Contract 6).

**Acceptance:** client-impact measurements for at least the streaming-critical scenarios —
**1** (backend killed pre-first-token), **2** (backend killed post-first-token), **5** (gateway
termination during streaming), **6** (queue saturation), **12** (rolling update under load) —
attached to the campaign matrix and to ≥2 postmortems. inferbench records what the *client*
experienced (error classes, partial streams, stalls, shed typing) per the Contract 1 taxonomy.

## Dependency direction summary

- Upstream of inferbench: `serving-contracts` (pinned bundle), `infergate`'s released mock image
  (CI target) and released gateway builds (network targets at integration time).
- Downstream of inferbench: `fleetlab` (result + raw-event + workload files), `inference-lab`
  (reports + result files as evidence).
- Cross-repo evidence is filed in `inference-lab` at integration time; local evidence links live
  in `implementation-notes.md`.
