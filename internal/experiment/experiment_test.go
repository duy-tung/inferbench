package experiment

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/duy-tung/inferbench/internal/manifest"
)

func validHypothesisJSON() map[string]any {
	return map[string]any{
		"id":                 "EXP-test-001",
		"hypothesis":         "Raising X from A to B increases throughput and worsens tail latency.",
		"variable":           "target_topology",
		"levels":             []string{"engine-direct", "via-gateway"},
		"expected_direction": "gateway adds a small p95 overhead",
		"workload":           "chat-short@1.0.0 (seed 1003001)",
		"constants":          "engine version, model, hardware, workload version+seed, warm-up policy held fixed",
		"stop_condition":     ">= 3 runs per arm complete or the target becomes unreachable",
		"repeat_policy":      "3 runs per arm, pooled percentiles",
	}
}

func writeHypothesis(t *testing.T, fields map[string]any) string {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "hypothesis.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRefusesEmptyPath(t *testing.T) {
	_, err := Load("")
	if !errors.Is(err, ErrHypothesisRequired) {
		t.Fatalf("want ErrHypothesisRequired, got %v", err)
	}
}

func TestLoadRefusesMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if !errors.Is(err, ErrHypothesisRequired) {
		t.Fatalf("want ErrHypothesisRequired, got %v", err)
	}
}

func TestLoadAcceptsCompleteHypothesis(t *testing.T) {
	path := writeHypothesis(t, validHypothesisJSON())
	h, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != "EXP-test-001" || h.Variable != "target_topology" || len(h.Levels) != 2 {
		t.Fatalf("unexpected hypothesis: %+v", h)
	}
}

func TestLoadRefusesIncompleteHypothesis(t *testing.T) {
	cases := map[string]func(m map[string]any){
		"missing id":             func(m map[string]any) { delete(m, "id") },
		"missing variable":       func(m map[string]any) { delete(m, "variable") },
		"single level":           func(m map[string]any) { m["levels"] = []string{"only-one"} },
		"short hypothesis text":  func(m map[string]any) { m["hypothesis"] = "too short" },
		"missing stop_condition": func(m map[string]any) { delete(m, "stop_condition") },
		"missing repeat_policy":  func(m map[string]any) { delete(m, "repeat_policy") },
		"missing constants":      func(m map[string]any) { delete(m, "constants") },
		"missing workload":       func(m map[string]any) { delete(m, "workload") },
	}
	for name, mutate := range cases {
		fields := validHypothesisJSON()
		mutate(fields)
		path := writeHypothesis(t, fields)
		if _, err := Load(path); err == nil {
			t.Errorf("%s: incomplete hypothesis file accepted", name)
		} else if !errors.Is(err, ErrIncompleteHypothesis) {
			t.Errorf("%s: want ErrIncompleteHypothesis, got %v", name, err)
		}
	}
}

func TestLoadRefusesUnknownField(t *testing.T) {
	fields := validHypothesisJSON()
	fields["variables"] = []string{"a", "b"} // plural — a matrix-shaped mistake
	path := writeHypothesis(t, fields)
	if _, err := Load(path); err == nil {
		t.Fatal("an unknown field (e.g. a mistaken 'variables' plural) must be refused")
	}
}

func baseManifest(runID, topology string) *manifest.Manifest {
	rtt := 0.2
	val := 50.0
	m := &manifest.Manifest{
		RunID:          runID,
		TargetTopology: topology,
		WorkloadRef:    manifest.WorkloadRef{Name: "chat-short", Version: "1.0.0", Seed: 1003001},
		Engine:         manifest.Engine{Name: "mock", Version: "dev", Flags: map[string]any{}},
		Model:          manifest.Model{Checkpoint: "mock-8b", Revision: "n/a", Tokenizer: "mock-estimator"},
		Hardware:       manifest.Hardware{GPUCount: 0, InstanceType: "local-dev"},
		Client:         manifest.Client{Location: "same-host", RTTms: &rtt},
		WarmUp:         manifest.WarmUp{Policy: "discard-requests", Value: &val},
		Repetitions:    1,
		Hypothesis:     "Test manifest for experiment governance unit tests.",
	}
	if topology != manifest.TopologyEngineDirect {
		m.Gateway = &manifest.Gateway{Version: "dev", ConfigVersion: "static"}
	}
	return m
}

func TestCheckArmsAcceptsSingleDeclaredVariable(t *testing.T) {
	fields := validHypothesisJSON()
	path := writeHypothesis(t, fields)
	h, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a := baseManifest("run-direct", manifest.TopologyEngineDirect)
	b := baseManifest("run-gateway", manifest.TopologyGatewayMock)
	if err := h.CheckArms([]*manifest.Manifest{a, b}); err != nil {
		t.Fatalf("single-variable comparison should pass: %v", err)
	}
}

func TestCheckArmsRefusesCombinatorial(t *testing.T) {
	fields := validHypothesisJSON()
	path := writeHypothesis(t, fields)
	h, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a := baseManifest("run-direct", manifest.TopologyEngineDirect)
	b := baseManifest("run-gateway", manifest.TopologyGatewayMock)
	// A second, undeclared variable also changed: model checkpoint.
	b.Model.Checkpoint = "different-model"
	err = h.CheckArms([]*manifest.Manifest{a, b})
	if !errors.Is(err, ErrCombinatorial) {
		t.Fatalf("want ErrCombinatorial, got %v", err)
	}
}

func TestCheckArmsRefusesDegenerateComparison(t *testing.T) {
	fields := validHypothesisJSON()
	path := writeHypothesis(t, fields)
	h, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a := baseManifest("run-a", manifest.TopologyGatewayMock)
	b := baseManifest("run-b", manifest.TopologyGatewayMock) // declared variable never differs
	err = h.CheckArms([]*manifest.Manifest{a, b})
	if !errors.Is(err, ErrNotVarying) {
		t.Fatalf("want ErrNotVarying, got %v", err)
	}
}

func TestCheckGPUSessionRequiredWhenHardwareDeclaresGPU(t *testing.T) {
	fields := validHypothesisJSON()
	path := writeHypothesis(t, fields)
	h, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	gpuModel := "H100"
	m := baseManifest("run-gpu", manifest.TopologyGatewayMock)
	m.Hardware.GPUModel = &gpuModel
	m.Hardware.GPUCount = 1
	if err := h.CheckGPUSession([]*manifest.Manifest{m}); !errors.Is(err, ErrGPUSessionRequired) {
		t.Fatalf("want ErrGPUSessionRequired, got %v", err)
	}

	fields["gpu_session"] = map[string]any{
		"session_manifest_ref":   "sessions/2026-07-11-gpu.json",
		"auto_stop_ref":          "scripts/gpu-auto-stop.sh",
		"budget_alert_confirmed": true,
	}
	path2 := writeHypothesis(t, fields)
	h2, err := Load(path2)
	if err != nil {
		t.Fatal(err)
	}
	if err := h2.CheckGPUSession([]*manifest.Manifest{m}); err != nil {
		t.Fatalf("complete GPU session block should pass: %v", err)
	}
}

func TestCheckGPUSessionNotRequiredForCPU(t *testing.T) {
	fields := validHypothesisJSON()
	path := writeHypothesis(t, fields)
	h, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m := baseManifest("run-cpu", manifest.TopologyGatewayMock)
	if err := h.CheckGPUSession([]*manifest.Manifest{m}); err != nil {
		t.Fatalf("CPU-only manifest must not require a GPU session block: %v", err)
	}
}
