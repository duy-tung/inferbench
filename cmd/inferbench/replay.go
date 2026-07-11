package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/duy-tung/inferbench/internal/replay"
)

// cmdReplay re-executes a workload file EXACTLY (no --seed/--rate override
// exists here — see internal/replay package doc): it verifies the freshly
// built schedule against a reference recorded by a prior `run`/`replay`
// (reference.json in that run's output directory, or an explicit
// --reference-file), then runs it.
func cmdReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	workloadPath := fs.String("workload", "", "workload file (must be byte-identical to the one that produced --reference)")
	manifestPath := fs.String("manifest", "", "manifest facts file")
	target := fs.String("target", "", "target base URL (Contract 1 endpoint)")
	outDir := fs.String("out", "", "output run directory")
	referenceRun := fs.String("reference-run", "", "prior run directory containing reference.json (mutually exclusive with --reference-file)")
	referenceFile := fs.String("reference-file", "", "explicit reference.json path (mutually exclusive with --reference-run)")
	model := fs.String("model", "mock-8b", "model id sent in every request")
	stream := fs.Bool("stream", false, "use streaming (SSE) requests")
	repetition := fs.Int("repetition", 1, "1-based repetition index recorded in events")
	maxSlip := fs.Duration("max-slip", 100*time.Millisecond, "schedule-slip watchdog threshold")
	reqTimeout := fs.Duration("request-timeout", 60*time.Second, "per-request end-to-end timeout")
	runID := fs.String("run-id", "", "run id (default: derived from workload name + start time)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *workloadPath == "" || *manifestPath == "" || *target == "" || *outDir == "" {
		return errors.New("--workload, --manifest, --target, and --out are required")
	}
	if (*referenceRun == "") == (*referenceFile == "") {
		return errors.New("exactly one of --reference-run or --reference-file is required")
	}
	refPath := *referenceFile
	if *referenceRun != "" {
		refPath = *referenceRun + "/reference.json"
	}
	ref, err := replay.Load(refPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	out, err := runOnce(ctx, onceParams{
		WorkloadPath:    *workloadPath,
		ManifestPath:    *manifestPath,
		Target:          *target,
		OutDir:          *outDir,
		RunID:           *runID,
		Model:           *model,
		Stream:          *stream,
		Repetition:      *repetition,
		MaxSlip:         *maxSlip,
		ReqTimeout:      *reqTimeout,
		ReplayReference: ref,
	})
	if out != nil {
		printResult(out.Manifest.RunID, out.Result)
	}
	if err != nil {
		return err
	}
	fmt.Printf("REPLAY DETERMINISTIC: schedule_fingerprint matches reference (%s)\n", ref.ScheduleFingerprint)
	fmt.Printf("artifacts: %s\n", *outDir)
	return nil
}
