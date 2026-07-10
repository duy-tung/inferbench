// Package manifest loads, completes, and validates the run manifest per the
// contracts-owned benchmark-run.schema.json (Contract 3). The generator
// REFUSES to run without a complete manifest: an incomplete manifest is not
// a valid run record (methodology rule 6).
//
// The operator authors a "facts file" holding everything the tool cannot
// know (engine, model, hardware, client location, warm-up policy,
// repetitions, hypothesis). The tool fills run_id (if empty), workload_ref,
// started_at, and contracts_bundle_version, then validates the whole.
package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
)

// ContractsBundleVersion is the serving-contracts pin this build emits.
// Re-pinned at IB-T003 (2026-07-10) from pre-release commit 8c58863 to the
// released v0.1.0 tag (commit 2df9f81); see docs/implementation-notes.md.
const ContractsBundleVersion = "v0.1.0"

var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Topology values.
const (
	TopologyEngineDirect = "engine-direct"
	TopologyViaGateway   = "via-gateway"
	TopologyGatewayMock  = "gateway-mock"
)

// Manifest mirrors benchmark-run.schema.json.
type Manifest struct {
	RunID                  string      `json:"run_id"`
	TargetTopology         string      `json:"target_topology"`
	WorkloadRef            WorkloadRef `json:"workload_ref"`
	Engine                 Engine      `json:"engine"`
	Model                  Model       `json:"model"`
	Hardware               Hardware    `json:"hardware"`
	Gateway                *Gateway    `json:"gateway,omitempty"`
	Client                 Client      `json:"client"`
	WarmUp                 WarmUp      `json:"warm_up"`
	Repetitions            int         `json:"repetitions"`
	Hypothesis             string      `json:"hypothesis"`
	StartedAt              string      `json:"started_at,omitempty"`
	ContractsBundleVersion string      `json:"contracts_bundle_version,omitempty"`
	Notes                  string      `json:"notes,omitempty"`
}

// WorkloadRef is the exact workload identity of the run.
type WorkloadRef struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Seed    int64  `json:"seed"`
}

// Engine describes the target engine. Commit is nullable only when
// genuinely unavailable; Flags empty asserts pure defaults.
type Engine struct {
	Name    string         `json:"name"`
	Version string         `json:"version"`
	Commit  *string        `json:"commit"`
	Flags   map[string]any `json:"flags"`
}

// Model identifies the checkpoint, revision, and tokenizer.
type Model struct {
	Checkpoint string `json:"checkpoint"`
	Revision   string `json:"revision"`
	Tokenizer  string `json:"tokenizer"`
}

// Hardware nulls assert "not applicable" (CPU-only mock runs), never
// "not recorded".
type Hardware struct {
	GPUModel      *string  `json:"gpu_model"`
	GPUCount      int      `json:"gpu_count"`
	VRAMGB        *float64 `json:"vram_gb"`
	DriverVersion *string  `json:"driver_version"`
	CUDAVersion   *string  `json:"cuda_version"`
	InstanceType  string   `json:"instance_type"`
}

// Gateway is required for via-gateway/gateway-mock topologies and forbidden
// for engine-direct.
type Gateway struct {
	Version       string `json:"version"`
	ConfigVersion string `json:"config_version"`
}

// Client records where the load generator ran relative to the target.
// RTTms null is permitted only for same-process mocks; the runner measures
// and fills it when the facts file leaves it null.
type Client struct {
	Location string   `json:"location"`
	RTTms    *float64 `json:"rtt_ms"`
}

// WarmUp is the declared warm-up policy.
type WarmUp struct {
	Policy string   `json:"policy"`
	Value  *float64 `json:"value,omitempty"`
}

// LoadFacts reads the operator-authored facts file (strict: unknown fields
// rejected, the schema is additionalProperties: false).
func LoadFacts(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest: invalid JSON: %w", err)
	}
	return &m, nil
}

// Validate refuses incomplete manifests with the first missing fact.
func (m *Manifest) Validate() error {
	if m.RunID == "" {
		return errors.New("manifest: run_id is required")
	}
	switch m.TargetTopology {
	case TopologyEngineDirect:
		if m.Gateway != nil {
			return errors.New("manifest: engine-direct must not carry a gateway block")
		}
	case TopologyViaGateway, TopologyGatewayMock:
		if m.Gateway == nil || m.Gateway.Version == "" || m.Gateway.ConfigVersion == "" {
			return fmt.Errorf("manifest: topology %s requires gateway.version and gateway.config_version", m.TargetTopology)
		}
	default:
		return fmt.Errorf("manifest: unknown target_topology %q", m.TargetTopology)
	}
	if m.WorkloadRef.Name == "" || !versionRe.MatchString(m.WorkloadRef.Version) || m.WorkloadRef.Seed < 0 {
		return errors.New("manifest: workload_ref (name, SemVer version, seed >= 0) is required")
	}
	if m.Engine.Name == "" || m.Engine.Version == "" {
		return errors.New("manifest: engine.name and engine.version are required")
	}
	if m.Engine.Flags == nil {
		return errors.New("manifest: engine.flags is required (empty object = pure defaults)")
	}
	if m.Model.Checkpoint == "" || m.Model.Revision == "" || m.Model.Tokenizer == "" {
		return errors.New("manifest: model.checkpoint, model.revision, and model.tokenizer are required")
	}
	if m.Hardware.InstanceType == "" {
		return errors.New("manifest: hardware.instance_type is required")
	}
	if m.Hardware.GPUCount < 0 {
		return errors.New("manifest: hardware.gpu_count must be >= 0")
	}
	if m.Client.Location == "" {
		return errors.New("manifest: client.location is required")
	}
	switch m.WarmUp.Policy {
	case "none":
	case "discard-duration", "discard-requests":
		if m.WarmUp.Value == nil || *m.WarmUp.Value <= 0 {
			return fmt.Errorf("manifest: warm_up policy %s requires a positive value", m.WarmUp.Policy)
		}
	default:
		return fmt.Errorf("manifest: unknown warm_up.policy %q", m.WarmUp.Policy)
	}
	if m.Repetitions < 1 {
		return errors.New("manifest: repetitions must be >= 1")
	}
	if len(m.Hypothesis) < 10 {
		return errors.New("manifest: hypothesis is required (>= 10 chars, a falsifiable statement)")
	}
	return nil
}

// Write emits the manifest as pretty JSON to path.
func (m *Manifest) Write(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
