package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validManifest() *Manifest {
	commit := "abc123"
	rtt := 0.2
	val := 50.0
	return &Manifest{
		RunID:          "run-1",
		TargetTopology: TopologyGatewayMock,
		WorkloadRef:    WorkloadRef{Name: "chat-short", Version: "0.1.0", Seed: 42},
		Engine:         Engine{Name: "mock", Version: "dev", Commit: &commit, Flags: map[string]any{}},
		Model:          Model{Checkpoint: "mock-8b", Revision: "n/a-mock", Tokenizer: "mock-estimator"},
		Hardware:       Hardware{GPUCount: 0, InstanceType: "local-dev"},
		Gateway:        &Gateway{Version: "dev", ConfigVersion: "static-flags"},
		Client:         Client{Location: "same-host", RTTms: &rtt},
		WarmUp:         WarmUp{Policy: "discard-requests", Value: &val},
		Repetitions:    1,
		Hypothesis:     "The generator keeps its precomputed schedule against the local mock pair.",
	}
}

func TestValidateOK(t *testing.T) {
	if err := validManifest().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRefusals(t *testing.T) {
	cases := map[string]func(m *Manifest){
		"missing run_id":                func(m *Manifest) { m.RunID = "" },
		"bad topology":                  func(m *Manifest) { m.TargetTopology = "direct" },
		"gateway-mock needs gateway":    func(m *Manifest) { m.Gateway = nil },
		"engine-direct forbids gateway": func(m *Manifest) { m.TargetTopology = TopologyEngineDirect },
		"nil flags":                     func(m *Manifest) { m.Engine.Flags = nil },
		"missing tokenizer":             func(m *Manifest) { m.Model.Tokenizer = "" },
		"missing instance":              func(m *Manifest) { m.Hardware.InstanceType = "" },
		"missing location":              func(m *Manifest) { m.Client.Location = "" },
		"warmup needs value":            func(m *Manifest) { m.WarmUp.Value = nil },
		"bad warmup policy":             func(m *Manifest) { m.WarmUp.Policy = "skip" },
		"zero repetitions":              func(m *Manifest) { m.Repetitions = 0 },
		"short hypothesis":              func(m *Manifest) { m.Hypothesis = "trust me" },
		"bad workload version":          func(m *Manifest) { m.WorkloadRef.Version = "one" },
	}
	for name, mutate := range cases {
		m := validManifest()
		mutate(m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: incomplete manifest accepted — the generator must refuse to run", name)
		}
	}
}

func TestLoadFactsStrict(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "facts.json")
	if err := os.WriteFile(p, []byte(`{"target_topology":"engine-direct","surprise":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFacts(p); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown fields must be rejected, got %v", err)
	}
}

func TestWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.json")
	m := validManifest()
	if err := m.Write(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFacts(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if got.RunID != m.RunID || got.Gateway == nil || *got.Client.RTTms != 0.2 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestDiffIgnoresBookkeepingFields(t *testing.T) {
	a := validManifest()
	b := validManifest()
	a.RunID, b.RunID = "run-a", "run-b"
	a.StartedAt, b.StartedAt = "2026-07-11T00:00:00Z", "2026-07-11T00:05:00Z"
	rttB := 5.0
	b.Client.RTTms = &rttB
	diffs, err := Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 0 {
		t.Fatalf("bookkeeping-only differences must be ignored, got %v", diffs)
	}
}

func TestDiffFindsDeclaredVariable(t *testing.T) {
	a := validManifest()
	b := validManifest()
	b.TargetTopology = TopologyEngineDirect
	b.Gateway = nil
	diffs, err := Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 2 { // target_topology, gateway (whole block disappears)
		t.Fatalf("want 2 diffs (target_topology, gateway), got %v", diffs)
	}
	found := map[string]bool{}
	for _, d := range diffs {
		found[d] = true
	}
	if !found["target_topology"] {
		t.Fatalf("expected target_topology in diffs, got %v", diffs)
	}
}

func TestDiffFindsEngineFlag(t *testing.T) {
	a := validManifest()
	b := validManifest()
	a.Engine.Flags = map[string]any{"max_num_seqs": 8}
	b.Engine.Flags = map[string]any{"max_num_seqs": 16}
	diffs, err := Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || diffs[0] != "engine.flags.max_num_seqs" {
		t.Fatalf("want [engine.flags.max_num_seqs], got %v", diffs)
	}
}

func TestDiffNoDifferenceBetweenIdenticalManifests(t *testing.T) {
	a := validManifest()
	b := validManifest()
	diffs, err := Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 0 {
		t.Fatalf("identical manifests must diff to nothing, got %v", diffs)
	}
}
