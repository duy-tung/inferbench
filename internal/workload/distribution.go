package workload

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
)

// Dist mirrors the schema's $defs/distribution oneOf. Type discriminates.
type Dist struct {
	Type string `json:"type"`

	// constant
	Value *float64 `json:"value,omitempty"`
	// uniform + clamps for normal/lognormal
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
	// normal
	Mean   *float64 `json:"mean,omitempty"`
	Stddev *float64 `json:"stddev,omitempty"`
	// lognormal
	Mu    *float64 `json:"mu,omitempty"`
	Sigma *float64 `json:"sigma,omitempty"`
	// empirical
	Samples []float64 `json:"samples,omitempty"`
	// mixture
	Components []MixtureComponent `json:"components,omitempty"`
}

// MixtureComponent is one weighted mixture member.
type MixtureComponent struct {
	Weight       *float64 `json:"weight"`
	Distribution *Dist    `json:"distribution"`
}

// Validate checks the distribution's structural schema rules.
func (d *Dist) Validate(path string) error {
	switch d.Type {
	case "constant":
		if d.Value == nil || *d.Value < 0 {
			return fmt.Errorf("workload: %s: constant requires value >= 0", path)
		}
	case "uniform":
		if d.Min == nil || d.Max == nil || *d.Min < 0 || *d.Max < 0 {
			return fmt.Errorf("workload: %s: uniform requires min and max >= 0", path)
		}
		if *d.Max < *d.Min {
			return fmt.Errorf("workload: %s: uniform max < min", path)
		}
	case "normal":
		if d.Mean == nil || *d.Mean <= 0 || d.Stddev == nil || *d.Stddev < 0 {
			return fmt.Errorf("workload: %s: normal requires mean > 0 and stddev >= 0", path)
		}
	case "lognormal":
		if d.Mu == nil || d.Sigma == nil || *d.Sigma < 0 {
			return fmt.Errorf("workload: %s: lognormal requires mu and sigma >= 0", path)
		}
	case "empirical":
		if len(d.Samples) == 0 {
			return fmt.Errorf("workload: %s: empirical requires at least one sample", path)
		}
		for i, s := range d.Samples {
			if s < 0 {
				return fmt.Errorf("workload: %s: samples[%d] must be >= 0", path, i)
			}
		}
	case "mixture":
		if len(d.Components) < 2 {
			return fmt.Errorf("workload: %s: mixture requires >= 2 components", path)
		}
		for i, c := range d.Components {
			if c.Weight == nil || *c.Weight <= 0 {
				return fmt.Errorf("workload: %s: components[%d].weight must be > 0", path, i)
			}
			if c.Distribution == nil {
				return fmt.Errorf("workload: %s: components[%d].distribution required", path, i)
			}
			if err := c.Distribution.Validate(fmt.Sprintf("%s.components[%d]", path, i)); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("workload: %s: unknown distribution type %q", path, d.Type)
	}
	return nil
}

// Sample draws one value using rng. All randomness comes from rng, so the
// caller's seeding fully determines the draw (schema: all sampling MUST
// derive deterministically from the workload seed).
func (d *Dist) Sample(rng *rand.Rand) float64 {
	switch d.Type {
	case "constant":
		return *d.Value
	case "uniform":
		return *d.Min + rng.Float64()*(*d.Max-*d.Min)
	case "normal":
		return clamp(*d.Mean+*d.Stddev*rng.NormFloat64(), d.Min, d.Max)
	case "lognormal":
		return clamp(math.Exp(*d.Mu+*d.Sigma*rng.NormFloat64()), d.Min, d.Max)
	case "empirical":
		return d.Samples[rng.IntN(len(d.Samples))]
	case "mixture":
		total := 0.0
		for _, c := range d.Components {
			total += *c.Weight
		}
		x := rng.Float64() * total
		for _, c := range d.Components {
			x -= *c.Weight
			if x < 0 {
				return c.Distribution.Sample(rng)
			}
		}
		return d.Components[len(d.Components)-1].Distribution.Sample(rng)
	}
	// Validate() rejects unknown types before sampling is reachable.
	panic(errors.New("workload: sample on unvalidated distribution"))
}

// SampleTokens draws a token-valued sample: rounded to the nearest integer,
// floored at minTokens (schema: "rounded to the nearest integer >= 1" for
// lengths; >= 0 for cancellation points).
func (d *Dist) SampleTokens(rng *rand.Rand, minTokens int) int {
	n := int(math.Round(d.Sample(rng)))
	if n < minTokens {
		return minTokens
	}
	return n
}

func clamp(v float64, min, max *float64) float64 {
	if min != nil && v < *min {
		v = *min
	}
	if max != nil && v > *max {
		v = *max
	}
	if v < 0 {
		v = 0
	}
	return v
}
