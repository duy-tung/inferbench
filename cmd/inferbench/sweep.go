package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/duy-tung/inferbench/internal/sweep"
	"github.com/duy-tung/inferbench/internal/workload"
)

// sweepFlags are shared by `sweep` and `experiment sweep`.
type sweepFlags struct {
	fs *flag.FlagSet

	workloadPath, manifestPath, target, outDir, model string
	stream                                            bool
	maxSlip, reqTimeout                               time.Duration
	maxConns                                          int
	repetitions                                       int
	points                                            int
	minFraction, maxFraction                          float64
	probeRate                                         float64
	probeRequests                                     int
	probeMaxSlip                                      time.Duration
}

func newSweepFlagSet(name string) *sweepFlags {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	sf := &sweepFlags{fs: fs}
	fs.StringVar(&sf.workloadPath, "workload", "", "base workload file (single-rate open-loop-poisson)")
	fs.StringVar(&sf.manifestPath, "manifest", "", "manifest facts file, shared by the probe and every point")
	fs.StringVar(&sf.target, "target", "", "target base URL (Contract 1 endpoint)")
	fs.StringVar(&sf.outDir, "out", "", "output sweep directory (sweep.json, probe/, point-N/)")
	fs.StringVar(&sf.model, "model", "mock-8b", "model id sent in every request")
	fs.BoolVar(&sf.stream, "stream", false, "use streaming (SSE) requests")
	fs.DurationVar(&sf.maxSlip, "max-slip", 2*time.Second, "schedule-slip watchdog threshold for sweep points (raised above run's default: overload points intentionally accumulate wire-stage slip -- ADR-0001's at-saturation caveat -- the slip is recorded per event either way)")
	fs.DurationVar(&sf.reqTimeout, "request-timeout", 60*time.Second, "per-request end-to-end timeout")
	fs.IntVar(&sf.maxConns, "max-conns", 8, "target concurrency-cap model held fixed across the probe and every point (see internal/sweep package doc: the mock/gateway pair has no admission control of its own at the pinned build, so this bounds the client's connection pool to model a capacity-limited target; required > 0)")
	fs.IntVar(&sf.repetitions, "repetitions", 3, "repetitions per point (experiments.md rule 4: >= 3)")
	fs.IntVar(&sf.points, "points", sweep.MinPoints, "number of sweep points (>= 6, experiments.md rule 3)")
	fs.Float64Var(&sf.minFraction, "min-fraction", 0.10, "low end of the sweep range as a fraction of estimated capacity")
	fs.Float64Var(&sf.maxFraction, "max-fraction", 1.20, "high end of the sweep range as a fraction of estimated capacity")
	fs.Float64Var(&sf.probeRate, "probe-rate", 200, "offered rate (rps) of the capacity-estimate overload probe -- must comfortably exceed any plausible capacity")
	fs.IntVar(&sf.probeRequests, "probe-requests", 150, "request_count of the capacity-estimate probe")
	fs.DurationVar(&sf.probeMaxSlip, "probe-max-slip", 10*time.Second, "schedule-slip watchdog threshold for the probe (it is EXPECTED to fall far behind its own offered rate)")
	return sf
}

func (sf *sweepFlags) validate() error {
	if sf.workloadPath == "" || sf.manifestPath == "" || sf.target == "" || sf.outDir == "" {
		return errors.New("--workload, --manifest, --target, and --out are required")
	}
	if sf.maxConns <= 0 {
		return errors.New("--max-conns must be > 0 (the sweep's capacity model requires a held-fixed concurrency cap)")
	}
	if sf.repetitions < 1 {
		return errors.New("--repetitions must be >= 1")
	}
	return nil
}

