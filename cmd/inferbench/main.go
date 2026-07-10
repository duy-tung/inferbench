// Command inferbench is the load-generation CLI (IB-T002 scope: the `run`
// subcommand; sweep/replay/compare/experiment land at IB-T008/IB-T009).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/duy-tung/inferbench/internal/client"
	"github.com/duy-tung/inferbench/internal/manifest"
	"github.com/duy-tung/inferbench/internal/run"
	"github.com/duy-tung/inferbench/internal/schedule"
	"github.com/duy-tung/inferbench/internal/workload"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "run" {
		fmt.Fprintln(os.Stderr, "usage: inferbench run --workload <file> --manifest <facts.json> --target <url> --out <dir> [--seed n] [--rate rps] [--model id] [--stream] [--repetition n] [--max-slip d] [--request-timeout d] [--run-id id]")
		os.Exit(2)
	}
	if err := cmdRun(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "inferbench: %v\n", err)
		os.Exit(1)
	}
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	workloadPath := fs.String("workload", "", "workload file (workload.schema.json)")
	manifestPath := fs.String("manifest", "", "manifest facts file (benchmark-run.schema.json fields the tool cannot know)")
	target := fs.String("target", "", "target base URL (Contract 1 endpoint)")
	outDir := fs.String("out", "", "output run directory (manifest.json, events.jsonl, run.log)")
	seed := fs.Int64("seed", -1, "override the workload seed (recorded in workload_ref.seed)")
	rate := fs.Float64("rate", 0, "override open-loop-poisson rate_rps (single-rate workloads only)")
	model := fs.String("model", "mock-8b", "model id sent in every request")
	stream := fs.Bool("stream", false, "use streaming (SSE) requests with stream_options.include_usage")
	repetition := fs.Int("repetition", 1, "1-based repetition index recorded in events")
	maxSlip := fs.Duration("max-slip", 100*time.Millisecond, "schedule-slip watchdog threshold (run INVALID beyond it)")
	reqTimeout := fs.Duration("request-timeout", 60*time.Second, "per-request end-to-end timeout")
	runID := fs.String("run-id", "", "run id (default: derived from workload name + start time)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *workloadPath == "" || *manifestPath == "" || *target == "" || *outDir == "" {
		return errors.New("--workload, --manifest, --target, and --out are required")
	}
	if *repetition < 1 {
		return errors.New("--repetition must be >= 1")
	}

	w, err := workload.Load(*workloadPath)
	if err != nil {
		return err
	}
	if *seed >= 0 {
		w.Seed = seed
	}
	if *rate > 0 {
		if w.Arrival.Type != workload.ArrivalOpenLoopPoisson || w.Arrival.RateRPS == nil {
			return errors.New("--rate override requires a single-rate open-loop-poisson workload")
		}
		w.Arrival.RateRPS = rate
	}
	// The output-tokens cancellation trigger counts content deltas as they
	// stream in; without --stream there is nothing to count until the full
	// body has already arrived, so running it non-streaming would silently
	// realize zero cancellations. Refuse instead (typed, never silent).
	if *w.Cancel.Rate > 0 && w.Cancel.Point.Trigger == workload.CancelTriggerTokens && !*stream {
		return errors.New("cancellation trigger output-tokens requires --stream")
	}

	plan, err := schedule.Build(w)
	if err != nil {
		return err
	}

	m, err := manifest.LoadFacts(*manifestPath)
	if err != nil {
		return err
	}
	start := time.Now().UTC()
	if *runID == "" {
		*runID = fmt.Sprintf("%s-%s", w.Name, start.Format("20060102-150405"))
	}
	m.RunID = *runID
	m.WorkloadRef = manifest.WorkloadRef{Name: w.Name, Version: w.Version, Seed: *w.Seed}
	m.StartedAt = start.Format(time.RFC3339)
	m.ContractsBundleVersion = manifest.ContractsBundleVersion

	// Preflight: the target must be reachable before the first scheduled
	// send (typed abort target_unreachable otherwise), and the healthz
	// round trip measures client RTT when the facts file left it null.
	rtt, err := preflight(*target)
	if err != nil {
		return fmt.Errorf("%w: %v", run.ErrTargetUnreachable, err)
	}
	if m.Client.RTTms == nil {
		ms := float64(rtt) / float64(time.Millisecond)
		m.Client.RTTms = &ms
	}
	if err := m.Validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	if err := m.Write(filepath.Join(*outDir, "manifest.json")); err != nil {
		return err
	}
	eventsFile, err := os.Create(filepath.Join(*outDir, "events.jsonl"))
	if err != nil {
		return err
	}
	defer eventsFile.Close()
	logFile, err := os.Create(filepath.Join(*outDir, "run.log"))
	if err != nil {
		return err
	}
	defer logFile.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cl := client.New(client.Config{
		BaseURL:        *target,
		Model:          *model,
		Stream:         *stream,
		RequestTimeout: *reqTimeout,
	})

	res, runErr := run.Execute(ctx, run.Options{
		Plan:       plan,
		Client:     cl,
		RunID:      *runID,
		Repetition: *repetition,
		MaxSlip:    *maxSlip,
		EventSink:  eventsFile,
		Log:        logFile,
	})
	if res != nil {
		fmt.Printf("run %s: sent=%d ok=%d errors=%d shed=%d canceled=%d max_dispatch_slip=%s max_send_slip=%s wall=%s\n",
			*runID, res.Sent, res.OK, res.Errors, res.Shed, res.Canceled,
			res.MaxDispatchSlip, res.MaxSendSlip,
			res.Finished.Sub(res.Started).Round(time.Millisecond))
		if res.UsageMissing > 0 {
			fmt.Printf("warning: %d responses carried no usage payload; their token counts are client-side chunk counts (or 0)\n", res.UsageMissing)
		}
	}
	if runErr != nil {
		return fmt.Errorf("run INVALID: %w", runErr)
	}
	fmt.Printf("artifacts: %s\n", *outDir)
	return nil
}

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
