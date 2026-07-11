package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func okHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","created":1,"model":"mock-8b",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`)
	}
}

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const testWorkloadJSON = `{
	"name": "common-test",
	"version": "1.0.0",
	"seed": 314159,
	"arrival_process": {"type": "open-loop-poisson", "rate_rps": 20},
	"input_length_distribution": {"type": "constant", "value": 16},
	"output_length_distribution": {"type": "constant", "value": 8},
	"prefix_sharing": {"ratio": 0},
	"cancellation": {"rate": 0},
	"slow_client": {"fraction": 0},
	"stop": {"request_count": 5}
}`

const testFactsJSON = `{
	"run_id": "",
	"target_topology": "engine-direct",
	"workload_ref": {"name": "", "version": "", "seed": 0},
	"engine": {"name": "mock", "version": "dev", "flags": {}},
	"model": {"checkpoint": "mock-8b", "revision": "n/a", "tokenizer": "mock-estimator"},
	"hardware": {"gpu_model": null, "gpu_count": 0, "vram_gb": null, "driver_version": null, "cuda_version": null, "instance_type": "test"},
	"client": {"location": "same-host", "rtt_ms": null},
	"warm_up": {"policy": "none"},
	"repetitions": 1,
	"hypothesis": "cmd/inferbench common.go regression test."
}`

// Regression test for the seed-override bug: a nil SeedOverride (the Go
// zero value for a pointer, i.e. every call site that does not explicitly
// set it) must leave the workload's own declared seed intact. An earlier
// version used an int sentinel ("< 0 means no override") and every call
// site outside cmdRun forgot to set it, so the Go zero value (0) silently
// overrode every sweep/compare/experiment run's seed to 0.
func TestRunOnceNilSeedOverridePreservesWorkloadSeed(t *testing.T) {
	srv := httptest.NewServer(okHandler())
	defer srv.Close()

	dir := t.TempDir()
	wPath := writeTestFile(t, dir, "workload.json", testWorkloadJSON)
	fPath := writeTestFile(t, dir, "facts.json", testFactsJSON)

	out, err := runOnce(context.Background(), onceParams{
		WorkloadPath: wPath,
		ManifestPath: fPath,
		Target:       srv.URL,
		OutDir:       filepath.Join(dir, "out"),
		RunID:        "seed-regression",
		Model:        "mock-8b",
		Repetition:   1,
		ReqTimeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("runOnce failed: %v", err)
	}
	if *out.Workload.Seed != 314159 {
		t.Fatalf("workload seed must be preserved when SeedOverride is nil, got %d", *out.Workload.Seed)
	}
	if out.Manifest.WorkloadRef.Seed != 314159 {
		t.Fatalf("manifest workload_ref.seed must match the workload's declared seed, got %d", out.Manifest.WorkloadRef.Seed)
	}
	if out.Result.Sent != 5 || out.Result.OK != 5 {
		t.Fatalf("sent=%d ok=%d, want 5/5", out.Result.Sent, out.Result.OK)
	}
}

// A non-nil SeedOverride must actually override.
func TestRunOnceSeedOverrideApplies(t *testing.T) {
	srv := httptest.NewServer(okHandler())
	defer srv.Close()

	dir := t.TempDir()
	wPath := writeTestFile(t, dir, "workload.json", testWorkloadJSON)
	fPath := writeTestFile(t, dir, "facts.json", testFactsJSON)

	override := int64(999)
	out, err := runOnce(context.Background(), onceParams{
		WorkloadPath: wPath,
		SeedOverride: &override,
		ManifestPath: fPath,
		Target:       srv.URL,
		OutDir:       filepath.Join(dir, "out"),
		RunID:        "seed-override",
		Model:        "mock-8b",
		Repetition:   1,
		ReqTimeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("runOnce failed: %v", err)
	}
	if *out.Workload.Seed != 999 {
		t.Fatalf("workload seed must reflect the override, got %d", *out.Workload.Seed)
	}
}
