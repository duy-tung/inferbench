"""IB-T006 report generator: the honesty rules must be structurally
unbreakable — no report without validity sections, hypothesis prominent,
goodput never without shed+stall adjacent, withheld latency always explains
WHY, anomalies never silently empty, comparability rule verbatim."""

import dataclasses
import json

import pytest

from inferbench_analysis import (
    AnalysisConfig,
    COMPARABILITY_RULE_VERBATIM,
    ManifestEntry,
    ReportInputError,
    ReportModel,
    analyze_run_set,
    check_comparability_verbatim,
    report_from_analysis,
    report_from_result_dict,
)
from inferbench_analysis.percentiles import BootstrapParams
from inferbench_analysis.report import _render
from conftest import make_event, make_manifest, make_run, make_slo

REPRO = (
    "python3 -m inferbench_analysis report --bundle B --run synthetic/syn-A "
    "--slo slo.json --result-id rep-test"
)

OPEN_LOOP_WL = {
    "name": "synthetic",
    "version": "1.0.0",
    "seed": 1,
    "arrival_process": {"type": "open-loop-poisson", "rate_rps": 2},
}
CLOSED_LOOP_WL = {
    "name": "synthetic",
    "version": "1.0.0",
    "seed": 1,
    "arrival_process": {
        "type": "closed-loop",
        "concurrency": 4,
        "closed_loop_disclosed": True,
    },
}


def _ok_events(n=20):
    return [
        make_event(i, sched=i * 0.5, ttft=0.1, e2e=0.5, itl_series=(0.02, 0.03))
        for i in range(n)
    ]


def _result(events=None, run=None, anomalies=(), threats=(), bootstrap=None):
    run = run or make_run(events if events is not None else _ok_events())
    return run, analyze_run_set(
        [run],
        AnalysisConfig(
            slo=make_slo(),
            bootstrap=bootstrap,
            anomalies=tuple(anomalies),
            extra_threats=tuple(threats),
        ),
        result_id="rep-test",
        created_at="2026-07-11T00:00:00Z",
    )


def _entries(run, workload=None):
    return [
        ManifestEntry(
            path=run.manifest_path,
            manifest=run.manifest,
            workload=workload,
            workload_path="synthetic/syn-A/workload.json" if workload else None,
        )
    ]


def _report(events=None, run=None, workload=None, **kw):
    run, res = _result(events=events, run=run, **kw)
    return report_from_analysis(
        res, _entries(run, workload), repro_command=REPRO
    )


def _section(md: str, header: str) -> str:
    """The report slice from `header` to the next same-or-higher heading."""
    start = md.index(header)
    level = header.split(" ")[0]  # "##" or "###"
    rest = md[start + len(header):]
    ends = []
    for prefix in ("\n## ", "\n### " if level == "###" else "\n## "):
        i = rest.find(prefix)
        if i != -1:
            ends.append(i)
    return rest[: min(ends)] if ends else rest


def _minimal_doc(**overrides):
    table = {
        "n": 10, "p50": 0.1, "p90": 0.11, "p95": 0.12, "p99": 0.13,
        "max": 0.2, "mean": 0.1,
    }
    doc = {
        "result_id": "hand-doc",
        "created_at": "2026-07-11T00:00:00Z",
        "links": {
            "run_manifests": ["synthetic/syn-A/manifest.json"],
            "raw_events": ["synthetic/syn-A/events.jsonl"],
        },
        "throughput": {
            "requests_per_second": 1.0,
            "output_tokens_per_second": 10.0,
            "total_requests": 10,
            "total_output_tokens": 100,
        },
        "pooled_percentiles": {
            "method": "pooled-raw-events",
            "pooled_event_count": 10,
            "tables": {
                "ttft_seconds": dict(table),
                "e2e_duration_seconds": dict(table),
            },
        },
        "goodput": {
            "slo_ref": {"id": "syn-slo", "version": "1.0.0"},
            "requests_per_second_meeting_slo": 1.0,
            "ratio": 1.0,
            "shed_rate": 0.0,
            "stall_rate": 0.0,
        },
        "knee_estimate": None,
        "cost": None,
        "validity": {
            "warm_up_handling": "manifest warm-up policy 'none': no warm-up "
            "exclusion applied; 0 of 10 events excluded",
            "run_count": 1,
            "threats_to_validity": [
                "no cost profile applies to this run set — cost is null "
                "(cost figures are only computed from a declared, "
                "provenanced cost-profile file, never from assumed rates)"
            ],
            "unexplained_anomalies": [],
        },
    }
    doc.update(overrides)
    return doc


