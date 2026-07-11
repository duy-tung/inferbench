package sweep

import (
	"math"
	"testing"

	"github.com/duy-tung/inferbench/internal/workload"
)

func baseWorkload(t *testing.T) *workload.Workload {
	t.Helper()
	w, err := workload.Parse([]byte(`{
		"name": "sweep-base",
		"version": "1.0.0",
		"seed": 900001,
		"arrival_process": {"type": "open-loop-poisson", "rate_rps": 5},
		"input_length_distribution": {"type": "uniform", "min": 32, "max": 64},
		"output_length_distribution": {"type": "constant", "value": 32},
		"prefix_sharing": {"ratio": 0},
		"cancellation": {"rate": 0},
		"slow_client": {"fraction": 0},
		"stop": {"request_count": 40}
	}`))
	if err != nil {
		t.Fatalf("parse workload: %v", err)
	}
	return w
}

func TestRatePointsSpacingAndCount(t *testing.T) {
	rates, err := RatePoints(100, 0.10, 1.20, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(rates) != 6 {
		t.Fatalf("want 6 points, got %d", len(rates))
	}
	if math.Abs(rates[0]-10) > 1e-9 {
		t.Fatalf("first point should be 10%% of capacity (10), got %g", rates[0])
	}
	if math.Abs(rates[len(rates)-1]-120) > 1e-9 {
		t.Fatalf("last point should be 120%% of capacity (120), got %g", rates[len(rates)-1])
	}
	for i := 1; i < len(rates); i++ {
		if rates[i] <= rates[i-1] {
			t.Fatalf("rates must be strictly ascending: %v", rates)
		}
	}
}

func TestRatePointsRefusesFewerThanSix(t *testing.T) {
	if _, err := RatePoints(100, 0.10, 1.20, 5); err == nil {
		t.Fatal("must refuse < 6 points")
	}
}

func TestRatePointsRefusesBadFractionRange(t *testing.T) {
	if _, err := RatePoints(100, 1.0, 0.5, 6); err == nil {
		t.Fatal("must refuse max <= min")
	}
	if _, err := RatePoints(100, 0, 1.0, 6); err == nil {
		t.Fatal("must refuse min <= 0")
	}
}

func TestEstimateCapacitySaturatedProbe(t *testing.T) {
	// 40 completions in 10s at an offered rate of 100rps: clearly saturated.
	got, err := EstimateCapacity(40, 10.0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-4.0) > 1e-9 {
		t.Fatalf("want capacity 4.0 rps, got %g", got)
	}
}

func TestEstimateCapacityRefusesUnsaturatedProbe(t *testing.T) {
	// Achieved throughput ~= offered rate: the probe never saturated.
	_, err := EstimateCapacity(95, 10.0, 10)
	if err == nil {
		t.Fatal("must refuse an unsaturated probe")
	}
}

func TestEstimateCapacityRefusesZero(t *testing.T) {
	_, err := EstimateCapacity(0, 10.0, 100)
	if err == nil {
		t.Fatal("must refuse zero achieved throughput")
	}
}

func TestDeriveRateWorkloadOnlyChangesRateAndName(t *testing.T) {
	base := baseWorkload(t)
	derived, err := DeriveRateWorkload(base, 12.5, "sweep-base-p3")
	if err != nil {
		t.Fatal(err)
	}
	if derived.Name != "sweep-base-p3" {
		t.Fatalf("name not renamed: %s", derived.Name)
	}
	if *derived.Arrival.RateRPS != 12.5 {
		t.Fatalf("rate not overridden: %g", *derived.Arrival.RateRPS)
	}
	if *derived.Seed != *base.Seed {
		t.Fatalf("seed must be preserved: %d vs %d", *derived.Seed, *base.Seed)
	}
	if err := CheckSingleVariable(base, []*workload.Workload{derived}); err != nil {
		t.Fatalf("derived workload should pass the single-variable check: %v", err)
	}
}

func TestCheckSingleVariableCatchesExtraDifference(t *testing.T) {
	base := baseWorkload(t)
	derived, err := DeriveRateWorkload(base, 12.5, "sweep-base-p3")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an accidental second variable: the max output length also changed.
	derived.OutputLen.Value = float64Ptr(64)
	if err := CheckSingleVariable(base, []*workload.Workload{derived}); err == nil {
		t.Fatal("a second varying field must be refused (combinatorial guard)")
	}
}

func float64Ptr(f float64) *float64 { return &f }
