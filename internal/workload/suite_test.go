package workload

import (
	"fmt"
	"path/filepath"
	"testing"
)

// suiteDir is the canonical workload suite (IB-T003). These tests are the
// in-repo arm of the suite's review focus: length distributions controlled
// (output capped/directed — uncontrolled output length is a named
// anti-pattern), prefix-sharing ratio actually controlled, fixed distinct
// seeds, one suite version.
const suiteDir = "../../workloads"

const suiteVersion = "1.0.0"

// suiteSeeds pins each workload's fixed seed (10030NN, NN = suite position).
var suiteSeeds = map[string]int64{
	"chat-short":    1003001,
	"rag-long-in":   1003002,
	"gen-long-out":  1003003,
	"shared-prefix": 1003004,
	"mixed":         1003005,
	"bursty":        1003006,
	"cancel-storm":  1003007,
	"slow-client":   1003008,
}

func loadSuite(t *testing.T) map[string]*Workload {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(suiteDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	suite := make(map[string]*Workload, len(paths))
	for _, p := range paths {
		w, err := Load(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		base := filepath.Base(p)
		if base != w.Name+".json" {
			t.Errorf("%s: file name must match workload name %q", base, w.Name)
		}
		suite[w.Name] = w
	}
	return suite
}

func TestSuiteCompleteVersionedSeeded(t *testing.T) {
	suite := loadSuite(t)
	if len(suite) != len(suiteSeeds) {
		t.Fatalf("suite has %d workloads, want %d", len(suite), len(suiteSeeds))
	}
	for name, seed := range suiteSeeds {
		w, ok := suite[name]
		if !ok {
			t.Errorf("missing canonical workload %q", name)
			continue
		}
		if w.Version != suiteVersion {
			t.Errorf("%s: version %q, want suite version %q", name, w.Version, suiteVersion)
		}
		if *w.Seed != seed {
			t.Errorf("%s: seed %d, want fixed seed %d", name, *w.Seed, seed)
		}
		if w.Description == "" {
			t.Errorf("%s: intent must be documented in description", name)
		}
	}
}

// TestSuiteOutputLengthsControlled fails if anyone reintroduces an
// uncontrolled length distribution (anti-pattern, methodology rule 12).
func TestSuiteOutputLengthsControlled(t *testing.T) {
	for name, w := range loadSuite(t) {
		if err := bounded(w.OutputLen); err != nil {
			t.Errorf("%s: output_length_distribution uncontrolled: %v", name, err)
		}
		if err := bounded(w.InputLen); err != nil {
			t.Errorf("%s: input_length_distribution uncontrolled: %v", name, err)
		}
	}
}

// bounded reports whether every branch of the distribution has a finite
// declared upper bound (constant/uniform/empirical are bounded by
// construction; normal/lognormal need an explicit max clamp).
func bounded(d *Dist) error {
	switch d.Type {
	case "constant", "uniform", "empirical":
		return nil
	case "normal", "lognormal":
		if d.Max == nil {
			return fmt.Errorf("%s distribution without a max clamp", d.Type)
		}
		return nil
	case "mixture":
		for i, c := range d.Components {
			if err := bounded(c.Distribution); err != nil {
				return fmt.Errorf("components[%d]: %w", i, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown distribution type %q", d.Type)
	}
}

// TestSuiteOpenLoopOnly: the canonical suite exists to make latency/goodput
// claims, so closed-loop arrival has no place in it (ADR-0001/ADR-0003).
func TestSuiteOpenLoopOnly(t *testing.T) {
	for name, w := range loadSuite(t) {
		if w.Arrival.Type != ArrivalOpenLoopPoisson {
			t.Errorf("%s: canonical suite must use open-loop-poisson, got %q", name, w.Arrival.Type)
		}
	}
}

// TestSuitePrefixSharingControlled: the sharing ratio is a controlled,
// measurable variable — declared prefix length below the input floor so
// every sharing request carries a unique suffix, and declared group size.
func TestSuitePrefixSharingControlled(t *testing.T) {
	suite := loadSuite(t)
	sp := suite["shared-prefix"]
	if sp == nil {
		t.Fatal("shared-prefix missing")
	}
	if *sp.Prefix.Ratio != 0.8 || sp.Prefix.SharedPrefixLengthTokens == nil || sp.Prefix.GroupSize == nil {
		t.Fatalf("shared-prefix: ratio/prefix-length/group-size must all be pinned, got %+v", sp.Prefix)
	}
	if sp.InputLen.Min == nil || *sp.InputLen.Min <= float64(*sp.Prefix.SharedPrefixLengthTokens) {
		t.Error("shared-prefix: input floor must exceed the shared prefix length (unique suffix per request)")
	}
	for name, w := range suite {
		if name != "shared-prefix" && *w.Prefix.Ratio != 0 {
			t.Errorf("%s: prefix ratio must be 0 outside shared-prefix (incidental sharing is not controlled)", name)
		}
	}
}

// TestSuiteFullyRunnable pins today's honest execution boundary: since
// IB-T004 (prefix-sharing prompts, cancellation issuance, slow-client
// throttling) every canonical workload runs end-to-end. Only closed-loop
// arrival still refuses (ErrNotImplemented, IB-T008) — covered by
// TestClosedLoopDisclosedParsesButDeferred.
func TestSuiteFullyRunnable(t *testing.T) {
	for name, w := range loadSuite(t) {
		if err := w.CheckRunnable(); err != nil {
			t.Errorf("%s: canonical suite must be fully runnable since IB-T004, got %v", name, err)
		}
	}
}
