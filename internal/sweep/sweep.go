// Package sweep provides the pure mechanics of a rate sweep (IB-T008):
// rate-point placement, single-variable workload derivation, and capacity
// estimation math. It has no network/IO dependency — orchestration (running
// the probe and each point via internal/run, writing artifacts) lives in
// cmd/inferbench, mirroring the existing split between internal/schedule
// (pure planning) and internal/run (execution).
//
// Capacity-estimate procedure (documented, experiments.md rule 3): a short
// OVERLOAD PROBE — a single open-loop-poisson run at a rate declared to
// comfortably exceed any plausible capacity, for a bounded duration/count —
// measures achieved sustained throughput (ok completions / elapsed
// seconds). ADR-0003 sanctions exactly this kind of run ("closed-loop mode
// ... narrowly, for throughput-ceiling discovery and capacity estimation
// for sweep-range placement"); this repo uses an OPEN-LOOP overload probe
// instead of implementing closed-loop dispatch, because at the offered
// probe rate the achieved throughput saturates at the same ceiling either
// way (Little's law: ceiling ~= concurrency-cap / mean-service-time) and
// closed-loop execution stays out of IB-T008's scope (recorded in
// docs/implementation-notes.md as a reversible scoping decision). The
// probe's own numbers are NEVER used for a latency/goodput claim — only to
// place the sweep range.
package sweep

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/duy-tung/inferbench/internal/structdiff"
	"github.com/duy-tung/inferbench/internal/workload"
)

// MinPoints mirrors the methodology minimum (experiments.md rule 3;
// analysis/knee.py MIN_SWEEP_POINTS). Kept in sync manually: the Go
// generator and the Python analysis core share no statistics library
// (docs/interfaces.md forbidden edges) — this constant is a mechanics
// constraint (how many points a sweep call produces), not a statistics one.
const MinPoints = 6

// DeclaredVariable is the one field a rate sweep is allowed to vary,
// expressed with the same dotted-path convention internal/manifest.Diff and
// internal/structdiff use.
const DeclaredVariable = "arrival_process.rate_rps"

var (
	// ErrTooFewPoints refuses a sweep declared with fewer than MinPoints
	// points (experiments.md rule 3).
	ErrTooFewPoints = errors.New("sweep: fewer than 6 points requested")
	// ErrBadFractionRange flags an invalid [min, max] fraction-of-capacity
	// range.
	ErrBadFractionRange = errors.New("sweep: fraction range invalid")
	// ErrProbeDidNotSaturate flags a capacity probe whose achieved
	// throughput was too close to the offered probe rate for the estimate
	// to be trustworthy — the probe rate was not high enough to find a
	// ceiling.
	ErrProbeDidNotSaturate = errors.New("sweep: capacity probe did not saturate; raise the probe rate")
	// ErrZeroCapacity flags a probe that completed nothing.
	ErrZeroCapacity = errors.New("sweep: probe achieved zero throughput; target unusable")
	// ErrCombinatorial flags a derived point set that varies in more than
	// the single declared variable (DeclaredVariable) — checked as defense
	// in depth alongside DeriveRateWorkload's construction guarantee.
	ErrCombinatorial = errors.New("sweep: derived workloads vary in more than the single declared variable")
)

// RatePoints returns n rates spanning [minFraction, maxFraction] *
// capacityRPS inclusive, evenly spaced, ascending. n must be >= MinPoints;
// capacityRPS must be > 0; 0 < minFraction < maxFraction is required.
func RatePoints(capacityRPS, minFraction, maxFraction float64, n int) ([]float64, error) {
	if n < MinPoints {
		return nil, fmt.Errorf("%w: got %d, want >= %d", ErrTooFewPoints, n, MinPoints)
	}
	if capacityRPS <= 0 {
		return nil, fmt.Errorf("sweep: capacity estimate must be > 0, got %g", capacityRPS)
	}
	if minFraction <= 0 || maxFraction <= minFraction {
		return nil, fmt.Errorf("%w: [%g, %g]", ErrBadFractionRange, minFraction, maxFraction)
	}
	rates := make([]float64, n)
	for i := 0; i < n; i++ {
		frac := minFraction + float64(i)*(maxFraction-minFraction)/float64(n-1)
		rates[i] = frac * capacityRPS
	}
	return rates, nil
}

// EstimateCapacity turns one overload probe's raw counts into a capacity
// estimate (see package doc). It refuses (typed) an unreliable estimate
// instead of silently returning one.
func EstimateCapacity(okCount int, elapsedSeconds, probeRateRPS float64) (float64, error) {
	if elapsedSeconds <= 0 {
		return 0, errors.New("sweep: probe elapsed seconds must be > 0")
	}
	if probeRateRPS <= 0 {
		return 0, errors.New("sweep: probe rate must be > 0")
	}
	achieved := float64(okCount) / elapsedSeconds
	if achieved <= 0 {
		return 0, fmt.Errorf("%w (0 ok / %.3fs elapsed)", ErrZeroCapacity, elapsedSeconds)
	}
	if achieved > 0.85*probeRateRPS {
		return achieved, fmt.Errorf("%w (achieved %.3f rps vs offered %.3f rps; the probe never fell meaningfully behind its offered rate)",
			ErrProbeDidNotSaturate, achieved, probeRateRPS)
	}
	return achieved, nil
}