def _doc_report(doc, manifest=None, **kw):
    manifest = manifest or make_manifest()
    entries = [
        ManifestEntry(path="synthetic/syn-A/manifest.json", manifest=manifest)
    ]
    return report_from_result_dict(
        doc, entries, repro_command=REPRO,
        source_path="results/hand-doc.benchmark-result.json", **kw,
    )


# --------------------------------------------------------------------------
# mandatory sections: presence, order, refusal when absent
# --------------------------------------------------------------------------


class TestMandatorySections:
    HEADERS = [
        "# Benchmark report — rep-test",
        "## Hypothesis under test",
        "## Interpretation rules — what may and may not be concluded",
        "## Run manifest(s) — full, embedded",
        "## Results",
        "### Goodput @ SLO",
        "### Saturation knee",
        "### Cost",
        "## Validity block (mandatory)",
        "### Threats to validity (mandatory)",
        "### Unexplained anomalies (mandatory — never silently empty)",
        "## Reproduction — one command",
        "## Provenance links",
    ]

    def test_all_sections_present_in_fixed_order(self):
        md = _report()
        positions = [md.index(h) for h in self.HEADERS]  # ValueError if absent
        assert positions == sorted(positions)

    def test_hypothesis_displayed_prominently_before_any_number(self):
        md = _report()
        assert md.index("synthetic known-answer test data") < md.index("## Results")

    def test_full_manifest_embedded_as_json(self):
        md = _report()
        assert '"run_id": "syn-A"' in md
        assert '"warm_up"' in md
        assert '"hypothesis"' in md

    def test_comparability_rule_verbatim(self):
        md = _report()
        assert COMPARABILITY_RULE_VERBATIM in md

    def test_comparability_rule_matches_pinned_bundle(self, bundle):
        # drift guard: the embedded copy must still be verbatim in the
        # pinned bundle's compatibility policy
        check_comparability_verbatim(bundle)

    def test_repro_command_and_pins_present(self):
        md = _report()
        assert REPRO in md
        assert "Pinned versions:" in md

    def test_missing_hypothesis_refused(self):
        manifest = make_manifest()
        del manifest["hypothesis"]
        run = make_run(_ok_events(), manifest=manifest)
        with pytest.raises(ReportInputError, match="hypothesis"):
            _report(run=run)

    def test_empty_repro_command_refused(self):
        run, res = _result()
        with pytest.raises(ReportInputError, match="command"):
            report_from_analysis(res, _entries(run), repro_command="  ")

    def test_result_dict_without_validity_block_refused(self):
        doc = _minimal_doc()
        del doc["validity"]
        with pytest.raises(ReportInputError, match="validity"):
            _doc_report(doc)

    def test_result_dict_with_incomplete_validity_refused(self):
        doc = _minimal_doc()
        del doc["validity"]["unexplained_anomalies"]
        with pytest.raises(ReportInputError, match="unexplained_anomalies"):
            _doc_report(doc)

    def test_warm_up_handling_inconsistent_with_manifest_refused(self):
        doc = _minimal_doc()
        doc["validity"]["warm_up_handling"] = "50 requests dropped somehow"
        with pytest.raises(ReportInputError, match="warm-up policy"):
            _doc_report(doc)


# --------------------------------------------------------------------------
# withheld latency: the report must show WHY, never a blank table
# --------------------------------------------------------------------------


class TestWithheldLatency:
    def _gated_events(self):
        evs = _ok_events(18)
        for i in range(18, 20):  # 10% timeouts: error+shed 0.10 > 0.05 gate
            evs.append(
                make_event(i, sched=i * 0.5, status="error", ttft=None,
                           e2e=30.0, output_tokens=0)
            )
        return evs

    def test_gate_tripped_renders_reason_not_blank_table(self):
        md = _report(events=self._gated_events())
        assert "WITHHELD (error-shed-gate)" in md
        assert "exceeds the declared gate threshold" in md
        assert "### Latency — pooled percentiles" not in md
        sec = _section(md, "### Latency percentiles — WITHHELD")
        assert "error rate (measured window)" in sec
        assert "0.1000" in sec
        assert "declared gate threshold" in sec
        # the run is still presented as valid, with the report as the
        # publishable artifact
        assert "remains VALID" in sec
        assert "THIS REPORT is the publishable artifact" in sec

    def test_no_samples_withholding_renders_kind_and_reason(self):
        evs = [
            make_event(i, sched=i * 0.5, status="canceled", ttft=None,
                       e2e=0.05, output_tokens=0)
            for i in range(10)
        ]
        md = _report(events=evs)
        assert "WITHHELD (no-samples)" in md
        assert "zero pooled samples" in md

    def test_goodput_still_rendered_with_rates_when_latency_withheld(self):
        md = _report(events=self._gated_events())
        sec = _section(md, "### Goodput @ SLO")
        assert "shed rate (adjacent by rule)" in sec
        assert "stall rate (adjacent by rule)" in sec


