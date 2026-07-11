# IB-T009 refusal-demo transcript

Generated 2026-07-11T08:55:51Z against inferbench built from this checkout,
infergate pin `74f2372` (mock/gateway, built read-only via `git archive`).

## experiment run, no --hypothesis
```text
$ /home/user/inferbench/docs/evidence/ib-t009/inferbench-bin experiment run --workload /home/user/inferbench/docs/evidence/ib-t009/demo-workload.json --manifest /home/user/inferbench/docs/evidence/ib-t009/facts-gateway.json --target http://127.0.0.1:18380 --out /home/user/inferbench/docs/evidence/ib-t009/refused-run-nohyp
inferbench: experiment: --hypothesis is required (experiments.md §5); refusing to run a hypothesis-less experiment
(exit 1)
```

## experiment sweep, no --hypothesis
```text
$ /home/user/inferbench/docs/evidence/ib-t009/inferbench-bin experiment sweep --workload /home/user/inferbench/docs/evidence/ib-t009/demo-workload.json --manifest /home/user/inferbench/docs/evidence/ib-t009/facts-gateway.json --target http://127.0.0.1:18380 --out /home/user/inferbench/docs/evidence/ib-t009/refused-sweep-nohyp
inferbench: experiment: --hypothesis is required (experiments.md §5); refusing to run a hypothesis-less experiment
(exit 1)
```

## experiment compare, no --hypothesis
```text
$ /home/user/inferbench/docs/evidence/ib-t009/inferbench-bin experiment compare --workload /home/user/inferbench/docs/evidence/ib-t009/demo-workload.json --variable target_topology --arm direct=/home/user/inferbench/docs/evidence/ib-t009/facts-direct.json@http://127.0.0.1:18381 --arm gateway=/home/user/inferbench/docs/evidence/ib-t009/facts-gateway.json@http://127.0.0.1:18380 --out /home/user/inferbench/docs/evidence/ib-t009/refused-compare-nohyp
inferbench: experiment: --hypothesis is required (experiments.md §5); refusing to run a hypothesis-less experiment
(exit 1)
```

## experiment run, incomplete hypothesis (missing stop_condition)
```text
$ /home/user/inferbench/docs/evidence/ib-t009/inferbench-bin experiment run --hypothesis /home/user/inferbench/docs/evidence/ib-t009/hyp-incomplete.json --workload /home/user/inferbench/docs/evidence/ib-t009/demo-workload.json --manifest /home/user/inferbench/docs/evidence/ib-t009/facts-gateway.json --target http://127.0.0.1:18380 --out /home/user/inferbench/docs/evidence/ib-t009/refused-run-incomplete
inferbench: experiment: incomplete hypothesis file: stop_condition is required
(exit 1)
```

## experiment compare, arms differ in 2 fields (target_topology declared, model.checkpoint also differs)
```text
$ /home/user/inferbench/docs/evidence/ib-t009/inferbench-bin experiment compare --hypothesis hypotheses/EXP-ib-t009-gateway-overhead-demo.json --workload /home/user/inferbench/docs/evidence/ib-t009/demo-workload.json --variable target_topology --arm direct=/home/user/inferbench/docs/evidence/ib-t009/facts-direct.json@http://127.0.0.1:18381 --arm gateway=/home/user/inferbench/docs/evidence/ib-t009/facts-gateway-diffmodel.json@http://127.0.0.1:18380 --out /home/user/inferbench/docs/evidence/ib-t009/refused-compare-combinatorial
inferbench: experiment: arms vary in more than the single declared variable — no full-matrix sweeps (experiments.md §5): arm 1 () differs from arm 0 () in "model.checkpoint"; declared variable is "target_topology"
(exit 1)
```

## experiment compare, --variable != hypothesis.variable
```text
$ /home/user/inferbench/docs/evidence/ib-t009/inferbench-bin experiment compare --hypothesis hypotheses/EXP-ib-t009-gateway-overhead-demo.json --workload /home/user/inferbench/docs/evidence/ib-t009/demo-workload.json --variable engine.flags.max_num_seqs --arm direct=/home/user/inferbench/docs/evidence/ib-t009/facts-direct.json@http://127.0.0.1:18381 --arm gateway=/home/user/inferbench/docs/evidence/ib-t009/facts-gateway.json@http://127.0.0.1:18380 --out /home/user/inferbench/docs/evidence/ib-t009/refused-compare-variable-mismatch
inferbench: experiment: hypothesis declares variable "target_topology" but --variable is "engine.flags.max_num_seqs"; they must match
(exit 1)
```

## experiment run against a GPU-declaring manifest, hypothesis has no gpu_session block
```text
$ /home/user/inferbench/docs/evidence/ib-t009/inferbench-bin experiment run --hypothesis hypotheses/EXP-ib-t009-gateway-overhead-demo.json --workload /home/user/inferbench/docs/evidence/ib-t009/demo-workload.json --manifest /home/user/inferbench/docs/evidence/ib-t009/facts-gpu-no-session.json --target http://127.0.0.1:18380 --out /home/user/inferbench/docs/evidence/ib-t009/refused-run-gpu-no-session
inferbench: experiment: hardware declares a GPU session but the hypothesis file has no gpu_session block (session manifest ref + auto-stop ref + budget alert confirmation required, G6) (run_id=)
(exit 1)
```

## compliant experiment compare (hypothesis + single-variable arms, executes)
```text
$ /home/user/inferbench/docs/evidence/ib-t009/inferbench-bin experiment compare --hypothesis hypotheses/EXP-ib-t009-gateway-overhead-demo.json ...
experiment compare (hypothesis EXP-ib-t009-gateway-overhead-demo): variable=target_topology diff_fields=[gateway target_topology]
  arm direct: target=http://127.0.0.1:18381 reps=1 sent=40 ok=40 errors=0 shed=0 canceled=0
  arm gateway: target=http://127.0.0.1:18380 reps=1 sent=40 ok=40 errors=0 shed=0 canceled=0
comparison manifest: /home/user/inferbench/docs/evidence/ib-t009/compliant-compare/comparison.json
(exit 0)
```