// DeriveRateWorkload deep-copies base and overrides ONLY
// arrival_process.rate_rps, renaming it to name — every other field,
// including the seed, is untouched. Because per-item content (input/output
// lengths, cancellation, slow-client, prefix-sharing assignment) is sampled
// from PRNG streams keyed by item index and independent of the arrival
// stream (internal/schedule), two derived workloads sharing a seed but
// differing in rate_rps produce byte-identical item content — rate really
// is the only thing that varies, not merely by convention (see
// CheckSingleVariable and internal/schedule.TestFingerprintRateChangeOnlyAffectsOffsets).
func DeriveRateWorkload(base *workload.Workload, rateRPS float64, name string) (*workload.Workload, error) {
	if base.Arrival.Type != workload.ArrivalOpenLoopPoisson || base.Arrival.RateRPS == nil {
		return nil, errors.New("sweep: base workload must be single-rate open-loop-poisson")
	}
	if rateRPS <= 0 {
		return nil, fmt.Errorf("sweep: derived rate must be > 0, got %g", rateRPS)
	}
	data, err := json.Marshal(base)
	if err != nil {
		return nil, fmt.Errorf("sweep: %w", err)
	}
	var w workload.Workload
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("sweep: %w", err)
	}
	w.Name = name
	rate := rateRPS
	w.Arrival.RateRPS = &rate
	if err := w.Validate(); err != nil {
		return nil, fmt.Errorf("sweep: derived workload invalid: %w", err)
	}
	return &w, nil
}

// CheckSingleVariable structurally verifies that every derived workload
// differs from base ONLY in "name" (a required rename, not an experimental
// variable) and DeclaredVariable — defense in depth alongside
// DeriveRateWorkload's construction guarantee, and the same primitive
// internal/manifest.Diff and the `compare`/`experiment` commands use.
func CheckSingleVariable(base *workload.Workload, points []*workload.Workload) error {
	for _, w := range points {
		diffs, err := structdiff.Diff(base, w)
		if err != nil {
			return fmt.Errorf("sweep: %w", err)
		}
		for _, d := range diffs {
			if d == "name" || d == DeclaredVariable {
				continue
			}
			return fmt.Errorf("%w: field %q differs (workload %s)", ErrCombinatorial, d, w.Name)
		}
	}
	return nil
}

// Probe records one capacity probe's evidence.
type Probe struct {
	OfferedRateRPS float64 `json:"offered_rate_rps"`
	RequestCount   int     `json:"request_count"`
	OKCount        int     `json:"ok_count"`
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	AchievedRPS    float64 `json:"achieved_rps"`
	RunDir         string  `json:"run_dir"`
	Method         string  `json:"method"`
}

// Point records one executed sweep point.
type Point struct {
	Index              int     `json:"index"`
	RateRPS            float64 `json:"rate_rps"`
	FractionOfCapacity float64 `json:"fraction_of_capacity"`
	RunID              string  `json:"run_id"`
	RunDir             string  `json:"run_dir"`
	Repetitions        int     `json:"repetitions"`
	Sent               int     `json:"sent"`
	OK                 int     `json:"ok"`
	Errors             int     `json:"errors"`
	Shed               int     `json:"shed"`
	Canceled           int     `json:"canceled"`
}

// Manifest ties a sweep's points together into one evidence artifact
// (docs/tasks.md IB-T008: "sweep-level manifest tying them together (single
// varying variable = rate)"). This is a repo-local JSON format, not a
// contracts-owned schema (contracts own workload/run/raw-event/result only;
// docs/interfaces.md forbidden edges) — documented in docs/testing.md.
type Manifest struct {
	SweepID             string  `json:"sweep_id"`
	BaseWorkload        string  `json:"base_workload"`
	WorkloadVersion     string  `json:"workload_version"`
	Seed                int64   `json:"seed"`
	DeclaredVariable    string  `json:"declared_variable"`
	ConcurrencyCap      int     `json:"concurrency_cap"`
	ConcurrencyCapNote  string  `json:"concurrency_cap_note"`
	Target              string  `json:"target"`
	MinFraction         float64 `json:"min_fraction"`
	MaxFraction         float64 `json:"max_fraction"`
	Probe               Probe   `json:"probe"`
	CapacityEstimateRPS float64 `json:"capacity_estimate_rps"`
	Points              []Point `json:"points"`
	CreatedAt           string  `json:"created_at"`
}
