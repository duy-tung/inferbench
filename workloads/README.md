# Canonical workload suite v1 (IB-T003)

The eight named workloads of the program, as versioned seeded files per the contracts-owned
`workload.schema.json` (pinned bundle `v0.1.0`). **These files are canonical**: `fleetlab` and
all published reports consume them from here. The similarly named files under
`serving-contracts/examples/workloads/` are non-normative schema fixtures and must never be
cited as the benchmark configuration.

| File | Intent | Key controlled parameters | Runnable today? |
|---|---|---|---|
| `chat-short.json` | interactive chat baseline | short input/output distributions, output cap 384 | yes |
| `rag-long-in.json` | prefill-heavy (long context in) | input uniform 2048–8192, output cap 512 | yes |
| `gen-long-out.json` | decode-heavy (long generation) | output directed: floor 512, cap 2048 | yes |
| `shared-prefix.json` | prefix-cache behavior | **ratio 0.8, prefix 1024 tokens, groups of 16** | yes (IB-T004) |
| `mixed.json` | realistic blend | declared 60/25/15 mixture proportions | yes |
| `bursty.json` | queueing/admission behavior | 2 rps base, 10× burst 15 s, period 75 s | yes |
| `cancel-storm.json` | cancellation correctness under load | cancel rate 0.5, point uniform 0.2–3.0 s | yes (IB-T004) |
| `slow-client.json` | backpressure/write-buffer behavior | 25% readers at 1024 B/s | yes (IB-T004) |

## Design rules (enforced by `internal/workload/suite_test.go`)

- **Versioned + seeded.** Every file carries SemVer `version` (`1.0.0` for the whole suite) and a
  fixed distinct seed (`10030NN`, NN = suite position). Any change to any traffic-shaping field
  bumps the version; results are comparable only across identical version + seed
  (`docs/experiments.md` rule 10).
- **Output length is always capped or directed** — every output-length distribution (and every
  mixture component) declares a finite upper bound, and normal/lognormal declare a floor too.
  Uncontrolled output length is a named anti-pattern (methodology rule 12): without a cap,
  decode time — and therefore every latency and throughput number — is at the model's mercy and
  runs stop being comparable.
- **Input length is bounded the same way** (prompts are generated to the sampled length, so an
  unbounded tail would also mean unbounded request bodies).
- **Open-loop arrivals only** in the canonical suite. Closed-loop definitions are allowed by the
  schema (with the mandatory disclosure flag) but have no place here — the suite exists to make
  latency/goodput claims (ADR-0001/ADR-0003).
- **Prefix sharing is a controlled variable, not an accident.** `shared-prefix` pins ratio 0.8,
  prefix length 1024 tokens, group size 16; the input floor (1152) exceeds the prefix length so
  every sharing request still has a unique suffix. Prefix-cache experiments (IB-T011) vary ONLY
  the ratio, publishing each level as a versioned variant of this file.
- **Zero is declared, never implied.** Workloads that use no prefix sharing / cancellation /
  slow clients still declare `ratio: 0`, `rate: 0`, `fraction: 0` per the schema.

## Known limitation (recorded)

`mixed` declares its 60/25/15 blend independently on the input and output side: the schema has
no joint (input, output) distribution, so proportions hold per dimension but an individual
request can pair, say, a RAG-like input with a generation-like output cap. If correlated
archetype pairs become necessary for realism claims, that is a schema change to propose to
`serving-contracts` — never to hack in here (this repo owns no schema).

## Execution status

Since IB-T004 the whole suite executes end-to-end: prefix-sharing prompt construction (shared
prefix per group, unique suffix per request), cancellation issuance (elapsed-seconds and
output-tokens triggers; honest `cancellation_point` per event, planned-vs-realized counts in
the run log), and slow-client read throttling (bounded bytes/second + initial read delay) are
implemented in the client. Closed-loop arrival remains the only typed `ErrNotImplemented`
refusal (IB-T008). A refusal is not a silent downgrade: a workload never runs with its defining
feature quietly disabled.

## Dry-running the suite

`scripts/dryrun-workloads.sh <target-url> <out-dir>` derives short, low-volume variants (stop
condition shortened; bursty phases compressed 5×) and runs all eight as streaming runs against
an already-running target, recording per-workload artifacts. Derived variants are for smoke
only — published results always use these files as-is.
