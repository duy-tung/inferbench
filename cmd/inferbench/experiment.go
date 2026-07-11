package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/duy-tung/inferbench/internal/experiment"
	"github.com/duy-tung/inferbench/internal/manifest"
	"github.com/duy-tung/inferbench/internal/sweep"
)

// cmdExperiment is the G6 enforcement gate: every mode below loads and
// validates a hypothesis file FIRST — before any workload/manifest/target
// flag is even consulted for reachability — and refuses (typed,
// experiment.ErrHypothesisRequired) if one is not given. This is the
// mechanism experiments.md §5 calls for: "the framework rejects
// hypothesis-less runs".
func cmdExperiment(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: inferbench experiment <run|sweep|compare> --hypothesis FILE [mode flags]")
	}
	mode, rest := args[0], args[1:]
	switch mode {
	case "run":
		return cmdExperimentRun(rest)
	case "sweep":
		return cmdExperimentSweep(rest)
	case "compare":
		return cmdExperimentCompare(rest)
	default:
		return fmt.Errorf("experiment: unknown mode %q (want run|sweep|compare)", mode)
	}
}

// extractHypothesisFlag pulls --hypothesis out of args (it is common to all
// three modes but each mode's flag.FlagSet also defines its own,
// mode-specific flags, so a single shared FlagSet cannot parse the full
// line) and loads it BEFORE any mode-specific flag is even parsed, so a
// hypothesis-less invocation is refused even if the rest of the command
// line is garbage. This is the G6 gate: experiment.Load refuses an empty,
// missing, or incomplete hypothesis file with a typed error.
func extractHypothesisFlag(args []string) (*experiment.Hypothesis, []string, error) {
	path := extractFlagValue(args, "hypothesis")
	h, err := experiment.Load(path)
	if err != nil {
		return nil, nil, err
	}
	return h, args, nil
}

// extractFlagValue scans args for -name/--name value or -name=value/--name=value
// without needing a full flag.FlagSet pass (mode flag sets parse the full
// arg list separately afterward; this is just an early, additive peek).
func extractFlagValue(args []string, name string) string {
	for i, a := range args {
		for _, prefix := range []string{"-" + name + "=", "--" + name + "="} {
			if len(a) > len(prefix) && a[:len(prefix)] == prefix {
				return a[len(prefix):]
			}
		}
		if (a == "-"+name || a == "--"+name) && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func cmdExperimentRun(args []string) error {
	h, args, err := extractHypothesisFlag(args)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("experiment run", flag.ExitOnError)
	_ = fs.String("hypothesis", "", "hypothesis file (JSON; required)")
	workloadPath := fs.String("workload", "", "workload file")
	manifestPath := fs.String("manifest", "", "manifest facts file")
	target := fs.String("target", "", "target base URL")
	outDir := fs.String("out", "", "output run directory")
	model := fs.String("model", "mock-8b", "model id")
	stream := fs.Bool("stream", false, "use streaming (SSE) requests")
	repetition := fs.Int("repetition", 1, "1-based repetition index")
	runID := fs.String("run-id", "", "run id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *workloadPath == "" || *manifestPath == "" || *target == "" || *outDir == "" {
		return errors.New("--workload, --manifest, --target, and --out are required")
	}

	facts, err := manifest.LoadFacts(*manifestPath)
	if err != nil {
		return err
	}
	if err := h.CheckGPUSession([]*manifest.Manifest{facts}); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	out, err := runOnce(ctx, onceParams{
		WorkloadPath: *workloadPath,
		ManifestPath: *manifestPath,
		Target:       *target,
		OutDir:       *outDir,
		RunID:        *runID,
		Model:        *model,
		Stream:       *stream,
		Repetition:   *repetition,
		MaxSlip:      0,
		ReqTimeout:   0,
	})
	if out != nil {
		printResult(out.Manifest.RunID, out.Result)
	}
	if err != nil {
		return err
	}
	fmt.Printf("experiment %s (hypothesis %s) OK: artifacts %s\n", *runID, h.ID, *outDir)
	return nil
}

func cmdExperimentSweep(args []string) error {
	h, args, err := extractHypothesisFlag(args)
	if err != nil {
		return err
	}
	if h.Variable != sweep.DeclaredVariable {
		return fmt.Errorf("experiment: hypothesis declares variable %q but a sweep's only variable is %q", h.Variable, sweep.DeclaredVariable)
	}

	sf := newSweepFlagSet("experiment sweep")
	_ = sf.fs.String("hypothesis", "", "hypothesis file (JSON; required)")
	if err := sf.fs.Parse(args); err != nil {
		return err
	}
	if err := sf.validate(); err != nil {
		return err
	}

	facts, err := manifest.LoadFacts(sf.manifestPath)
	if err != nil {
		return err
	}
	if err := h.CheckGPUSession([]*manifest.Manifest{facts}); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	man, err := runSweep(ctx, sf)
	if man != nil {
		fmt.Printf("experiment sweep (hypothesis %s): capacity_estimate=%.4f rps, %d points\n", h.ID, man.CapacityEstimateRPS, len(man.Points))
	}
	if err != nil {
		return err
	}
	fmt.Printf("sweep manifest: %s/sweep.json\n", sf.outDir)
	return nil
}

func cmdExperimentCompare(args []string) error {
	h, args, err := extractHypothesisFlag(args)
	if err != nil {
		return err
	}

	cf := newCompareFlagSet("experiment compare")
	_ = cf.fs.String("hypothesis", "", "hypothesis file (JSON; required)")
	if err := cf.fs.Parse(args); err != nil {
		return err
	}
	if err := cf.validate(); err != nil {
		return err
	}
	if h.Variable != cf.variable {
		return fmt.Errorf("experiment: hypothesis declares variable %q but --variable is %q; they must match", h.Variable, cf.variable)
	}

	arms := make([]*manifest.Manifest, len(cf.arms))
	for i, a := range cf.arms {
		m, err := manifest.LoadFacts(a.FactsPath)
		if err != nil {
			return err
		}
		arms[i] = m
	}
	if err := h.CheckArms(arms); err != nil {
		return err
	}
	if err := h.CheckGPUSession(arms); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	prefix := "exp-" + h.ID
	cmp, err := runCompare(ctx, cf, &prefix)
	if cmp != nil {
		fmt.Printf("experiment compare (hypothesis %s): variable=%s diff_fields=%v\n", h.ID, cf.variable, cmp.DiffFields)
		for _, r := range cmp.Arms {
			fmt.Printf("  arm %s: target=%s reps=%d sent=%d ok=%d errors=%d shed=%d canceled=%d\n",
				r.ID, r.Target, r.Repetitions, r.Sent, r.OK, r.Errors, r.Shed, r.Canceled)
		}
	}
	if err != nil {
		return err
	}
	fmt.Printf("comparison manifest: %s/comparison.json\n", cf.outDir)
	return nil
}
