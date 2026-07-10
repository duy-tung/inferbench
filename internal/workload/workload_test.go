package workload

import (
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

const validJSON = `{
	"name": "chat-short",
	"version": "0.1.0",
	"seed": 20260710,
	"description": "test fixture",
	"arrival_process": {"type": "open-loop-poisson", "rate_rps": 8},
	"input_length_distribution": {"type": "normal", "mean": 220, "stddev": 80, "min": 16, "max": 1024},
	"output_length_distribution": {"type": "lognormal", "mu": 5.0, "sigma": 0.6, "min": 8, "max": 512},
	"prefix_sharing": {"ratio": 0},
	"cancellation": {"rate": 0},
	"slow_client": {"fraction": 0},
	"stop": {"duration_seconds": 600},
	"tags": ["interactive"]
}`

func TestParseValid(t *testing.T) {
	w, err := Parse([]byte(validJSON))
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "chat-short" || *w.Seed != 20260710 || *w.Arrival.RateRPS != 8 {
		t.Fatalf("unexpected parse: %+v", w)
	}
	if err := w.CheckRunnable(); err != nil {
		t.Fatalf("chat-short shape must be runnable: %v", err)
	}
}

func mutate(t *testing.T, from, to string) []byte {
	t.Helper()
	if !strings.Contains(validJSON, from) {
		t.Fatalf("fixture does not contain %q", from)
	}
	return []byte(strings.Replace(validJSON, from, to, 1))
}

func TestParseRejections(t *testing.T) {
	cases := map[string][]byte{
		"unknown field": mutate(t, `"description": "test fixture",`, `"description": "x", "bogus": 1,`),
		"bad name":      mutate(t, `"name": "chat-short"`, `"name": "Chat_Short"`),
		"bad version":   mutate(t, `"version": "0.1.0"`, `"version": "v1"`),
		"missing seed":  mutate(t, `"seed": 20260710,`, ``),
		"both stops":    mutate(t, `"stop": {"duration_seconds": 600}`, `"stop": {"duration_seconds": 600, "request_count": 5}`),
		"empty stop":    mutate(t, `"stop": {"duration_seconds": 600}`, `"stop": {}`),
		"rate and phases": mutate(t,
			`{"type": "open-loop-poisson", "rate_rps": 8}`,
			`{"type": "open-loop-poisson", "rate_rps": 8, "phases": [{"duration_seconds": 1, "rate_rps": 2}]}`),
		"prefix ratio no length": mutate(t, `"prefix_sharing": {"ratio": 0}`, `"prefix_sharing": {"ratio": 0.5}`),
		"cancel rate no point":   mutate(t, `"cancellation": {"rate": 0}`, `"cancellation": {"rate": 0.1}`),
		"slow fraction no rate":  mutate(t, `"slow_client": {"fraction": 0}`, `"slow_client": {"fraction": 0.1}`),
		"closed-loop undisclosed": mutate(t,
			`{"type": "open-loop-poisson", "rate_rps": 8}`,
			`{"type": "closed-loop", "concurrency": 4}`),
	}
	for name, data := range cases {
		if _, err := Parse(data); err == nil {
			t.Errorf("%s: accepted invalid workload", name)
		}
	}
}

// The mandatory disclosure flag makes a closed-loop workload parseable —
// but this build refuses to run it (typed, ADR-0003 / deferred mode).
func TestClosedLoopDisclosedParsesButDeferred(t *testing.T) {
	data := mutate(t,
		`{"type": "open-loop-poisson", "rate_rps": 8}`,
		`{"type": "closed-loop", "concurrency": 4, "closed_loop_disclosed": true}`)
	w, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.CheckRunnable(); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got %v", err)
	}
}

func TestDistributionSampling(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	v := 5.0
	c := Dist{Type: "constant", Value: &v}
	for range 10 {
		if c.Sample(rng) != 5 {
			t.Fatal("constant must always return value")
		}
	}

	min, max := 10.0, 20.0
	u := Dist{Type: "uniform", Min: &min, Max: &max}
	for range 1000 {
		s := u.Sample(rng)
		if s < 10 || s > 20 {
			t.Fatalf("uniform sample %f out of range", s)
		}
	}

	mean, sd := 100.0, 10.0
	lo, hi := 90.0, 110.0
	n := Dist{Type: "normal", Mean: &mean, Stddev: &sd, Min: &lo, Max: &hi}
	sum := 0.0
	for range 5000 {
		s := n.Sample(rng)
		if s < 90 || s > 110 {
			t.Fatalf("normal clamp violated: %f", s)
		}
		sum += s
	}
	if got := sum / 5000; math.Abs(got-100) > 2 {
		t.Fatalf("clamped-normal mean %f, want ~100", got)
	}

	w1, w2 := 1.0, 3.0
	a, b := 1.0, 2.0
	m := Dist{Type: "mixture", Components: []MixtureComponent{
		{Weight: &w1, Distribution: &Dist{Type: "constant", Value: &a}},
		{Weight: &w2, Distribution: &Dist{Type: "constant", Value: &b}},
	}}
	twos := 0
	for range 4000 {
		if m.Sample(rng) == 2 {
			twos++
		}
	}
	if twos < 2800 || twos > 3200 {
		t.Fatalf("mixture weight 3:1 gave %d/4000 twos", twos)
	}

	if (&Dist{Type: "empirical", Samples: []float64{7}}).Sample(rng) != 7 {
		t.Fatal("empirical single-sample must return it")
	}
}

func TestSampleTokensFloor(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	z := 0.2
	d := Dist{Type: "constant", Value: &z}
	if got := d.SampleTokens(rng, 1); got != 1 {
		t.Fatalf("token floor: got %d, want 1", got)
	}
}

func TestPromptDeterministicAndSized(t *testing.T) {
	p1 := Prompt(42, 7, 200)
	p2 := Prompt(42, 7, 200)
	if p1 != p2 {
		t.Fatal("prompt not deterministic for same (seed, item, tokens)")
	}
	if Prompt(42, 8, 200) == p1 {
		t.Fatal("different items produced identical prompts")
	}
	if Prompt(43, 7, 200) == p1 {
		t.Fatal("different seeds produced identical prompts")
	}
	// ~4 bytes/token target: stay within a loose factor.
	if len(p1) < 200*2 || len(p1) > 200*8 {
		t.Fatalf("prompt length %d wildly off 200-token target", len(p1))
	}
}
