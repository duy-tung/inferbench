"""Raw-event / manifest loading with refusal semantics.

ADR-0002 §6: the loader refuses manifest-less or schema-invalid events;
statistics never silently drop records. Every event either enters the
analysis or aborts it with a typed reason.

Latency basis (raw-event schema v0.2.0, pin 8d81492): client-side TTFT and
end-to-end latency are measured from ``scheduled_send_ts`` — never from
``send_ts`` — so client-side dispatch/connect/write queueing counts against
latency (coordinated-omission safety). This module derives ``e2e_seconds``
accordingly and exposes no send_ts-based latency.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Mapping, Sequence

from .contracts import Bundle
from .errors import ComparabilityError, LoaderError


def _ts(s: str) -> float:
    return datetime.fromisoformat(s.replace("Z", "+00:00")).timestamp()


@dataclass(frozen=True)
class ITLRecord:
    """Inter-chunk gap record of one stream (series and/or summary + max stall)."""

    max_stall_seconds: float
    series_seconds: tuple[float, ...] | None = None
    summary: Mapping[str, float] | None = None


@dataclass(frozen=True)
class RawEvent:
    """One parsed raw event. Timestamps are epoch seconds."""

    run_id: str
    repetition: int
    request_id: str
    scheduled_send_ts: float
    send_ts: float
    end_ts: float
    status: str  # ok | error | canceled | shed
    error_class: str | None
    ttft_seconds: float | None
    itl: ITLRecord | None
    input_tokens: int
    output_tokens: int
    shed: bool
    send_slip_seconds: float | None = None

    @property
    def e2e_seconds(self) -> float:
        """End-to-end latency from the SCHEDULED send (CO-safe basis)."""
        return self.end_ts - self.scheduled_send_ts


@dataclass(frozen=True)
class Run:
    """One benchmark-run manifest plus its raw events (all repetitions)."""

    manifest: Mapping
    events: tuple[RawEvent, ...]
    manifest_path: str
    events_path: str


def _parse_event(obj: Mapping) -> RawEvent:
    itl = None
    if obj["itl"] is not None:
        itl = ITLRecord(
            max_stall_seconds=obj["itl"]["max_stall_seconds"],
            series_seconds=(
                tuple(obj["itl"]["series_seconds"])
                if "series_seconds" in obj["itl"]
                else None
            ),
            summary=obj["itl"].get("summary"),
        )
    return RawEvent(
        run_id=obj["run_id"],
        repetition=obj["repetition"],
        request_id=obj["request_id"],
        scheduled_send_ts=_ts(obj["scheduled_send_ts"]),
        send_ts=_ts(obj["send_ts"]),
        end_ts=_ts(obj["end_ts"]),
        status=obj["status"],
        error_class=obj["error_class"],
        ttft_seconds=obj["ttft_seconds"],
        itl=itl,
        input_tokens=obj["input_tokens"],
        output_tokens=obj["output_tokens"],
        shed=obj["shed"],
        send_slip_seconds=obj.get("send_slip_seconds"),
    )


def load_run(run_dir: str | Path, bundle: Bundle) -> Run:
    """Load one run directory (manifest.json + events.jsonl), validating both
    against the pinned bundle. Refuses manifest-less or schema-invalid data.
    """
    d = Path(run_dir)
    manifest_path = d / "manifest.json"
    events_path = d / "events.jsonl"
    if not manifest_path.is_file():
        raise LoaderError(
            f"{d}: manifest.json missing — manifest-less events are refused "
            "(experiments.md rule 6)"
        )
    if not events_path.is_file():
        raise LoaderError(f"{d}: events.jsonl missing")

    with open(manifest_path, encoding="utf-8") as f:
        manifest = json.load(f)
    bundle.validate("benchmark-run", manifest, context=str(manifest_path))

    events: list[RawEvent] = []
    with open(events_path, encoding="utf-8") as f:
        for lineno, line in enumerate(f, start=1):
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError as e:
                raise LoaderError(f"{events_path}:{lineno}: not JSON: {e}") from e
            bundle.validate("raw-event", obj, context=f"{events_path}:{lineno}")
            ev = _parse_event(obj)
            if ev.run_id != manifest["run_id"]:
                raise LoaderError(
                    f"{events_path}:{lineno}: run_id '{ev.run_id}' does not match "
                    f"manifest run_id '{manifest['run_id']}'"
                )
            if ev.repetition > manifest["repetitions"]:
                raise LoaderError(
                    f"{events_path}:{lineno}: repetition {ev.repetition} exceeds "
                    f"manifest repetitions {manifest['repetitions']}"
                )
            events.append(ev)
    if not events:
        raise LoaderError(f"{events_path}: no events")
    return Run(
        manifest=manifest,
        events=tuple(events),
        manifest_path=str(manifest_path),
        events_path=str(events_path),
    )


# Manifest fields the benchmark comparability rule keys on (experiments.md
# rule 10 / compatibility-policy §7). Pooling repetitions across manifests
# that differ on ANY of these is refused: it would launder an uncontrolled
# comparison into one percentile table.
COMPARABILITY_KEYS: tuple[str, ...] = (
    "target_topology",
    "workload_ref",
    "engine",
    "model",
    "hardware",
    "gateway",
    "warm_up",
)


def check_poolable(runs: Sequence[Run]) -> None:
    """Refuse run sets whose manifests differ on a comparability key."""
    if not runs:
        raise LoaderError("no runs supplied")
    ref = runs[0].manifest
    for other in runs[1:]:
        for key in COMPARABILITY_KEYS:
            if ref.get(key) != other.manifest.get(key):
                raise ComparabilityError(
                    f"cannot pool '{other.manifest['run_id']}' with "
                    f"'{ref['run_id']}': manifests differ on comparability key "
                    f"'{key}' ({ref.get(key)!r} != {other.manifest.get(key)!r}); "
                    "pooling across uncontrolled variables is forbidden "
                    "(experiments.md rule 10)"
                )