func cmdSweep(args []string) error {
	sf := newSweepFlagSet("sweep")
	if err := sf.fs.Parse(args); err != nil {
		return err
	}
	if err := sf.validate(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	man, err := runSweep(ctx, sf)
	if man != nil {
		fmt.Printf("sweep: capacity_estimate=%.4f rps (probe achieved %.4f/%.4f offered)\n",
			man.CapacityEstimateRPS, man.Probe.AchievedRPS, man.Probe.OfferedRateRPS)
		for _, pt := range man.Points {
			fmt.Printf("  point %d: rate=%.4f rps (%.0f%% of capacity) reps=%d sent=%d ok=%d errors=%d shed=%d canceled=%d\n",
				pt.Index, pt.RateRPS, pt.FractionOfCapacity*100, pt.Repetitions, pt.Sent, pt.OK, pt.Errors, pt.Shed, pt.Canceled)
		}
	}
	if err != nil {
		return err
	}
	fmt.Printf("sweep manifest: %s\n", filepath.Join(sf.outDir, "sweep.json"))
	return nil
}

// runSweep is the sweep orchestration shared by `sweep` and `experiment
// sweep`. It ALWAYS writes sweep.json (even on a later point's error, so
// partial evidence is never lost) if the probe succeeded.
func runSweep(ctx context.Context, sf *sweepFlags) (*sweep.Manifest, error) {
	base, err := workload.Load(sf.workloadPath)
	if err != nil {
		return nil, err
	}
	if base.Arrival.Type != workload.ArrivalOpenLoopPoisson || base.Arrival.RateRPS == nil {
		return nil, errors.New("sweep: base workload must be single-rate open-loop-poisson")
	}
	if err := os.MkdirAll(sf.outDir, 0o755); err != nil {
		return nil, err
	}

	man := &sweep.Manifest{
		SweepID:            filepath.Base(sf.outDir),
		BaseWorkload:       base.Name,
		WorkloadVersion:    base.Version,
		Seed:               *base.Seed,
		DeclaredVariable:   sweep.DeclaredVariable,
		ConcurrencyCap:     sf.maxConns,
		ConcurrencyCapNote: "client-transport MaxConnsPerHost, held fixed across the probe and every point: the mock/gateway pair has no admission control of its own at the pinned build (docs/implementation-notes.md), so this models the target's effective capacity for the purpose of this verification sweep. Go's http.Transport blocks (queues) requests past this cap instead of failing them, producing genuine client-visible queueing delay that counts against latency via the scheduled-send basis (ADR-0001).",
		Target:             sf.target,
		MinFraction:        sf.minFraction,
		MaxFraction:        sf.maxFraction,
		CreatedAt:          time.Now().UTC().Format(time.RFC3339),
	}

	// --- capacity probe (ADR-0003: sanctioned for ceiling discovery / sweep-range placement only) ---
	probeWL, err := sweep.DeriveRateWorkload(base, sf.probeRate, base.Name+"-capacity-probe")
	if err != nil {
		return nil, err
	}
	probeWL.Stop = &workload.Stop{RequestCount: intPtr(sf.probeRequests)}
	probeWLPath, err := writeWorkload(sf.outDir, "probe-workload.json", probeWL)
	if err != nil {
		return nil, err
	}
	probeDir := filepath.Join(sf.outDir, "probe")
	probeOut, probeErr := runOnce(ctx, onceParams{
		WorkloadPath: probeWLPath,
		ManifestPath: sf.manifestPath,
		Target:       sf.target,
		OutDir:       probeDir,
		RunID:        man.SweepID + "-probe",
		Model:        sf.model,
		Stream:       sf.stream,
		Repetition:   1,
		MaxSlip:      sf.probeMaxSlip,
		ReqTimeout:   sf.reqTimeout,
		ClientOpts:   clientOpts{MaxConnsPerHost: sf.maxConns},
	})
	if probeErr != nil {
		return nil, fmt.Errorf("capacity probe: %w", probeErr)
	}
	elapsed := probeOut.Result.Finished.Sub(probeOut.Result.Started).Seconds()
	capacity, err := sweep.EstimateCapacity(probeOut.Result.OK, elapsed, sf.probeRate)
	man.Probe = sweep.Probe{
		OfferedRateRPS: sf.probeRate,
		RequestCount:   sf.probeRequests,
		OKCount:        probeOut.Result.OK,
		ElapsedSeconds: elapsed,
		AchievedRPS:    capacity,
		RunDir:         probeDir,
		Method:         "open-loop overload probe (ADR-0003): achieved_rps = ok_count / elapsed_seconds at an offered rate far above any plausible capacity",
	}
	if err != nil {
		man.CapacityEstimateRPS = capacity
		_ = writeSweepManifest(sf.outDir, man)
		return man, err
	}
	man.CapacityEstimateRPS = capacity

	rates, err := sweep.RatePoints(capacity, sf.minFraction, sf.maxFraction, sf.points)
	if err != nil {
		_ = writeSweepManifest(sf.outDir, man)
		return man, err
	}

	var derivedWorkloads []*workload.Workload
	for i, rate := range rates {
		name := fmt.Sprintf("%s-sweep-p%d", base.Name, i)
		w, err := sweep.DeriveRateWorkload(base, rate, name)
		if err != nil {
			_ = writeSweepManifest(sf.outDir, man)
			return man, err
		}
		derivedWorkloads = append(derivedWorkloads, w)
	}
	if err := sweep.CheckSingleVariable(base, derivedWorkloads); err != nil {
		_ = writeSweepManifest(sf.outDir, man)
		return man, err
	}

	for i, w := range derivedWorkloads {
		pointDir := filepath.Join(sf.outDir, fmt.Sprintf("point-%d", i))
		wPath, err := writeWorkload(sf.outDir, fmt.Sprintf("point-%d-workload.json", i), w)
		if err != nil {
			man.Points = append(man.Points, sweep.Point{Index: i, RateRPS: rates[i]})
			_ = writeSweepManifest(sf.outDir, man)
			return man, err
		}
		pt := sweep.Point{
			Index:              i,
			RateRPS:            rates[i],
			FractionOfCapacity: rates[i] / capacity,
			RunDir:             pointDir,
			Repetitions:        sf.repetitions,
		}
		for rep := 1; rep <= sf.repetitions; rep++ {
			repDir := filepath.Join(pointDir, fmt.Sprintf("rep-%d", rep))
			runID := fmt.Sprintf("%s-p%d-r%d", man.SweepID, i, rep)
			out, err := runOnce(ctx, onceParams{
				WorkloadPath: wPath,
				ManifestPath: sf.manifestPath,
				Target:       sf.target,
				OutDir:       repDir,
				RunID:        runID,
				Model:        sf.model,
				Stream:       sf.stream,
				Repetition:   rep,
				MaxSlip:      sf.maxSlip,
				ReqTimeout:   sf.reqTimeout,
				ClientOpts:   clientOpts{MaxConnsPerHost: sf.maxConns},
			})
			if err != nil {
				man.Points = append(man.Points, pt)
				_ = writeSweepManifest(sf.outDir, man)
				return man, fmt.Errorf("point %d rep %d: %w", i, rep, err)
			}
			pt.RunID = runID
			pt.Sent += out.Result.Sent
			pt.OK += out.Result.OK
			pt.Errors += out.Result.Errors
			pt.Shed += out.Result.Shed
			pt.Canceled += out.Result.Canceled
		}
		man.Points = append(man.Points, pt)
	}

	if err := writeSweepManifest(sf.outDir, man); err != nil {
		return man, err
	}
	return man, nil
}

func writeSweepManifest(outDir string, man *sweep.Manifest) error {
	data, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "sweep.json"), append(data, '\n'), 0o644)
}

// writeWorkload serializes a derived workload next to the sweep's other
// artifacts so it is inspectable evidence, not a throwaway temp file.
func writeWorkload(outDir, name string, w *workload.Workload) (string, error) {
	path := filepath.Join(outDir, name)
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return "", fmt.Errorf("sweep: marshaling derived workload: %w", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func intPtr(n int) *int { return &n }
