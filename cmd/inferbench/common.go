// Shared helpers used by every subcommand: preflight, manifest completion,
// and one-run execution. Factored out of the original `run` implementation
// so `sweep`, `compare`, `replay`, and `experiment` execute exactly the
// same run mechanics `run` does — there is only one way this binary sends a
// request.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/duy-tung/inferbench/internal/client"
	"github.com/duy-tung/inferbench/internal/manifest"
	"github.com/duy-tung/inferbench/internal/replay"
	"github.com/duy-tung/inferbench/internal/run"
	"github.com/duy-tung/inferbench/internal/schedule"
	"github.com/duy-tung/inferbench/internal/workload"
)

// preflight checks reachability via /healthz (any HTTP response counts as
// reachable — only transport failures abort) and returns the round-trip
// time of the successful probe.
func preflight(target string) (time.Duration, error) {
	c := &http.Client{Timeout: 5 * time.Second}
	t0 := time.Now()
	resp, err := c.Get(target + "/healthz")
	if err != nil {
		// Some OpenAI-compatible targets have no /healthz; try /v1/models
		// before declaring the target unreachable.
		resp, err = c.Get(target + "/v1/models")
		if err != nil {
			return 0, err
		}
	}
	rtt := time.Since(t0)
	resp.Body.Close()
	return rtt, nil
}

// clientOpts configures the underlying HTTP transport beyond
// client.Config's exposed fields. MaxConnsPerHost > 0 imposes a hard
// concurrency cap on the connection pool to the target — the mock/target
// capacity model `sweep` and its probe use (see internal/sweep package
// doc and docs/evidence/ib-t008/README.md): the mock backend and gateway at
// the pinned build have no admission control or concurrency limiter of
// their own (verified in docs/implementation-notes.md), so without a
// held-fixed capacity model of some kind, offered rate can never exceed a
// target's real capacity and no saturation knee would ever appear.
// Go's http.Transport blocks (queues) new requests once MaxConnsPerHost is
// reached rather than failing them, so this produces genuine queueing delay
// that counts against latency via the scheduled-send basis (ADR-0001's
// "at-saturation caveat"), not a fabricated one.
type clientOpts struct {
	MaxConnsPerHost int
}

func newHTTPClient(opts clientOpts) *http.Client {
	if opts.MaxConnsPerHost <= 0 {
		return nil // client.New supplies its own unlimited-pool default
	}
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: opts.MaxConnsPerHost,
			MaxConnsPerHost:     opts.MaxConnsPerHost,
		},
	}
}

// onceParams configures exactly one prepared-and-executed run: the common
// path every subcommand funnels through.
type onceParams struct {
	WorkloadPath string
	// SeedOverride, when non-nil, replaces the workload's declared seed.
	// A pointer (not a sentinel int, e.g. "< 0 means no override") is
	// deliberate: an int sentinel is exactly the bug class that shipped
	// here once already (see git history / implementation-notes.md IB-T008)
	// — every call site that forgot to set it got the Go zero value (0),
	// silently overriding the seed to 0 instead of leaving it alone. A
	// pointer's zero value (nil) IS "no override", so forgetting to set it
	// is safe by construction.
	SeedOverride *int64
	RateOverride float64 // <= 0 = no override

	ManifestPath string
	Target       string
	OutDir       string
	RunID        string

	Model      string
	Stream     bool
	Repetition int
	MaxSlip    time.Duration
	ReqTimeout time.Duration
	ClientOpts clientOpts

	// ReplayReference, when non-nil, must match the freshly built plan
	// exactly (typed refusal otherwise) before any request is sent.
	ReplayReference *replay.Reference
}

// onceOutcome is everything a caller might want after one prepared run.
type onceOutcome struct {
	Workload *workload.Workload
	Plan     *schedule.Plan
	Manifest *manifest.Manifest
	Result   *run.Result
}

