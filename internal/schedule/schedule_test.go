package schedule

import (
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

// withCancel adds an elapsed-seconds cancellation profile.
func withCancel(w *workload.Workload, rate float64, trigger string, dist *workload.Dist) {
	w.Cancel.Rate = &rate
	w.Cancel.Point = &workload.CancelPoint{Trigger: trigger, Distribution: dist}
}

// Cancellation planning (IB-T004): assignment and points are seeded and
// deterministic; the realized assignment rate tracks the profile; the
// sampled points respect the distribution bounds.
func TestCancellationPlanning(t *testing.T) {
	min, max := 0.2, 3.0
	uni := &workload.Dist{Type: "uniform", Min: &min, Max: &max}

	w := baseWorkload(t, 21)
	withCancel(w, 0.5, workload.CancelTriggerElapsed, uni)
	p1, err := Build(w)
	if err != nil {
		t.Fatal(err)
	}
	w2 := baseWorkload(t, 21)
	withCancel(w2, 0.5, workload.CancelTriggerElapsed, uni)
	p2, err := Build(w2)
	if err != nil {
		t.Fatal(err)
	}

	planned := 0
	for i, it := range p1.Items {
		if p2.Items[i] != it {
			t.Fatalf("cancel planning not deterministic at item %d", i)
		}
		switch it.CancelTrigger {
		case "":
		case workload.CancelTriggerElapsed:
			planned++
			if it.CancelAfterSeconds < min || it.CancelAfterSeconds > max {
				t.Fatalf("item %d cancel point %.3fs outside declared [%.1f, %.1f]",
					i, it.CancelAfterSeconds, min, max)
			}
		default:
			t.Fatalf("item %d unexpected trigger %q", i, it.CancelTrigger)
		}
	}
	// 2000 items at rate 0.5: realized assignment within ±10% of intent.
	if frac := float64(planned) / float64(len(p1.Items)); frac < 0.4 || frac > 0.6 {
		t.Fatalf("planned cancel fraction %.3f, want ~0.5", frac)
	}

	// Token trigger: sampled values round to integers >= 0.
	w3 := baseWorkload(t, 22)
	five := 5.0
	withCancel(w3, 0.3, workload.CancelTriggerTokens, &workload.Dist{Type: "constant", Value: &five})
	p3, err := Build(w3)
	if err != nil {
		t.Fatal(err)
	}
	tokenPlanned := 0
	for _, it := range p3.Items {
		if it.CancelTrigger == workload.CancelTriggerTokens {
			tokenPlanned++
			if it.CancelAfterTokens != 5 {
				t.Fatalf("token cancel point %d, want 5", it.CancelAfterTokens)
			}
		}
	}
	if tokenPlanned == 0 {
		t.Fatal("no token-trigger cancels planned at rate 0.3")
	}
}

// Slow-client planning (IB-T004): seeded assignment at the declared
// fraction, carrying the read-rate and initial-delay parameters.
func TestSlowClientPlanning(t *testing.T) {
	w := baseWorkload(t, 31)
	f := 0.25
	bps := 1024
	delay := 0.5
	w.SlowClient.Fraction = &f
	w.SlowClient.ReadBytesPerSecond = &bps
	w.SlowClient.InitialReadDelaySeconds = &delay
	p, err := Build(w)
	if err != nil {
		t.Fatal(err)
	}
	slow := 0
	for _, it := range p.Items {
		if it.SlowReadBytesPerSec == 0 {
			continue
		}
		slow++
		if it.SlowReadBytesPerSec != 1024 || it.SlowInitialDelay != 500*time.Millisecond {
			t.Fatalf("slow item carries wrong profile: %+v", it)
		}
	}
	if frac := float64(slow) / float64(len(p.Items)); frac < 0.18 || frac > 0.32 {
		t.Fatalf("slow fraction %.3f, want ~0.25", frac)
	}
}

// Prefix-sharing planning (IB-T004): sharing items share a byte-identical
// prefix within their group, carry unique suffixes, and the realized ratio
// tracks the profile.
func TestPrefixSharingPlanning(t *testing.T) {
	w := baseWorkload(t, 41)
	ratio := 0.8
	prefixTokens := 128
	group := 16
	w.Prefix.Ratio = &ratio
	w.Prefix.SharedPrefixLengthTokens = &prefixTokens
	w.Prefix.GroupSize = &group
	// Input floor above the prefix length so every sharing prompt has a
	// unique suffix (mirrors the canonical shared-prefix design rule).
	fmin, fmax := 200.0, 400.0
	w.InputLen = &workload.Dist{Type: "uniform", Min: &fmin, Max: &fmax}

	p, err := Build(w)
	if err != nil {
		t.Fatal(err)
	}

	prompts := map[int][]string{} // group -> prompts
	sharing := 0
	for _, it := range p.Items {
		if it.PrefixGroup == 0 {
			continue
		}
		sharing++
		prompts[it.PrefixGroup] = append(prompts[it.PrefixGroup], p.Prompt(it))
	}
	if frac := float64(sharing) / float64(len(p.Items)); frac < 0.72 || frac > 0.88 {
		t.Fatalf("sharing fraction %.3f, want ~0.8", frac)
	}

	prefixBytes := prefixTokens*4 - 28 // fillWords target for the prefix
	distinctPrefixes := map[string]bool{}
	for g, ps := range prompts {
		if len(ps) > group {
			t.Fatalf("group %d has %d members, group_size %d", g, len(ps), group)
		}
		prefix := ps[0][:prefixBytes]
		distinctPrefixes[prefix] = true
		seen := map[string]bool{}
		for _, prompt := range ps {
			if prompt[:prefixBytes] != prefix {
				t.Fatalf("group %d prompts do not share a byte-identical prefix", g)
			}
			if seen[prompt] {
				t.Fatalf("group %d contains fully duplicated prompts (suffix must be unique)", g)
			}
			seen[prompt] = true
		}
	}
	// Sequential fill: number of groups ~= sharing / group_size, each with
	// its own distinct prefix text.
	wantGroups := (sharing + group - 1) / group
	if len(prompts) != wantGroups || len(distinctPrefixes) != wantGroups {
		t.Fatalf("got %d groups / %d distinct prefixes, want %d", len(prompts), len(distinctPrefixes), wantGroups)
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

// Fingerprint is the replay-determinism primitive (IB-T008): same seed =>
// same fingerprint; different seed => different fingerprint.
func TestFingerprintDeterministic(t *testing.T) {
	p1, err := Build(baseWorkload(t, 7))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Build(baseWorkload(t, 7))
	if err != nil {
		t.Fatal(err)
	}
	f1, f2 := Fingerprint(p1), Fingerprint(p2)
	if f1 == "" {
		t.Fatal("fingerprint must not be empty")
	}
	if f1 != f2 {
		t.Fatalf("same seed produced different fingerprints: %s vs %s", f1, f2)
	}
}

func TestFingerprintDiffersOnSeed(t *testing.T) {
	p1, err := Build(baseWorkload(t, 7))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Build(baseWorkload(t, 8))
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(p1) == Fingerprint(p2) {
		t.Fatal("different seeds produced identical fingerprints")
	}
}

// A rate-only variant of the same seed must reproduce identical per-item
// content (input/output tokens etc): only send offsets differ, so the
// fingerprint differs, but the item-content hash (built the same way minus
// SendOffset) must match — this is what makes a rate sweep single-variable
// structurally, not just by convention (internal/sweep.DeriveRateWorkload).
func TestFingerprintRateChangeOnlyAffectsOffsets(t *testing.T) {
	w1 := baseWorkload(t, 55)
	w2 := baseWorkload(t, 55)
	fast := 999.0
	w2.Arrival.RateRPS = &fast

	p1, err := Build(w1)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Build(w2)
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Items) != len(p2.Items) {
		t.Fatalf("item counts differ: %d vs %d", len(p1.Items), len(p2.Items))
	}
	for i := range p1.Items {
		a, b := p1.Items[i], p2.Items[i]
		if a.InputTokens != b.InputTokens || a.MaxTokens != b.MaxTokens ||
			a.CancelTrigger != b.CancelTrigger || a.PrefixGroup != b.PrefixGroup {
			t.Fatalf("item %d content diverged across a rate-only change: %+v vs %+v", i, a, b)
		}
	}
	if Fingerprint(p1) == Fingerprint(p2) {
		t.Fatal("a genuine rate change must change the fingerprint (send offsets differ)")
	}
}
