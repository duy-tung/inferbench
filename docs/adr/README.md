# Architecture Decision Records — inferbench

| ID | Title | Status |
|---|---|---|
| [ADR-0001](ADR-0001-open-loop-scheduler.md) | Open-loop seeded arrival scheduler | Accepted (user review passed at the Wave-1 exit review, 2026-07-10) |
| [ADR-0002](ADR-0002-statistics-choices.md) | Statistics choices: pooled percentiles, bootstrap CIs, warm-up exclusion, knee detection | Accepted (user review passed at the Wave-1 exit review, 2026-07-10) |
| [ADR-0003](ADR-0003-closed-loop-flagging.md) | Closed-loop mode exists but is flagged everywhere | Accepted (user review passed at the Wave-1 exit review, 2026-07-10) |
| [ADR-0004](ADR-0004-tool-calibration-protocol.md) | Calibration protocol vs reference tooling | Accepted (user review passed at the Wave-1 exit review, 2026-07-10) |

Convention: one decision per file, `ADR-NNNN-slug.md`, status ∈ {Proposed, Accepted, Superseded}.
New ADRs are added when a decision constrains future work (statistics parameters, CLI shape, etc.)
and are linked from `implementation-notes.md`.
