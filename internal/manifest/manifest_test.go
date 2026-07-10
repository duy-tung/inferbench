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
