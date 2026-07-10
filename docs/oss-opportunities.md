# OSS opportunities — inferbench

Where this repo's work can produce legitimate upstream contributions, and the rules that govern
them.

## Opportunities

1. **Reproducible engine-behavior reports (IB-T011).** Controlled, single-variable vLLM
   experiments with full manifests (engine version + commit + all flags, hardware, driver/CUDA,
   workload version + seed) are upstream-communicable evidence. This is the vLLM fallback track
   of the program's OSS plan — docs/metrics/tests scope only. Concrete shapes:
   - a reproducible behavior report (e.g. the `max_num_batched_tokens` TTFT/ITL trade-off, or
     preemption onset vs `gpu_memory_utilization`) filed as an issue with the manifest attached;
   - a metric-documentation correction grounded in measured evidence (vLLM metric names/semantics
     vary by version — the Contract 4 mapping work will surface any mismatches).

2. **Calibration deltas vs `vllm bench serve` (IB-T007).** If inferbench and the reference tool
   disagree and the cause is enumerable (different measurement points, warm-up handling, arrival
   process), that may be an upstream documentation or tooling issue worth filing. As of 2026-07 —
   re-verify the tool's current name and behavior before drawing or filing any conclusion.

## Rules

- **All upstream communication goes through the `inference-lab` OSS log and requires user review
  before posting.** Nothing is filed or commented directly from work in this repo.
- Evidence standard applies: every number in an upstream report carries provenance and a date,
  the full manifest, and a reproduction command — the same bar as our own published reports.
- **Avoid:** unverified performance claims, scheduler rewrites, unsolicited large refactors.
  Scope is documentation, metrics, tests, and reproducible reports.
- Volatility flag: upstream repo layouts, tool names, and metric names are all
  "as of 2026-07 — re-verify at use time".
