"""Warm-up exclusion (experiments.md rule 2, ADR-0002 §3).

The policy comes from the run manifest — the analysis cannot be handed a
different policy than the run declared. Exclusion is applied per repetition
(each repetition warms the target independently), ordered by
``scheduled_send_ts`` (the arrival-process order, independent of responses).
Every excluded event is counted; the counts feed the validity block.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Mapping, Sequence

from .errors import WarmupError
from .events import RawEvent, Run


@dataclass(frozen=True)
class WarmupReport:
    policy: str  # discard-requests | discard-duration | none
    value: float | None
    excluded_total: int
    kept_total: int
    # "(run_id, repetition)" -> excluded count
    excluded_per_repetition: Mapping[str, int]

    def handling_statement(self) -> str:
        """The validity-block warm_up_handling string."""
        if self.policy == "none":
            return (
                "manifest warm-up policy 'none': no warm-up exclusion applied; "
                f"0 of {self.kept_total} events excluded"
            )
        unit = "requests" if self.policy == "discard-requests" else "seconds"
        per_rep = "; ".join(
            f"{k}: {v} excluded" for k, v in self.excluded_per_repetition.items()
        )
        return (
            f"manifest warm-up policy '{self.policy}' ({self.value:g} {unit} per "
            f"repetition, ordered by scheduled_send_ts): {self.excluded_total} "
            f"events excluded, {self.kept_total} kept ({per_rep})"
        )


def apply_warmup(runs: Sequence[Run]) -> tuple[list[RawEvent], WarmupReport]:
    """Apply the manifest-declared warm-up policy to every repetition of every
    run. Returns the kept (measured-window) events and the exclusion report.
    """
    if not runs:
        raise WarmupError("no runs supplied")
    policy_block = runs[0].manifest.get("warm_up")
    if not policy_block or "policy" not in policy_block:
        # benchmark-run schema already requires this; double refusal for
        # events that arrive through non-file paths.
        raise WarmupError(
            "run manifest declares no warm-up policy — events without a "
            "declared policy are refused (ADR-0002 §3)"
        )
    policy: str = policy_block["policy"]
    value = policy_block.get("value")
    if policy in ("discard-requests", "discard-duration") and value is None:
        raise WarmupError(f"warm-up policy '{policy}' requires a value")
    if policy not in ("discard-requests", "discard-duration", "none"):
        raise WarmupError(f"unknown warm-up policy '{policy}'")

    kept: list[RawEvent] = []
    excluded_per_rep: dict[str, int] = {}
    excluded_total = 0

    for run in runs:
        by_rep: dict[int, list[RawEvent]] = {}
        for ev in run.events:
            by_rep.setdefault(ev.repetition, []).append(ev)
        for rep, evs in sorted(by_rep.items()):
            evs.sort(key=lambda e: e.scheduled_send_ts)
            if policy == "discard-requests":
                n = int(value)
                cut, rest = evs[:n], evs[n:]
            elif policy == "discard-duration":
                start = evs[0].scheduled_send_ts
                cut = [e for e in evs if e.scheduled_send_ts - start < value]
                rest = [e for e in evs if e.scheduled_send_ts - start >= value]
            else:  # none
                cut, rest = [], evs
            key = f"{run.manifest['run_id']}/rep{rep}"
            excluded_per_rep[key] = len(cut)
            excluded_total += len(cut)
            kept.extend(rest)

    if not kept:
        raise WarmupError(
            f"warm-up policy '{policy}' (value={value!r}) excluded every event "
            "in every repetition — no measured window remains; the run set "
            "cannot produce statistics"
        )
    return kept, WarmupReport(
        policy=policy,
        value=value,
        excluded_total=excluded_total,
        kept_total=len(kept),
        excluded_per_repetition=excluded_per_rep,
    )
