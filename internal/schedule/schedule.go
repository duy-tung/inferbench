// Package schedule builds the seeded open-loop arrival schedule
// (ADR-0001). The entire send timeline and every per-request parameter are
// computed here, from the workload seed alone, BEFORE any network traffic:
// nothing about a response can influence when the next request is sent.
// This is the coordinated-omission defense.
//
// Same (workload version, seed, arrival process) => identical Plan, which
// is also what makes deterministic replay (IB-T008) possible.
package schedule

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/duy-tung/inferbench/internal/workload"
)

// PRNG stream selectors: each sampling purpose gets an independent
// deterministic stream derived from the workload seed, so e.g. changing the
// input-length distribution never perturbs arrival times.
const (
	streamArrivals = 0x4152525631303030 // "ARRV1000"
	streamInputLen = 0x494e505631303030 // "INPV1000"
	streamOutput   = 0x4f55545631303030 // "OUTV1000"
)

// maxPlannedRequests bounds schedule precomputation (bounded everything).
const maxPlannedRequests = 5_000_000

// Item is one planned request: when to send it and its sampled parameters.
type Item struct {
	// Index is the deterministic workload item index (raw-event
	// workload_item).
	Index int
	// SendOffset is the scheduled send time relative to run start.
	SendOffset time.Duration
	// InputTokens is the sampled target prompt length in tokens (the
	// honest measured count comes from the target's usage payload).
	InputTokens int
	// MaxTokens is the sampled output length cap (drives max_tokens —
	// output length is always directed, never uncontrolled).
	MaxTokens int
}

// Plan is the precomputed, response-independent send schedule.
type Plan struct {
	Seed  int64
	Items []Item
}

// Prompt builds the deterministic synthetic prompt for one planned item.
func (p *Plan) Prompt(it Item) string {
	return workload.Prompt(p.Seed, it.Index, it.InputTokens)
}

// Build computes the full schedule from the workload definition. It is pure:
// no clocks, no network, no I/O.
func Build(w *workload.Workload) (*Plan, error) {
	if err := w.CheckRunnable(); err != nil {
		return nil, err
	}
	seed := *w.Seed
	offsets, err := arrivalOffsets(w, seed)
	if err != nil {
		return nil, err
	}
	inRNG := rand.New(rand.NewPCG(uint64(seed), streamInputLen))
	outRNG := rand.New(rand.NewPCG(uint64(seed), streamOutput))
	items := make([]Item, len(offsets))
	for i, off := range offsets {
		items[i] = Item{
			Index:       i,
			SendOffset:  off,
			InputTokens: w.InputLen.SampleTokens(inRNG, 1),
			MaxTokens:   w.OutputLen.SampleTokens(outRNG, 1),
		}
	}
	return &Plan{Seed: seed, Items: items}, nil
}

// arrivalOffsets samples the Poisson arrival times (exponential
// inter-arrival gaps) against the stop condition.
func arrivalOffsets(w *workload.Workload, seed int64) ([]time.Duration, error) {
	rng := rand.New(rand.NewPCG(uint64(seed), streamArrivals))

	// A repeating phase schedule with no positive-rate phase would cycle
	// forever without ever producing an arrival: refuse it up front.
	if len(w.Arrival.Phases) > 0 && w.Arrival.RepeatPhases != nil && *w.Arrival.RepeatPhases {
		positive := false
		for _, p := range w.Arrival.Phases {
			if p.RateRPS > 0 {
				positive = true
				break
			}
		}
		if !positive {
			return nil, errors.New("schedule: repeat_phases requires at least one phase with rate_rps > 0")
		}
	}

	var wantCount int
	var horizon float64 // seconds; 0 = unbounded
	if w.Stop.RequestCount != nil {
		wantCount = *w.Stop.RequestCount
	}
	if w.Stop.DurationSeconds != nil {
		horizon = *w.Stop.DurationSeconds
	}

	next := nextArrivalFunc(w, rng)
	var offsets []time.Duration
	t := 0.0
	for {
		at, ok := next(t)
		if !ok {
			break // phase schedule exhausted (non-repeating)
		}
		t = at
		if horizon > 0 && t >= horizon {
			break
		}
		offsets = append(offsets, time.Duration(t*float64(time.Second)))
		if wantCount > 0 && len(offsets) == wantCount {
			break
		}
		if len(offsets) > maxPlannedRequests {
			return nil, fmt.Errorf("schedule: more than %d planned requests; refusing", maxPlannedRequests)
		}
	}
	if len(offsets) == 0 {
		return nil, errors.New("schedule: workload produces zero arrivals")
	}
	return offsets, nil
}

// nextArrivalFunc returns a sampler mapping the current absolute time t
// (seconds) to the next arrival's absolute time. For phased schedules the
// exponential draw is restarted at each phase boundary — valid because the
// exponential distribution is memoryless.
func nextArrivalFunc(w *workload.Workload, rng *rand.Rand) func(t float64) (float64, bool) {
	if w.Arrival.RateRPS != nil {
		rate := *w.Arrival.RateRPS
		return func(t float64) (float64, bool) {
			return t + rng.ExpFloat64()/rate, true
		}
	}

	phases := w.Arrival.Phases
	repeat := w.Arrival.RepeatPhases != nil && *w.Arrival.RepeatPhases
	cycle := 0.0
	for _, p := range phases {
		cycle += p.DurationSeconds
	}

	// phaseAt maps absolute time to (rate, absolute end of that phase, ok).
	phaseAt := func(t float64) (float64, float64, bool) {
		base := 0.0
		if repeat {
			base = math.Floor(t/cycle) * cycle
		} else if t >= cycle {
			return 0, 0, false
		}
		rel := t - base
		for _, p := range phases {
			if rel < p.DurationSeconds {
				return p.RateRPS, base + p.DurationSeconds, true
			}
			rel -= p.DurationSeconds
			base += p.DurationSeconds
		}
		// Floating-point edge: t landed exactly on a cycle boundary.
		return phases[0].RateRPS, base + phases[0].DurationSeconds, true
	}

	return func(t float64) (float64, bool) {
		for {
			rate, phaseEnd, ok := phaseAt(t)
			if !ok {
				return 0, false
			}
			if rate <= 0 {
				t = phaseEnd
				continue
			}
			cand := t + rng.ExpFloat64()/rate
			if cand < phaseEnd {
				return cand, true
			}
			// Crossed a phase boundary: discard and resample from the
			// boundary (memorylessness keeps the process exact).
			t = phaseEnd
		}
	}
}
