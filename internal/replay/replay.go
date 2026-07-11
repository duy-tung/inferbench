// Package replay re-executes a previously recorded workload EXACTLY (same
// seed => same schedule.Plan => same schedule.Fingerprint) and verifies the
// resulting schedule against a stored reference — the executable form of
// ADR-0001's "same seed -> identical schedule" contract (IB-T008).
//
// Every `inferbench run` (and `replay`) execution writes a small
// reference.json sidecar alongside manifest.json/events.jsonl/run.log:
// workload identity, seed, item count, and the schedule.Fingerprint.
// `inferbench replay` rebuilds the plan from the SAME workload file (the
// replay subcommand exposes no --seed/--rate override — replay is exact by
// construction, not by promise) and refuses, before sending a single
// request, if the rebuilt fingerprint disagrees with the reference.
package replay

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/duy-tung/inferbench/internal/schedule"
	"github.com/duy-tung/inferbench/internal/workload"
)

// ErrMismatch is returned when the rebuilt plan's fingerprint (or its
// declared identity) does not match the reference: the workload file, its
// seed, or its arrival process changed since the reference was recorded, so
// this is not a genuine replay. Refusing is preferred to silently running a
// different schedule under the "replay" label.
var ErrMismatch = errors.New("replay: rebuilt schedule does not match the reference (this is not an exact replay)")

// Reference is the sidecar every run/replay execution writes, carrying just
// enough to prove a later run replayed the identical schedule without
// needing to re-parse the full run.log dump.
type Reference struct {
	WorkloadName        string `json:"workload_name"`
	WorkloadVersion     string `json:"workload_version"`
	Seed                int64  `json:"seed"`
	ItemCount           int    `json:"item_count"`
	ScheduleFingerprint string `json:"schedule_fingerprint"`
}

// BuildReference derives the Reference for a built plan.
func BuildReference(w *workload.Workload, plan *schedule.Plan) Reference {
	return Reference{
		WorkloadName:        w.Name,
		WorkloadVersion:     w.Version,
		Seed:                plan.Seed,
		ItemCount:           len(plan.Items),
		ScheduleFingerprint: schedule.Fingerprint(plan),
	}
}

// Write serializes the reference as pretty JSON to path.
func (r Reference) Write(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Load reads a reference file (e.g. a prior run's reference.json).
func Load(path string) (*Reference, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("replay: %w", err)
	}
	var r Reference
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("replay: invalid reference %s: %w", path, err)
	}
	return &r, nil
}

// Verify checks a freshly built plan against the reference, refusing with a
// specific, typed ErrMismatch reason on any disagreement — never silently
// proceeding under a mismatched schedule.
func (r Reference) Verify(w *workload.Workload, plan *schedule.Plan) error {
	got := BuildReference(w, plan)
	switch {
	case got.WorkloadName != r.WorkloadName || got.WorkloadVersion != r.WorkloadVersion:
		return fmt.Errorf("%w: workload identity %s@%s != reference %s@%s",
			ErrMismatch, got.WorkloadName, got.WorkloadVersion, r.WorkloadName, r.WorkloadVersion)
	case got.Seed != r.Seed:
		return fmt.Errorf("%w: seed %d != reference seed %d", ErrMismatch, got.Seed, r.Seed)
	case got.ItemCount != r.ItemCount:
		return fmt.Errorf("%w: item_count %d != reference item_count %d", ErrMismatch, got.ItemCount, r.ItemCount)
	case got.ScheduleFingerprint != r.ScheduleFingerprint:
		return fmt.Errorf("%w: schedule_fingerprint %s != reference %s", ErrMismatch, got.ScheduleFingerprint, r.ScheduleFingerprint)
	}
	return nil
}
