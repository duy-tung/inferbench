// Package workload loads and validates workload definition files per the
// contracts-owned workload.schema.json (serving-contracts, Contract 3).
//
// The canonical validator is the contracts kit (jsonschema); this package
// re-checks the structural rules the generator depends on at run time so an
// invalid or unversioned workload refuses to run even without the kit.
//
// Scope note: prefix-sharing, cancellation, and slow-client profiles are
// fully executable since IB-T004. Closed-loop arrival is parsed and
// validated but NOT implemented (response-coupled dispatch does not fit the
// precomputed schedule.Plan model); attempting to run such a workload
// returns a typed ErrNotImplemented. IB-T008's capacity-estimate procedure
// (which ADR-0003 permits closed-loop for) uses an open-loop overload probe
// instead, so this stays deferred with no current owner — see
// docs/implementation-notes.md "IB-T008: closed-loop arrival execution
// stays deferred".
package workload

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
)

// ErrNotImplemented marks a schema-valid workload feature this build does
// not execute yet. It is a refusal, never a silent downgrade.
var ErrNotImplemented = errors.New("workload feature not implemented in this build")

// Arrival process types.
const (
	ArrivalOpenLoopPoisson = "open-loop-poisson"
	ArrivalClosedLoop      = "closed-loop"
)

// Cancellation point triggers (workload.schema.json cancellation.point.trigger).
const (
	CancelTriggerElapsed = "elapsed-seconds"
	CancelTriggerTokens  = "output-tokens"
)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Workload mirrors workload.schema.json.
type Workload struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Seed        *int64   `json:"seed"`
	Description string   `json:"description,omitempty"`
	Arrival     Arrival  `json:"arrival_process"`
	InputLen    *Dist    `json:"input_length_distribution"`
	OutputLen   *Dist    `json:"output_length_distribution"`
	Prefix      *Prefix  `json:"prefix_sharing"`
	Cancel      *Cancel  `json:"cancellation"`
	SlowClient  *Slow    `json:"slow_client"`
	Stop        *Stop    `json:"stop"`
	Tags        []string `json:"tags,omitempty"`
}

// Arrival is the arrival_process oneOf.
type Arrival struct {
	Type                string   `json:"type"`
	RateRPS             *float64 `json:"rate_rps,omitempty"`
	Phases              []Phase  `json:"phases,omitempty"`
	RepeatPhases        *bool    `json:"repeat_phases,omitempty"`
	Concurrency         *int     `json:"concurrency,omitempty"`
	ThinkTimeSeconds    *float64 `json:"think_time_seconds,omitempty"`
	ClosedLoopDisclosed *bool    `json:"closed_loop_disclosed,omitempty"`
}

// Phase is one piecewise-constant rate segment.
type Phase struct {
	DurationSeconds float64 `json:"duration_seconds"`
	RateRPS         float64 `json:"rate_rps"`
}

// Prefix is the prefix_sharing block.
type Prefix struct {
	Ratio                    *float64 `json:"ratio"`
	SharedPrefixLengthTokens *int     `json:"shared_prefix_length_tokens,omitempty"`
	GroupSize                *int     `json:"group_size,omitempty"`
}

// Cancel is the cancellation block.
type Cancel struct {
	Rate  *float64     `json:"rate"`
	Point *CancelPoint `json:"point,omitempty"`
}

// CancelPoint is the cancellation point spec.
type CancelPoint struct {
	Trigger      string `json:"trigger"`
	Distribution *Dist  `json:"distribution"`
}

// Slow is the slow_client block.
type Slow struct {
	Fraction                *float64 `json:"fraction"`
	ReadBytesPerSecond      *int     `json:"read_bytes_per_second,omitempty"`
	InitialReadDelaySeconds *float64 `json:"initial_read_delay_seconds,omitempty"`
}

// Stop is the stop condition (exactly one of the two fields).
type Stop struct {
	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
	RequestCount    *int     `json:"request_count,omitempty"`
}

// Load reads and validates a workload file. Unknown fields are rejected
// (the schema declares additionalProperties: false throughout).
func Load(path string) (*Workload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workload: %w", err)
	}
	return Parse(data)
}

// Parse decodes and validates workload JSON.
func Parse(data []byte) (*Workload, error) {
	var w Workload
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return nil, fmt.Errorf("workload: invalid JSON: %w", err)
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}
	return &w, nil
}

