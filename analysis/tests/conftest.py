"""Shared synthetic-data builders for the known-answer test suite."""

from __future__ import annotations

import os

import pytest

from inferbench_analysis import Bundle, RawEvent, Run
from inferbench_analysis.events import ITLRecord

BASE_TS = 1_800_000_000.0  # arbitrary epoch anchor for synthetic events


def make_event(
    i: int,
    *,
    run_id: str = "syn-A",
    rep: int = 1,
    sched: float | None = None,
    status: str = "ok",
    error_class: str | None = None,
    ttft: float | None = 0.1,
    e2e: float = 0.5,
    itl_series: tuple[float, ...] | None = None,
    itl_summary: dict | None = None,
    max_stall: float | None = None,
    shed: bool = False,
    input_tokens: int = 20,
    output_tokens: int = 10,
    send_slip: float = 0.001,
) -> RawEvent:
    """One synthetic raw event. Timing basis mirrors the contract: e2e is
    end_ts - scheduled_send_ts."""
    sched_ts = BASE_TS + (sched if sched is not None else float(i))
    itl = None
    if itl_series is not None or itl_summary is not None:
        stall = (
            max_stall
            if max_stall is not None
            else (max(itl_series) if itl_series else itl_summary["max_seconds"])
        )
        itl = ITLRecord(
            max_stall_seconds=stall, series_seconds=itl_series, summary=itl_summary
        )
    if status == "ok":
        error_class = None
    elif status == "error" and error_class is None:
        error_class = "upstream_timeout"
    elif status == "shed":
        shed, error_class, ttft = True, error_class or "overloaded", None
    elif status == "canceled":
        error_class = "canceled"
    return RawEvent(
        run_id=run_id,
        repetition=rep,
        request_id=f"{run_id}-r{rep}-{i:06d}",
        scheduled_send_ts=sched_ts,
        send_ts=sched_ts + send_slip,
        end_ts=sched_ts + e2e,
        status=status,
        error_class=error_class,
        ttft_seconds=ttft,
        itl=itl,
        input_tokens=input_tokens,
        output_tokens=output_tokens,
        shed=shed,
        send_slip_seconds=send_slip,
    )


def make_manifest(
    run_id: str = "syn-A",
    *,
    repetitions: int = 1,
    warm_up: dict | None = None,
    engine_flags: dict | None = None,
) -> dict:
    return {
        "run_id": run_id,
        "target_topology": "engine-direct",
        "workload_ref": {"name": "synthetic", "version": "1.0.0", "seed": 1},
        "engine": {
            "name": "synthetic",
            "version": "0",
            "commit": None,
            "flags": engine_flags or {},
        },
        "model": {"checkpoint": "syn", "revision": "syn", "tokenizer": "syn"},
        "hardware": {
            "gpu_model": None,
            "gpu_count": 0,
            "vram_gb": None,
            "driver_version": None,
            "cuda_version": None,
            "instance_type": "test",
        },
        "client": {"location": "in-process", "rtt_ms": None},
        "warm_up": warm_up or {"policy": "none"},
        "repetitions": repetitions,
        "hypothesis": "synthetic known-answer test data",
    }


def make_run(events, manifest=None, run_id: str = "syn-A") -> Run:
    return Run(
        manifest=manifest or make_manifest(run_id),
        events=tuple(events),
        manifest_path=f"synthetic/{run_id}/manifest.json",
        events_path=f"synthetic/{run_id}/events.jsonl",
    )


def make_slo(
    *,
    ttft_max: float = 0.3,
    e2e_max: float = 2.0,
    stall_max: float | None = 0.2,
    extra_objectives: list[dict] | None = None,
) -> dict:
    prov = {
        "basis": "measured",
        "as_of": "2026-07-10",
        "source": "synthetic known-answer test construction",
    }
    objectives = [
        {
            "signal": "ttft_seconds",
            "statistic": "max",
            "comparator": "<=",
            "threshold": ttft_max,
            "unit": "seconds",
            "provenance": prov,
        },
        {
            "signal": "e2e_duration_seconds",
            "statistic": "max",
            "comparator": "<=",
            "threshold": e2e_max,
            "unit": "seconds",
            "provenance": prov,
        },
    ]
    if stall_max is not None:
        objectives.append(
            {
                "signal": "max_stall_seconds",
                "statistic": "max",
                "comparator": "<=",
                "threshold": stall_max,
                "unit": "seconds",
                "provenance": prov,
            }
        )
    objectives.extend(extra_objectives or [])
    return {
        "slo_id": "syn-slo",
        "version": "1.0.0",
        "scope": "model-serving",
        "objectives": objectives,
    }


@pytest.fixture(scope="session")
def bundle() -> Bundle:
    """The pinned serving-contracts bundle; schema-dependent tests skip
    loudly when it is not configured (CI must always configure it)."""
    root = os.environ.get("CONTRACTS_BUNDLE")
    if not root:
        pytest.skip("CONTRACTS_BUNDLE not set — schema-validation tests need the pinned bundle")
    return Bundle(root)