# --------------------------------------------------------------------------
# goodput: shed + stall structurally adjacent in the output
# --------------------------------------------------------------------------


class TestGoodputAdjacency:
    def test_ratio_shed_stall_in_one_table(self):
        md = _report()
        sec = _section(md, "### Goodput @ SLO")
        assert "goodput ratio (meeting / ALL offered" in sec
        assert "**shed rate (adjacent by rule)**" in sec
        assert "**stall rate (adjacent by rule)**" in sec
        # same markdown table: no blank line between the three rows
        rows = [l for l in sec.splitlines() if l.startswith("|")]
        joined = "\n".join(rows)
        assert "goodput ratio" in joined
        assert "shed rate" in joined and "stall rate" in joined

    def test_goodput_missing_shed_rate_refused(self):
        doc = _minimal_doc()
        del doc["goodput"]["shed_rate"]
        with pytest.raises(ReportInputError, match="shed_rate"):
            _doc_report(doc)

    def test_goodput_missing_stall_rate_refused(self):
        doc = _minimal_doc()
        del doc["goodput"]["stall_rate"]
        with pytest.raises(ReportInputError, match="stall_rate"):
            _doc_report(doc)

    def test_goodput_missing_slo_ref_refused(self):
        doc = _minimal_doc()
        doc["goodput"]["slo_ref"] = {}
        with pytest.raises(ReportInputError, match="slo_id"):
            _doc_report(doc)


# --------------------------------------------------------------------------
# anomalies: never silently empty
# --------------------------------------------------------------------------


class TestAnomaliesNeverSilent:
    def test_empty_anomalies_states_none_observed_with_checks_run(self):
        md = _report()
        sec = _section(md, "### Unexplained anomalies")
        assert "**None observed.**" in sec
        assert "checks that were run" in sec
        checks = [l for l in sec.splitlines() if l.startswith("- ")]
        assert len(checks) >= 5
        # the gate check carries the run set's actual numbers
        assert any("declared error/shed gate" in c for c in checks)
        assert any("0.05" in c for c in checks)

    def test_provided_anomalies_are_listed_not_none_observed(self):
        md = _report(anomalies=("p99 spike at minute 3, cause not found",))
        sec = _section(md, "### Unexplained anomalies")
        assert "p99 spike at minute 3" in sec
        assert "None observed" not in sec

    def test_empty_anomalies_with_no_checks_is_refused(self):
        # structural guard on the renderer itself: even a hand-built model
        # cannot claim "none observed" without naming the checks
        run, res = _result()
        captured = {}

        import inferbench_analysis.report as report_mod

        orig = report_mod._render

        def capture(model):
            captured["m"] = model
            return orig(model)

        report_mod._render = capture
        try:
            report_from_analysis(res, _entries(run), repro_command=REPRO)
        finally:
            report_mod._render = orig
        stripped = dataclasses.replace(captured["m"], anomaly_checks=())
        with pytest.raises(ReportInputError, match="none observed"):
            _render(stripped)


# --------------------------------------------------------------------------
# cost: null never renders without WHY
# --------------------------------------------------------------------------


class TestCostNull:
    def test_analysis_mode_cost_null_says_why(self):
        md = _report()
        sec = _section(md, "### Cost")
        assert "`cost: null` — **why:**" in sec
        assert "no cost profile applies" in sec

    def test_file_mode_cost_null_reason_recovered_from_threats(self):
        md = _doc_report(_minimal_doc())
        sec = _section(md, "### Cost")
        assert "`cost: null` — **why:**" in sec
        assert "no cost profile applies" in sec

    def test_file_mode_cost_null_without_recorded_reason_is_called_out(self):
        doc = _minimal_doc()
        doc["validity"]["threats_to_validity"] = []
        md = _doc_report(doc)
        sec = _section(md, "### Cost")
        assert "records no reason" in sec
        assert "validity gap" in sec


