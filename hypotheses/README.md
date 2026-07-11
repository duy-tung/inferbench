# Hypothesis files (IB-T009)

Every controlled experiment run through `inferbench experiment {run,sweep,compare}` requires a
hypothesis file. The framework refuses to generate any load without one (typed
`experiment.ErrHypothesisRequired`) — this is the G6 enforcement mechanism described in
`docs/experiments.md` §5.

## Format: JSON, not the YAML sketch in `docs/experiments.md` §5

The field set below is identical to the `docs/experiments.md` §5 template; only the
serialization differs. The Go module is stdlib-only end to end (see
`docs/implementation-notes.md`, IB-T002), and every other artifact this repo reads or writes
(workload, manifest, raw-event) is JSON. Adding a YAML dependency for exactly one file type was
judged not worth it — a **reversible, recorded decision** (`docs/implementation-notes.md`,
IB-T009). Unknown fields are rejected (`encoding/json` `DisallowUnknownFields`), so a mistyped
field (e.g. a plural `variables` where a matrix was intended) is refused, not silently ignored.

## Fields (all required unless noted)

| Field | Meaning |
|---|---|
| `id` | stable experiment id, e.g. `EXP-mnbt-001` |
| `hypothesis` | falsifiable statement with expected direction (>= 10 chars) |
| `variable` | the ONE declared experimental variable, as a dotted manifest-field path (e.g. `target_topology`, `engine.flags.max_num_seqs`) — matches the path convention `internal/manifest.Diff` and `internal/structdiff` use |
| `levels` | the levels tested (>= 2); no cross-products — a second `variable`-shaped field is a JSON decode error (unknown field), not a silently accepted matrix |
| `expected_direction` | the falsifiable direction, e.g. "throughput up, ITL tail worse" |
| `workload` | the workload identity (name@version, seed) the experiment runs against |
| `constants` | everything held fixed: engine version+flags, model+revision, hardware, workload version+seed, warm-up policy |
| `stop_condition` | when to stop, including abort criteria and (for GPU) the budget cap |
| `repeat_policy` | e.g. "3 runs per point, pooled percentiles" (experiments.md rule 4) |
| `slo_reference` | optional; required if goodput is claimed |
| `provenance_notes` | optional; source-reported baselines being falsified, with dates |
| `gpu_session` | required ONLY when any arm's manifest declares GPU hardware (`hardware.gpu_count > 0` or `hardware.gpu_model` non-null) — G6: `session_manifest_ref`, `auto_stop_ref`, `budget_alert_confirmed: true` |

## Enforcement (`internal/experiment`)

- `Load(path)` refuses (`ErrHypothesisRequired`) an empty path or unreadable file, and
  (`ErrIncompleteHypothesis`) a file missing any required field or declaring < 2 levels — all
  BEFORE any workload/manifest/target flag is even consulted for reachability.
- `Hypothesis.CheckArms(manifests)` refuses (`ErrCombinatorial`) an arm set that differs from the
  first arm in anything other than the declared `variable` (structurally, via
  `internal/manifest.Diff` — the same primitive `compare` and `sweep` use), and refuses
  (`ErrNotVarying`) a degenerate arm set where the declared variable never actually differs.
- `Hypothesis.CheckGPUSession(manifests)` refuses (`ErrGPUSessionRequired`) any GPU-declaring arm
  when `gpu_session` is missing or incomplete.

See `docs/evidence/ib-t009/` for the refusal-demo transcript and a compliant end-to-end run.

## Files here

- `TEMPLATE.json` — copy this and fill in every field.
- `EXP-ib-t009-gateway-overhead-demo.json` — the compliant example used in the IB-T009 evidence
  (a single-variable direct-vs-gateway comparison sketch; the REAL gateway-overhead experiment
  with a full methodology run is IB-T010 — this is IB-T009's governance-mechanics demo, not a
  published benchmark claim).
