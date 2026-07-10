# Milestones — inferbench

Dependency-ordered; no calendar durations. Task details in `tasks.md`. The critical path is
IB-T002 → IB-T004 → IB-T005 → IB-T006 → IB-T010; tasks marked parallel-safe in `tasks.md` may
proceed concurrently once their dependencies are met.

| # | Milestone | Contents | Acceptance criteria |
|---|---|---|---|
| M1 | Plan & docs | IB-T001 | all 15 `docs/` files + the `adr/` directory exist with repo-specific content; reviewed |
| M2 | Generator core | IB-T002, IB-T003 | open-loop + Poisson + seeds; 8 workloads validate against the pinned schema and dry-run vs the released mock image; emitted JSONL events schema-valid; same seed → identical send schedule; CO-safety design reviewed |
| M3 | Measurement correctness | IB-T004 | client TTFT/ITL vs mock's configured latencies agree within declared tolerance (calibration report vs mock); cancellation issuance works at all 3 points; slow-client emulation bounded-rate verified |
| M4 | Analysis & reports (gate **G4**) | IB-T005, IB-T006 | synthetic known-answer statistics tests green; pooled-percentile + shed-adjacent-goodput + stall-rate enforced in code; sample end-to-end report from a mock run is schema-valid and passes G4 methodology review |
| M5 | Sweeps & governance | IB-T008, IB-T009 | sweep (≥6 points) produces a knee on the mock; replay deterministic; framework rejects hypothesis-less experiment runs and combinatorial sweeps |
| M6 | Calibration | IB-T007 | deltas vs reference tooling (`vllm bench serve` on GPU, or llama.cpp-based reference on CPU — as of 2026-07, re-verify tool behavior at use time) tabulated and within stated tolerance or explained |
| M7 | Experiment set 1 (CPU) | IB-T010 | benchmark report #1 published: gateway overhead (direct vs via-gateway, mock + llama.cpp) and admission on/off at ~5× capacity; ≥3 runs/point, pooled stats, validity block; feeds gate G5 |
| M8 | Experiment set 2 (GPU) | IB-T011 | benchmark report #2 published from ≤ budgeted scripted GPU sessions, or the documented CPU fallback deviation |
| M9 | Independence proof | (part of DoD) | a run against a non-infergate OpenAI-compatible endpoint completes with schema-valid outputs |
| M10 | Stretch | IB-T012 | only if budget remains and baseline stable; kill rules apply |

## Gate notes

- **G4 (methodology review)** closes with M4 *and* M5's governance half (IB-T006 + IB-T009
  together). No report is published before G4 passes.
- **G6 (GPU session gate)** applies per session to IB-T007 (GPU variant), IB-T011, IB-T012:
  written hypothesis + full config manifest + auto-stop script + budget alert. Program GPU
  envelope ~$150–250 total (as of 2026-07, user-confirmable), alerts at 50% and 80%.
- **M6 ordering:** IB-T007 is not on the critical path and its GPU variant is gated on G6; the
  CPU (llama.cpp-based reference) variant can run any time after M3.
- **M8 fallback:** if the GPU budget is unavailable, the documented CPU-fallback deviation
  (llama.cpp becomes the measured baseline) is recorded in `implementation-notes.md` per the
  deviation policy — M8 does not silently disappear.
- **M10 kill rules:** SGLang comparison drops first (>4 h setup without a running comparison, GPU
  budget ≥80% consumed, or unstable vLLM baseline); pre-armed fallback = vLLM prefix caching
  on/off, which is already in IB-T011.
