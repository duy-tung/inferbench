"""Cost computation from provenanced cost-profile files."""

import pytest

from inferbench_analysis import Cost, CostError, CostInputs, CostUnavailable, compute_cost


def profile(rates=None):
    prov = {
        "basis": "source-reported",
        "as_of": "2026-07-01",
        "source": "vendor pricing page",
    }
    return {
        "profile_id": "test-cloud",
        "version": "1.0.0",
        "currency": "USD",
        "rates": rates
        or [
            {
                "hardware_profile_ref": {"id": "a100-1x"},
                "pricing_model": "on-demand",
                "usd_per_hour": {"value": 3.6, "provenance": prov},
            }
        ],
    }


def inputs(**kw):
    kw.setdefault("profile", profile())
    kw.setdefault("hardware_profile_id", "a100-1x")
    return CostInputs(**kw)


class TestKnownAnswers:
    def test_per_request_and_per_million_tokens(self):
        # 3.6 USD/h for exactly one hour = 3.6 USD window cost
        c = compute_cost(
            inputs(),
            window_seconds=3600.0,
            ok_count=100,
            total_tokens=2_000_000,
            output_tokens=1_000_000,
        )
        assert isinstance(c, Cost)
        assert c.per_successful_request_usd == pytest.approx(0.036)
        assert c.per_million_tokens_usd == pytest.approx(1.8)
        assert c.per_million_output_tokens_usd == pytest.approx(3.6)
        assert c.usd_per_hour == 3.6
        assert c.rate_provenance["basis"] == "source-reported"
        assert c.profile_id == "test-cloud"


class TestProvenanceHonesty:
    def test_no_profile_means_null_with_reason(self):
        c = compute_cost(
            None, window_seconds=60, ok_count=10, total_tokens=100, output_tokens=50
        )
        assert isinstance(c, CostUnavailable)
        assert "null" in c.reason

    def test_zero_successes_means_null(self):
        c = compute_cost(
            inputs(), window_seconds=60, ok_count=0, total_tokens=100, output_tokens=0
        )
        assert isinstance(c, CostUnavailable)
        assert "0" in c.reason and "successful" in c.reason

    def test_zero_tokens_means_null(self):
        c = compute_cost(
            inputs(), window_seconds=60, ok_count=5, total_tokens=0, output_tokens=0
        )
        assert isinstance(c, CostUnavailable)


class TestRateSelection:
    def test_unknown_hardware_refused(self):
        with pytest.raises(CostError, match="no rate"):
            compute_cost(
                inputs(hardware_profile_id="h100-8x"),
                window_seconds=60,
                ok_count=1,
                total_tokens=1,
                output_tokens=1,
            )

    def test_ambiguous_rates_refused(self):
        prov = {
            "basis": "source-reported",
            "as_of": "2026-07-01",
            "source": "vendor",
        }
        two = [
            {
                "hardware_profile_ref": {"id": "a100-1x"},
                "pricing_model": "on-demand",
                "usd_per_hour": {"value": 3.6, "provenance": prov},
            },
            {
                "hardware_profile_ref": {"id": "a100-1x"},
                "pricing_model": "spot",
                "usd_per_hour": {"value": 1.2, "provenance": prov},
            },
        ]
        with pytest.raises(CostError, match="disambiguate"):
            compute_cost(
                inputs(profile=profile(two)),
                window_seconds=60,
                ok_count=1,
                total_tokens=1,
                output_tokens=1,
            )
        # disambiguated by pricing model: fine
        c = compute_cost(
            inputs(profile=profile(two), pricing_model="spot"),
            window_seconds=3600,
            ok_count=1,
            total_tokens=1_000_000,
            output_tokens=1,
        )
        assert isinstance(c, Cost)
        assert c.per_million_tokens_usd == pytest.approx(1.2)