// runOnce loads the workload + manifest facts, builds the plan, preflights
// the target, verifies a replay reference when supplied, writes
// manifest.json + reference.json into OutDir, and executes the run —
// exactly what `cmdRun` did, now shared by every subcommand.
func runOnce(ctx context.Context, p onceParams) (*onceOutcome, error) {
	w, err := workload.Load(p.WorkloadPath)
	if err != nil {
		return nil, err
	}
	if p.SeedOverride != nil {
		w.Seed = p.SeedOverride
	}
	if p.RateOverride > 0 {
		if w.Arrival.Type != workload.ArrivalOpenLoopPoisson || w.Arrival.RateRPS == nil {
			return nil, fmt.Errorf("--rate override requires a single-rate open-loop-poisson workload")
		}
		w.Arrival.RateRPS = &p.RateOverride
	}
	if *w.Cancel.Rate > 0 && w.Cancel.Point.Trigger == workload.CancelTriggerTokens && !p.Stream {
		return nil, fmt.Errorf("cancellation trigger output-tokens requires --stream")
	}

	plan, err := schedule.Build(w)
	if err != nil {
		return nil, err
	}

	if p.ReplayReference != nil {
		if err := p.ReplayReference.Verify(w, plan); err != nil {
			return nil, err
		}
	}

	m, err := manifest.LoadFacts(p.ManifestPath)
	if err != nil {
		return nil, err
	}
	start := time.Now().UTC()
	runID := p.RunID
	if runID == "" {
		runID = fmt.Sprintf("%s-%s", w.Name, start.Format("20060102-150405"))
	}
	m.RunID = runID
	m.WorkloadRef = manifest.WorkloadRef{Name: w.Name, Version: w.Version, Seed: *w.Seed}
	m.StartedAt = start.Format(time.RFC3339)
	m.ContractsBundleVersion = manifest.ContractsBundleVersion

	rtt, err := preflight(p.Target)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", run.ErrTargetUnreachable, err)
	}
	if m.Client.RTTms == nil {
		ms := float64(rtt) / float64(time.Millisecond)
		m.Client.RTTms = &ms
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(p.OutDir, 0o755); err != nil {
		return nil, err
	}
	if err := m.Write(filepath.Join(p.OutDir, "manifest.json")); err != nil {
		return nil, err
	}
	ref := replay.BuildReference(w, plan)
	if err := ref.Write(filepath.Join(p.OutDir, "reference.json")); err != nil {
		return nil, err
	}
	eventsFile, err := os.Create(filepath.Join(p.OutDir, "events.jsonl"))
	if err != nil {
		return nil, err
	}
	defer eventsFile.Close()
	logFile, err := os.Create(filepath.Join(p.OutDir, "run.log"))
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	cl := client.New(client.Config{
		BaseURL:        p.Target,
		Model:          p.Model,
		Stream:         p.Stream,
		RequestTimeout: p.ReqTimeout,
		HTTPClient:     newHTTPClient(p.ClientOpts),
	})

	maxSlip := p.MaxSlip
	if maxSlip <= 0 {
		maxSlip = 100 * time.Millisecond
	}
	res, runErr := run.Execute(ctx, run.Options{
		Plan:       plan,
		Client:     cl,
		RunID:      runID,
		Repetition: p.Repetition,
		MaxSlip:    maxSlip,
		EventSink:  eventsFile,
		Log:        logFile,
	})
	out := &onceOutcome{Workload: w, Plan: plan, Manifest: m, Result: res}
	if runErr != nil {
		return out, fmt.Errorf("run INVALID: %w", runErr)
	}
	return out, nil
}

func printResult(runID string, res *run.Result) {
	if res == nil {
		return
	}
	fmt.Printf("run %s: sent=%d ok=%d errors=%d shed=%d canceled=%d max_dispatch_slip=%s max_send_slip=%s wall=%s\n",
		runID, res.Sent, res.OK, res.Errors, res.Shed, res.Canceled,
		res.MaxDispatchSlip, res.MaxSendSlip,
		res.Finished.Sub(res.Started).Round(time.Millisecond))
	if res.UsageMissing > 0 {
		fmt.Printf("warning: %d responses carried no usage payload; their token counts are client-side chunk counts (or 0)\n", res.UsageMissing)
	}
}
