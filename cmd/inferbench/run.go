package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/signal"
	"syscall"
	"time"
)

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	workloadPath := fs.String("workload", "", "workload file (workload.schema.json)")
	manifestPath := fs.String("manifest", "", "manifest facts file (benchmark-run.schema.json fields the tool cannot know)")
	target := fs.String("target", "", "target base URL (Contract 1 endpoint)")
	outDir := fs.String("out", "", "output run directory (manifest.json, events.jsonl, run.log, reference.json)")
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var seedOverride *int64
	if *seed >= 0 {
		seedOverride = seed
	}
	out, err := runOnce(ctx, onceParams{
		WorkloadPath: *workloadPath,
		SeedOverride: seedOverride,
		RateOverride: *rate,
		ManifestPath: *manifestPath,
		Target:       *target,
		OutDir:       *outDir,
		RunID:        *runID,
		Model:        *model,
		Stream:       *stream,
		Repetition:   *repetition,
		MaxSlip:      *maxSlip,
		ReqTimeout:   *reqTimeout,
	})
	if out != nil {
		printResult(out.Manifest.RunID, out.Result)
	}
	if err != nil {
		return err
	}
	fmt.Printf("artifacts: %s\n", *outDir)
	return nil
}
