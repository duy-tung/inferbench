// Package experiment enforces controlled-experiment governance
// (docs/experiments.md §5, IB-T009): a hypothesis file is REQUIRED before
// any load is generated, it must declare exactly one variable, and the
// arms actually executed must differ ONLY in that declared variable
// (reusing internal/manifest.Diff, the same structural primitive
// `compare`/`sweep` use) — the structural guard against combinatorial /
// full-matrix sweeps. GPU-targeting experiments additionally require the
// G6 session artifacts.
//
// Hypothesis file format: JSON, not the YAML sketch in docs/experiments.md
// §5. The Go module is stdlib-only end to end (docs/implementation-notes.md
// IB-T002), and every other artifact this repo reads or writes (workload,
// manifest, raw-event) is JSON; adding a YAML dependency for one file type
// was judged not worth it — the field set is identical to the documented
// template, this is a serialization choice only. Recorded as a reversible
// decision in docs/implementation-notes.md (IB-T009).
package experiment

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/duy-tung/inferbench/internal/manifest"
)

// ErrHypothesisRequired is the G6 enforcement mechanism: every experiment
// invocation must name a hypothesis file, or it is refused before any
// request is sent.
var ErrHypothesisRequired = errors.New("experiment: --hypothesis is required (experiments.md §5); refusing to run a hypothesis-less experiment")

// ErrIncompleteHypothesis flags a hypothesis file missing a required field.
var ErrIncompleteHypothesis = errors.New("experiment: incomplete hypothesis file")

// ErrCombinatorial flags an experiment whose arms vary in more than the
// declared variable — the structural full-matrix-sweep guard.
var ErrCombinatorial = errors.New("experiment: arms vary in more than the single declared variable — no full-matrix sweeps (experiments.md §5)")

// ErrNotVarying flags a degenerate "comparison" whose declared variable
// never actually differs across arms.
var ErrNotVarying = errors.New("experiment: declared variable never differs across arms — not a real comparison")

// ErrGPUSessionRequired flags a GPU-targeting experiment lacking the G6
// session artifacts (written hypothesis + manifest + auto-stop reference +
// budget alert confirmation).
var ErrGPUSessionRequired = errors.New("experiment: hardware declares a GPU session but the hypothesis file has no gpu_session block (session manifest ref + auto-stop ref + budget alert confirmation required, G6)")

// GPUSession is the G6 session-artifact block, required only when the
// experiment's manifests declare GPU hardware.
type GPUSession struct {
	SessionManifestRef   string `json:"session_manifest_ref"`
	AutoStopRef          string `json:"auto_stop_ref"`
	BudgetAlertConfirmed bool   `json:"budget_alert_confirmed"`
}

// Hypothesis mirrors the docs/experiments.md §5 template.
type Hypothesis struct {
	ID                string      `json:"id"`
	HypothesisText    string      `json:"hypothesis"`
	Variable          string      `json:"variable"`
	Levels            []string    `json:"levels"`
	ExpectedDirection string      `json:"expected_direction"`
	Workload          string      `json:"workload"`
	Constants         string      `json:"constants"`
	StopCondition     string      `json:"stop_condition"`
	RepeatPolicy      string      `json:"repeat_policy"`
	SLOReference      string      `json:"slo_reference,omitempty"`
	ProvenanceNotes   string      `json:"provenance_notes,omitempty"`
	GPU               *GPUSession `json:"gpu_session,omitempty"`
}

// Load reads and validates a hypothesis file. path == "" is refused with
// ErrHypothesisRequired — every experiment-mode CLI command routes through
// this call before doing anything else, so a hypothesis-less invocation
// never generates load.
func Load(path string) (*Hypothesis, error) {
	if path == "" {
		return nil, ErrHypothesisRequired
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHypothesisRequired, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var h Hypothesis
	if err := dec.Decode(&h); err != nil {
		return nil, fmt.Errorf("%w: %s: invalid JSON: %v", ErrIncompleteHypothesis, path, err)
	}
	if err := h.Validate(); err != nil {
		return nil, err
	}
	return &h, nil
}

// Validate checks every required field per the experiments.md §5 template.
func (h *Hypothesis) Validate() error {
	req := func(name, v string) error {
		if v == "" {
			return fmt.Errorf("%w: %s is required", ErrIncompleteHypothesis, name)
		}
		return nil
	}
	if err := req("id", h.ID); err != nil {
		return err
	}
	if len(h.HypothesisText) < 10 {
		return fmt.Errorf("%w: hypothesis must be a falsifiable statement (>= 10 chars)", ErrIncompleteHypothesis)
	}
	if err := req("variable", h.Variable); err != nil {
		return err
	}
	if len(h.Levels) < 2 {
		return fmt.Errorf("%w: levels must declare >= 2 levels of the single variable %q", ErrIncompleteHypothesis, h.Variable)
	}
	if err := req("expected_direction", h.ExpectedDirection); err != nil {
		return err
	}
	if err := req("workload", h.Workload); err != nil {
		return err
	}
	if err := req("constants", h.Constants); err != nil {
		return err
	}
	if err := req("stop_condition", h.StopCondition); err != nil {
		return err
	}
	if err := req("repeat_policy", h.RepeatPolicy); err != nil {
		return err
	}
	return nil
}

// CheckArms verifies the declared variable governs every difference among
// the given manifests (pairwise vs the first arm) — the structural
// single-variable / no-full-matrix guard, reusing the exact primitive
// `compare` and `sweep` use (internal/manifest.Diff). It also verifies the
// declared variable actually differs somewhere: arms that are otherwise
// identical are not a real comparison.
func (h *Hypothesis) CheckArms(arms []*manifest.Manifest) error {
	if len(arms) < 2 {
		return nil
	}
	allowed := map[string]bool{h.Variable: true}
	for _, f := range manifest.ImpliedFields(h.Variable) {
		allowed[f] = true
	}
	varied := false
	for i := 1; i < len(arms); i++ {
		diffs, err := manifest.Diff(arms[0], arms[i])
		if err != nil {
			return fmt.Errorf("experiment: %w", err)
		}
		for _, d := range diffs {
			if !allowed[d] {
				return fmt.Errorf("%w: arm %d (%s) differs from arm 0 (%s) in %q; declared variable is %q",
					ErrCombinatorial, i, arms[i].RunID, arms[0].RunID, d, h.Variable)
			}
			if d == h.Variable {
				varied = true
			}
		}
	}
	if !varied {
		return fmt.Errorf("%w: %q", ErrNotVarying, h.Variable)
	}
	return nil
}

// CheckGPUSession refuses (typed) an experiment whose manifests declare GPU
// hardware but whose hypothesis carries no complete gpu_session block (G6).
func (h *Hypothesis) CheckGPUSession(arms []*manifest.Manifest) error {
	for _, m := range arms {
		declaresGPU := m.Hardware.GPUCount > 0 || m.Hardware.GPUModel != nil
		if !declaresGPU {
			continue
		}
		if h.GPU == nil || h.GPU.SessionManifestRef == "" || h.GPU.AutoStopRef == "" || !h.GPU.BudgetAlertConfirmed {
			return fmt.Errorf("%w (run_id=%s)", ErrGPUSessionRequired, m.RunID)
		}
	}
	return nil
}
