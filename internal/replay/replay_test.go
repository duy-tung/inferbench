package replay

import (
	"path/filepath"
	"testing"

	"github.com/duy-tung/inferbench/internal/schedule"
	"github.com/duy-tung/inferbench/internal/workload"
)

func w(t *testing.T, seed int64, rate float64) *workload.Workload {
	t.Helper()
	out, err := workload.Parse([]byte(`{
		"name": "replay-test",
		"version": "1.0.0",
		"seed": ` + itoa(seed) + `,
		"arrival_process": {"type": "open-loop-poisson", "rate_rps": ` + ftoa(rate) + `},
		"input_length_distribution": {"type": "constant", "value": 32},
		"output_length_distribution": {"type": "constant", "value": 16},
		"prefix_sharing": {"ratio": 0},
		"cancellation": {"rate": 0},
		"slow_client": {"fraction": 0},
		"stop": {"request_count": 25}
	}`))
	if err != nil {
		t.Fatalf("parse workload: %v", err)
	}
	return out
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func ftoa(f float64) string {
	whole := int64(f)
	return itoa(whole)
}

func TestReferenceRoundTrip(t *testing.T) {
	plan, err := schedule.Build(w(t, 42, 10))
	if err != nil {
		t.Fatal(err)
	}
	ref := BuildReference(w(t, 42, 10), plan)
	path := filepath.Join(t.TempDir(), "reference.json")
	if err := ref.Write(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if *loaded != ref {
		t.Fatalf("round trip mismatch: %+v vs %+v", *loaded, ref)
	}
}

func TestVerifySameSeedPasses(t *testing.T) {
	original := w(t, 7, 10)
	plan1, err := schedule.Build(original)
	if err != nil {
		t.Fatal(err)
	}
	ref := BuildReference(original, plan1)

	replayed := w(t, 7, 10)
	plan2, err := schedule.Build(replayed)
	if err != nil {
		t.Fatal(err)
	}
	if err := ref.Verify(replayed, plan2); err != nil {
		t.Fatalf("identical seed/workload must verify: %v", err)
	}
}

func TestVerifyDetectsSeedChange(t *testing.T) {
	original := w(t, 7, 10)
	plan1, err := schedule.Build(original)
	if err != nil {
		t.Fatal(err)
	}
	ref := BuildReference(original, plan1)

	changed := w(t, 8, 10)
	plan2, err := schedule.Build(changed)
	if err != nil {
		t.Fatal(err)
	}
	err = ref.Verify(changed, plan2)
	if err == nil {
		t.Fatal("a different seed must fail verification")
	}
}

func TestVerifyDetectsWorkloadIdentityChange(t *testing.T) {
	original := w(t, 7, 10)
	plan1, err := schedule.Build(original)
	if err != nil {
		t.Fatal(err)
	}
	ref := BuildReference(original, plan1)

	renamed := w(t, 7, 10)
	renamed.Name = "different-workload"
	plan2, err := schedule.Build(renamed)
	if err != nil {
		t.Fatal(err)
	}
	if err := ref.Verify(renamed, plan2); err == nil {
		t.Fatal("a different workload name must fail verification")
	}
}
