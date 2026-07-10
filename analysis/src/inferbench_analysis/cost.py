"""Cost per successful request / per 1M tokens from cost-profile files
(cost-profile.schema.json; provenance-aware).

Provenance rules (structural):

* No cost profile supplied => :class:`CostUnavailable` with the reason —
  never a fabricated default rate. result.py maps this to ``cost: null``
  plus the mandatory threats_to_validity note.
* The profile instance is schema-validated by the caller (loader/CLI);
  rate selection here refuses ambiguity instead of guessing.
* The applied hourly rate's own provenance (basis/as_of/source) is carried
  into the Cost object so reports can print it next to the numbers.

Definitions:

* window cost      = usd_per_hour × measured_window_seconds / 3600
  (the measured window is the post-warm-up wall-clock span the throughput
  figures are computed over; warm-up seconds are honestly NOT billed into
  per-request cost, matching the statistics window)
* per successful request = window cost / ok_count
* per 1M tokens          = window cost / ((input+output tokens) / 1e6)
* per 1M output tokens   = window cost / (output tokens / 1e6)

Zero successful requests or zero tokens make the respective figure
undefined; the whole cost block becomes CostUnavailable (the contract
requires both figures together) with the reason preserved.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Mapping

from .errors import CostError


@dataclass(frozen=True)
class CostInputs:
    profile: Mapping  # schema-validated cost-profile instance
    hardware_profile_id: str
    pricing_model: str | None = None  # on-demand | reserved | spot
    region: str | None = None


@dataclass(frozen=True)
class Cost:
    profile_id: str
    profile_version: str
    per_successful_request_usd: float
    per_million_tokens_usd: float
    per_million_output_tokens_usd: float | None
    usd_per_hour: float
    rate_provenance: Mapping  # {basis, as_of, source?, notes?} of the applied rate


@dataclass(frozen=True)
class CostUnavailable:
    """cost: null, with the honest reason for the validity block."""

    reason: str


def _select_rate(inputs: CostInputs) -> Mapping:
    matches = []
    for rate in inputs.profile["rates"]:
        if rate["hardware_profile_ref"]["id"] != inputs.hardware_profile_id:
            continue
        if inputs.pricing_model and rate["pricing_model"] != inputs.pricing_model:
            continue
        if inputs.region and rate.get("region") != inputs.region:
            continue
        matches.append(rate)
    if not matches:
        raise CostError(
            f"cost profile '{inputs.profile['profile_id']}' has no rate for "
            f"hardware_profile_ref.id='{inputs.hardware_profile_id}'"
            + (f", pricing_model='{inputs.pricing_model}'" if inputs.pricing_model else "")
            + (f", region='{inputs.region}'" if inputs.region else "")
        )
    if len(matches) > 1:
        raise CostError(
            f"cost profile '{inputs.profile['profile_id']}' has {len(matches)} "
            f"rates matching hardware_profile_ref.id='{inputs.hardware_profile_id}'"
            " — disambiguate with pricing_model/region instead of letting the "
            "analyzer pick one"
        )
    return matches[0]


def compute_cost(
    inputs: CostInputs | None,
    *,
    window_seconds: float,
    ok_count: int,
    total_tokens: int,
    output_tokens: int,
) -> Cost | CostUnavailable:
    if inputs is None:
        return CostUnavailable(
            reason="no cost profile applies to this run set — cost is null "
            "(cost figures are only computed from a declared, provenanced "
            "cost-profile file, never from assumed rates)"
        )
    if window_seconds <= 0:
        raise CostError(f"non-positive measured window ({window_seconds}s)")
    rate = _select_rate(inputs)
    usd_per_hour = float(rate["usd_per_hour"]["value"])
    window_cost = usd_per_hour * window_seconds / 3600.0

    if ok_count <= 0:
        return CostUnavailable(
            reason="cost profile supplied but the measured window has 0 "
            "successful requests — cost per successful request is undefined; "
            "cost is null"
        )
    if total_tokens <= 0:
        return CostUnavailable(
            reason="cost profile supplied but the measured window carries 0 "
            "tokens — cost per 1M tokens is undefined; cost is null"
        )
    return Cost(
        profile_id=inputs.profile["profile_id"],
        profile_version=inputs.profile["version"],
        per_successful_request_usd=window_cost / ok_count,
        per_million_tokens_usd=window_cost / (total_tokens / 1_000_000.0),
        per_million_output_tokens_usd=(
            window_cost / (output_tokens / 1_000_000.0) if output_tokens > 0 else None
        ),
        usd_per_hour=usd_per_hour,
        rate_provenance=rate["usd_per_hour"]["provenance"],
    )