# --------------------------------------------------------------------------
# arrival process / closed-loop flagging
# --------------------------------------------------------------------------


class TestArrivalFlagging:
    def test_closed_loop_flagged_loudly(self):
        md = _report(workload=CLOSED_LOOP_WL)
        assert "FLAG: CLOSED-LOOP ARRIVAL CONTRIBUTES TO THIS REPORT" in md
        assert "**FLAGGED: closed-loop**" in md
        assert "understate queueing delay" in md or "hiding queueing delay" in md

    def test_open_loop_described_not_flagged(self):
        md = _report(workload=OPEN_LOOP_WL)
        assert "open-loop Poisson, rate 2 req/s" in md
        assert "FLAG: CLOSED-LOOP" not in md
        assert "no contributing workload declares closed-loop arrival" in md

    def test_missing_workload_file_does_not_imply_open_loop(self):
        md = _report()  # no workload supplied
        assert "arrival process NOT inspectable" in md
        assert "Open-loop arrivals are NOT implied" in md


# --------------------------------------------------------------------------
# latency table content rules
# --------------------------------------------------------------------------


class TestLatencyRendering:
    def test_tables_are_full_percentiles_never_mean_only(self):
        md = _report()
        sec = _section(md, "### Latency — pooled percentiles")
        header = "| signal | n | p50 | p90 | p95 | p99 | p999 | max | mean |"
        assert header in sec
        assert "pooled-raw-events" in sec
        assert "never a substitute" in sec  # the anti-mean-only note

    def test_bootstrap_cis_rendered_when_computed(self):
        md = _report(bootstrap=BootstrapParams(resamples=50, seed=7))
        sec = _section(md, "### Latency — pooled percentiles")
        assert "Bootstrap 95% confidence intervals" in sec
        assert "| `ttft_seconds` | p50 | [" in sec

    def test_validity_block_carries_gate_and_pooling_statement(self):
        md = _report()
        sec = _section(md, "## Validity block (mandatory)")
        assert "Declared error/shed gate" in sec
        assert "pooled raw events" in sec
        assert "Warm-up handling" in sec


# --------------------------------------------------------------------------
# end-to-end through the emitted contract file (needs the pinned bundle)
# --------------------------------------------------------------------------


class TestResultFileRoundTrip:
    def test_report_from_emitted_result_dict(self, bundle):
        run, res = _result()
        doc = res.to_benchmark_result_dict(bundle)
        entries = [
            ManifestEntry(path=run.manifest_path, manifest=run.manifest)
        ]
        md = report_from_result_dict(
            doc, entries, repro_command=REPRO,
            source_path="rep-test.benchmark-result.json", bundle=bundle,
        )
        assert "# Benchmark report — rep-test" in md
        assert "### Threats to validity (mandatory)" in md
        assert "**shed rate (adjacent by rule)**" in md
        assert COMPARABILITY_RULE_VERBATIM in md

    def test_cli_report_from_result_file(self, bundle, tmp_path):
        import os

        from inferbench_analysis.cli import main

        run, res = _result()
        doc = res.to_benchmark_result_dict(bundle)
        # place the manifest where the result's links point
        mdir = tmp_path / "synthetic" / "syn-A"
        mdir.mkdir(parents=True)
        (mdir / "manifest.json").write_text(json.dumps(run.manifest))
        rfile = tmp_path / "rep-test.benchmark-result.json"
        rfile.write_text(json.dumps(doc))
        outdir = tmp_path / "out"
        rc = main([
            "report",
            "--bundle", os.environ["CONTRACTS_BUNDLE"],
            "--result", str(rfile),
            "--root", str(tmp_path),
            "--out", str(outdir),
        ])
        assert rc == 0
        md = (outdir / "rep-test.report.md").read_text()
        assert "## Validity block (mandatory)" in md
        assert "### Unexplained anomalies (mandatory — never silently empty)" in md

    def test_cli_report_requires_exactly_one_source(self, bundle):
        import os

        from inferbench_analysis.cli import main

        with pytest.raises(SystemExit):
            main(["report", "--bundle", os.environ["CONTRACTS_BUNDLE"]])