// Validate re-checks the structural schema rules the generator relies on.
func (w *Workload) Validate() error {
	if !nameRe.MatchString(w.Name) {
		return fmt.Errorf("workload: name %q must be kebab-case", w.Name)
	}
	if !versionRe.MatchString(w.Version) {
		return fmt.Errorf("workload: version %q must be SemVer", w.Version)
	}
	if w.Seed == nil || *w.Seed < 0 {
		return errors.New("workload: seed is required and must be >= 0")
	}
	if err := w.validateArrival(); err != nil {
		return err
	}
	if w.InputLen == nil {
		return errors.New("workload: input_length_distribution is required")
	}
	if err := w.InputLen.Validate("input_length_distribution"); err != nil {
		return err
	}
	if w.OutputLen == nil {
		return errors.New("workload: output_length_distribution is required")
	}
	if err := w.OutputLen.Validate("output_length_distribution"); err != nil {
		return err
	}
	if w.Prefix == nil || w.Prefix.Ratio == nil {
		return errors.New("workload: prefix_sharing.ratio is required (0 = none, still declared)")
	}
	if r := *w.Prefix.Ratio; r < 0 || r > 1 {
		return errors.New("workload: prefix_sharing.ratio must be in [0,1]")
	}
	if *w.Prefix.Ratio > 0 && w.Prefix.SharedPrefixLengthTokens == nil {
		return errors.New("workload: prefix_sharing.shared_prefix_length_tokens required when ratio > 0")
	}
	if w.Cancel == nil || w.Cancel.Rate == nil {
		return errors.New("workload: cancellation.rate is required (0 = none, still declared)")
	}
	if r := *w.Cancel.Rate; r < 0 || r > 1 {
		return errors.New("workload: cancellation.rate must be in [0,1]")
	}
	if *w.Cancel.Rate > 0 {
		if w.Cancel.Point == nil || w.Cancel.Point.Distribution == nil ||
			(w.Cancel.Point.Trigger != CancelTriggerElapsed && w.Cancel.Point.Trigger != CancelTriggerTokens) {
			return errors.New("workload: cancellation.point (trigger + distribution) required when rate > 0")
		}
		if err := w.Cancel.Point.Distribution.Validate("cancellation.point.distribution"); err != nil {
			return err
		}
	}
	if w.SlowClient == nil || w.SlowClient.Fraction == nil {
		return errors.New("workload: slow_client.fraction is required (0 = none, still declared)")
	}
	if f := *w.SlowClient.Fraction; f < 0 || f > 1 {
		return errors.New("workload: slow_client.fraction must be in [0,1]")
	}
	if *w.SlowClient.Fraction > 0 && w.SlowClient.ReadBytesPerSecond == nil {
		return errors.New("workload: slow_client.read_bytes_per_second required when fraction > 0")
	}
	if w.Stop == nil {
		return errors.New("workload: stop is required")
	}
	hasDur := w.Stop.DurationSeconds != nil
	hasCount := w.Stop.RequestCount != nil
	if hasDur == hasCount {
		return errors.New("workload: stop requires exactly one of duration_seconds or request_count")
	}
	if hasDur && *w.Stop.DurationSeconds <= 0 {
		return errors.New("workload: stop.duration_seconds must be > 0")
	}
	if hasCount && *w.Stop.RequestCount < 1 {
		return errors.New("workload: stop.request_count must be >= 1")
	}
	return nil
}

func (w *Workload) validateArrival() error {
	switch w.Arrival.Type {
	case ArrivalOpenLoopPoisson:
		hasRate := w.Arrival.RateRPS != nil
		hasPhases := len(w.Arrival.Phases) > 0
		if hasRate == hasPhases {
			return errors.New("workload: open-loop-poisson requires exactly one of rate_rps or phases")
		}
		if hasRate && *w.Arrival.RateRPS <= 0 {
			return errors.New("workload: rate_rps must be > 0")
		}
		for i, p := range w.Arrival.Phases {
			if p.DurationSeconds <= 0 {
				return fmt.Errorf("workload: phases[%d].duration_seconds must be > 0", i)
			}
			if p.RateRPS < 0 {
				return fmt.Errorf("workload: phases[%d].rate_rps must be >= 0", i)
			}
		}
		if w.Arrival.RepeatPhases != nil && !hasPhases {
			return errors.New("workload: repeat_phases requires phases")
		}
		return nil
	case ArrivalClosedLoop:
		if w.Arrival.Concurrency == nil || *w.Arrival.Concurrency < 1 {
			return errors.New("workload: closed-loop requires concurrency >= 1")
		}
		if w.Arrival.ClosedLoopDisclosed == nil || !*w.Arrival.ClosedLoopDisclosed {
			return errors.New("workload: closed-loop requires the literal closed_loop_disclosed: true flag")
		}
		return nil
	default:
		return fmt.Errorf("workload: unknown arrival_process type %q", w.Arrival.Type)
	}
}

// CheckRunnable returns a typed ErrNotImplemented for schema-valid features
// this build cannot execute yet. Deferred scope is recorded in docs/tasks.md.
// Since IB-T004 the client-side traffic features (prefix-sharing prompt
// construction, cancellation issuance, slow-client read throttling) are
// implemented; only closed-loop arrival execution remains deferred.
func (w *Workload) CheckRunnable() error {
	if w.Arrival.Type == ArrivalClosedLoop {
		return fmt.Errorf("%w: closed-loop arrival (throughput-ceiling mode, ADR-0003) is not implemented; internal/sweep's capacity-estimate procedure uses an open-loop overload probe instead (see docs/implementation-notes.md)", ErrNotImplemented)
	}
	return nil
}
