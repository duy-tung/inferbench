"""Loader refusal semantics against the pinned bundle (ADR-0002 §6)."""

import json
from pathlib import Path

import pytest

from inferbench_analysis import LoaderError, load_run

REPO_ROOT = Path(__file__).resolve().parents[2]
SMOKE_A = REPO_ROOT / "docs" / "evidence" / "ib-t002" / "smoke-A"


def _write_run(tmp_path, manifest: dict | None, event_lines: list[str]):
    d = tmp_path / "run"
    d.mkdir()
    if manifest is not None:
        (d / "manifest.json").write_text(json.dumps(manifest))
    (d / "events.jsonl").write_text("\n".join(event_lines) + "\n")
    return d


@pytest.mark.skipif(not SMOKE_A.is_dir(), reason="evidence run not present")
def test_loads_real_evidence_run(bundle):
    run = load_run(SMOKE_A, bundle)
    assert len(run.events) == 200
    assert run.manifest["run_id"] == "mock-smoke-A"
    assert all(e.run_id == "mock-smoke-A" for e in run.events)
    # CO-safe basis: e2e derives from scheduled_send_ts
    ev = run.events[0]
    assert ev.e2e_seconds == pytest.approx(ev.end_ts - ev.scheduled_send_ts)


def test_manifestless_events_refused(bundle, tmp_path):
    d = _write_run(tmp_path, None, ["{}"])
    with pytest.raises(LoaderError, match="manifest.json missing"):
        load_run(d, bundle)


def test_schema_invalid_event_refused(bundle, tmp_path):
    manifest = json.loads((SMOKE_A / "manifest.json").read_text())
    good = (SMOKE_A / "events.jsonl").read_text().splitlines()[0]
    bad = json.loads(good)
    del bad["scheduled_send_ts"]  # pre-v0.2.0 event: must be refused
    d = _write_run(tmp_path, manifest, [good, json.dumps(bad)])
    with pytest.raises(LoaderError, match="scheduled_send_ts"):
        load_run(d, bundle)


def test_mismatched_run_id_refused(bundle, tmp_path):
    manifest = json.loads((SMOKE_A / "manifest.json").read_text())
    manifest["run_id"] = "some-other-run"
    good = (SMOKE_A / "events.jsonl").read_text().splitlines()[0]
    d = _write_run(tmp_path, manifest, [good])
    with pytest.raises(LoaderError, match="does not match"):
        load_run(d, bundle)
