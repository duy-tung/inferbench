package schedule

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/duy-tung/inferbench/internal/workload"
)

func baseWorkload(t *testing.T, seed int64) *workload.Workload {
	t.Helper()
	w, err := workload.Parse([]byte(`{
		"name": "test-poisson",
		"version": "0.1.0",
		"seed": ` + itoa(seed) + `,
		"arrival_process": {"type": "open-loop-poisson", "rate_rps": 50},
		"input_length_distribution": {"type": "normal", "mean": 200, "stddev": 50, "min": 16, "max": 1024},
		"output_length_distribution": {"type": "lognormal", "mu": 4.0, "sigma": 0.5, "min": 8, "max": 256},
		"prefix_sharing": {"ratio": 0},
		"cancellation": {"rate": 0},
		"slow_client": {"fraction": 0},
		"stop": {"request_count": 2000}
	}`))
	if err != nil {
		t.Fatalf("parse workload: %v", err)
	}
	return w
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Same seed => byte-identical schedule (ADR-0001: a first-class contract).
func TestBuildDeterministic(t *testing.T) {
	p1, err := Build(baseWorkload(t, 42))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Build(baseWorkload(t, 42))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p1, p2) {
		t.Fatal("same seed produced different plans")
	}
	if len(p1.Items) != 2000 {
		t.Fatalf("want 2000 items, got %d", len(p1.Items))
	}
	// Prompts must be deterministic too (request-body determinism).
	for _, i := range []int{0, 999, 1999} {
		if p1.Prompt(p1.Items[i]) != p2.Prompt(p2.Items[i]) {
			t.Fatalf("prompt for item %d differs across identical plans", i)
		}
	}
}

func TestBuildSeedChangesSchedule(t *testing.T) {
	p1, _ := Build(baseWorkload(t, 42))
	p2, _ := Build(baseWorkload(t, 43))
	if reflect.DeepEqual(p1.Items, p2.Items) {
		t.Fatal("different seeds produced identical schedules")
	}
}

// Poisson sanity: mean inter-arrival ~= 1/rate.
func TestPoissonMeanGap(t *testing.T) {
	p, err := Build(baseWorkload(t, 7))
	if err != nil {
		t.Fatal(err)
	}
	var sum time.Duration
	for i := 1; i < len(p.Items); i++ {
		sum += p.Items[i].SendOffset - p.Items[i-1].SendOffset
	}
	mean := sum.Seconds() / float64(len(p.Items)-1)
	want := 1.0 / 50.0
	if math.Abs(mean-want)/want > 0.10 {
		t.Fatalf("mean gap %.5fs; want within 10%% of %.5fs", mean, want)
	}
	// Offsets strictly increasing, all parameters in declared bounds.
	for i, it := range p.Items {
		if i > 0 && it.SendOffset <= p.Items[i-1].SendOffset {
			t.Fatalf("offsets not increasing at %d", i)
		}
		if it.InputTokens < 16 || it.InputTokens > 1024 {
			t.Fatalf("input tokens %d outside clamp", it.InputTokens)
		}
		if it.MaxTokens < 8 || it.MaxTokens > 256 {
			t.Fatalf("max tokens %d outside clamp (output length must stay directed)", it.MaxTokens)
		}
	}
}

func TestDurationStop(t *testing.T) {
	w := baseWorkload(t, 11)
	w.Stop.RequestCount = nil
	d := 10.0
	w.Stop.DurationSeconds = &d
	p, err := Build(w)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) == 0 {
		t.Fatal("no arrivals")
	}
	for _, it := range p.Items {
		if it.SendOffset >= 10*time.Second {
			t.Fatalf("arrival %s beyond duration stop", it.SendOffset)
		}
	}
	// ~ rate*duration arrivals expected (500); allow wide slack.
	if n := len(p.Items); n < 350 || n > 650 {
		t.Fatalf("got %d arrivals for 50 rps x 10s", n)
	}
}

func TestPhasedArrivals(t *testing.T) {
	w := baseWorkload(t, 5)
	w.Arrival.RateRPS = nil
	w.Arrival.Phases = []workload.Phase{
		{DurationSeconds: 2, RateRPS: 100},
		{DurationSeconds: 2, RateRPS: 0},
	}
	rep := true
	w.Arrival.RepeatPhases = &rep
	w.Stop.RequestCount = nil
	d := 8.0
	w.Stop.DurationSeconds = &d
	p, err := Build(w)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range p.Items {
		s := it.SendOffset.Seconds()
		inBurst := math.Mod(s, 4) < 2
		if !inBurst {
			t.Fatalf("arrival at %.3fs falls in a rate-0 phase", s)
		}
	}
	// Two burst windows of 2s at 100 rps => ~400.
	if n := len(p.Items); n < 280 || n > 520 {
		t.Fatalf("got %d arrivals for phased schedule", n)
	}
}

func TestPhasesExhaustedWithoutRepeat(t *testing.T) {
	w := baseWorkload(t, 5)
	w.Arrival.RateRPS = nil
	w.Arrival.Phases = []workload.Phase{{DurationSeconds: 1, RateRPS: 50}}
	w.Stop.RequestCount = nil
	d := 100.0
	w.Stop.DurationSeconds = &d
	p, err := Build(w)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range p.Items {
		if it.SendOffset >= time.Second {
			t.Fatalf("arrival %s after phase schedule exhausted", it.SendOffset)
		}
	}
}

// Deferred features refuse with the typed error, never run silently.
func TestDeferredFeaturesRefuse(t *testing.T) {
	cases := map[string]func(w *workload.Workload){
		"prefix": func(w *workload.Workload) {
			r := 0.5
			n := 128
			w.Prefix.Ratio = &r
			w.Prefix.SharedPrefixLengthTokens = &n
		},
		"cancellation": func(w *workload.Workload) {
			r := 0.2
			w.Cancel.Rate = &r
			w.Cancel.Point = &workload.CancelPoint{
				Trigger:      "elapsed-seconds",
				Distribution: constDist(1),
			}
		},
		"slow-client": func(w *workload.Workload) {
			f := 0.1
			bps := 1024
			w.SlowClient.Fraction = &f
			w.SlowClient.ReadBytesPerSecond = &bps
		},
	}
	for name, mutate := range cases {
		w := baseWorkload(t, 1)
		mutate(w)
		if _, err := Build(w); !errors.Is(err, workload.ErrNotImplemented) {
			t.Fatalf("%s: want ErrNotImplemented, got %v", name, err)
		}
	}
}

func constDist(v float64) *workload.Dist {
	return &workload.Dist{Type: "constant", Value: &v}
}

// A repeating phase schedule with no positive-rate phase must refuse
// (previously an infinite loop in Build).
func TestAllZeroRateRepeatingPhasesRefused(t *testing.T) {
	w := baseWorkload(t, 3)
	w.Arrival.RateRPS = nil
	w.Arrival.Phases = []workload.Phase{
		{DurationSeconds: 1, RateRPS: 0},
		{DurationSeconds: 2, RateRPS: 0},
	}
	rep := true
	w.Arrival.RepeatPhases = &rep
	w.Stop.RequestCount = nil
	d := 10.0
	w.Stop.DurationSeconds = &d
	done := make(chan error, 1)
	go func() {
		_, err := Build(w)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("all-zero-rate repeating phases must be refused")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Build hung on all-zero-rate repeating phases")
	}
}
