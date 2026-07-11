# inferbench analysis core (IB-T005) + report generator (IB-T006)

The statistics half of inferbench: turns raw-event JSONL + run manifests
(serving-contracts Contract 3, pinned bundle) into schema-valid
benchmark-result files and honest Markdown reports.

What is structural here, not conventional (see `docs/adr/ADR-0002` and
`docs/experiments.md`):

- **Pooled percentiles only** — `PercentileTable` can only be built from raw
  samples pooled across repetitions; averaging per-run percentiles is not
  constructible and the emitted `method` is the schema const
  `pooled-raw-events`. Cross-run dispersion (median ± range) is a separate type.
- **Bootstrap CIs** — percentile bootstrap, B=1000, 95%, seeded (ADR-0002
  changelog). Report-surface only: the pinned result schema has no CI fields.
- **Warm-up exclusion** — policy comes from the manifest only; every exclusion
  is counted into the validity block.
- **Error/shed gate** — above the declared error+shed threshold (default 0.05)
  latency percentiles are withheld (`WithheldLatency`), the reason goes into
  the validity block, and result-file emission is a typed refusal. The run
  itself stays valid.
- **Goodput@SLO** — one pass, one frozen object: ratio + shed_rate +
  stall_rate + slo_ref, never separable; SLOs without a stall bound refused.
- **Cost** — only from provenanced cost-profile files; no profile → cost null
  + validity note.
- **Knee detection** — plateau-departure (1.5×, sustained) + kneedle
  cross-check; limitations emitted in the method string; ≥6 sweep points.
- **Refusal-first reports (IB-T006)** — one renderer, fixed section order in
  code (deliberately not a template file: an editable template is what the
  validity sections would get stripped from). A report cannot render without
  a complete validity block, a hypothesis in every embedded manifest (shown
  prominently), goodput with shed+stall adjacent, and either latency tables
  or the withheld-latency WHY block. "Unexplained anomalies" is never
  silently empty: it lists anomalies or states "none observed" beside the
  checks that were run. The benchmark comparability rule is embedded
  verbatim (drift vs the pinned bundle's policy file is a typed refusal);
  closed-loop arrival is flagged loudly; `cost: null` always renders with
  its reason; rule-8 one-command reproduction line included.

## Usage

```sh
pip install -e "analysis[dev]"

python3 -m inferbench_analysis analyze \
  --bundle /path/to/pinned/serving-contracts-bundle \
  --run docs/evidence/ib-t004/calib-A \
  --slo docs/evidence/ib-t005/mock-loopback.slo.json \
  --result-id my-result \
  --out out/my-result.benchmark-result.json

# honest Markdown report from an emitted result file:
python3 -m inferbench_analysis report \
  --bundle /path/to/pinned/serving-contracts-bundle \
  --result out/my-result.benchmark-result.json \
  --root . --out out/

# report regenerated from raw events (adds bootstrap CIs + dispersion;
# the ONLY publishable surface for valid runs whose latency is withheld):
python3 -m inferbench_analysis report \
  --bundle /path/to/pinned/serving-contracts-bundle \
  --run docs/evidence/ib-t004/calib-A \
  --slo docs/evidence/ib-t005/mock-loopback.slo.json \
  --result-id my-result --out out/
```

Exit codes: 0 = emitted + self-validated / report rendered; 1 = typed
refusal; 3 = analyze only: run valid but result not expressible (latency
withheld — see stdout for the reason; publish via `report --run ...`).

## Tests

```sh
cd analysis
CONTRACTS_BUNDLE=/path/to/pinned/bundle python -m pytest -q
```

Schema-dependent tests skip loudly when `CONTRACTS_BUNDLE` is unset; CI must
always set it (pinned bundle fetch per the contracts kit README).
