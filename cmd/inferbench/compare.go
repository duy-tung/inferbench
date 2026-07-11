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
	"strings"
	"syscall"
	"time"

	"github.com/duy-tung/inferbench/internal/manifest"
)

// arm is one parsed `--arm id=factsPath@targetURL` flag value.
type arm struct {
	ID         string
	FactsPath  string
	Target     string
	factsCache *manifest.Manifest
}

func parseArm(spec string) (arm, error) {
	id, rest, ok := strings.Cut(spec, "=")
	if !ok || id == "" {
		return arm{}, fmt.Errorf("compare: --arm %q must be id=factsPath@targetURL", spec)
	}
	factsPath, target, ok := strings.Cut(rest, "@")
	if !ok || factsPath == "" || target == "" {
		return arm{}, fmt.Errorf("compare: --arm %q must be id=factsPath@targetURL", spec)
	}
	return arm{ID: id, FactsPath: factsPath, Target: target}, nil
}

// armsFlag implements flag.Value to collect repeated --arm flags.
type armsFlag struct{ arms *[]arm }

func (f armsFlag) String() string { return "" }
func (f armsFlag) Set(s string) error {
	a, err := parseArm(s)
	if err != nil {
		return err
	}
	*f.arms = append(*f.arms, a)
	return nil
}

// compareFlags are shared by `compare` and `experiment compare`.
type compareFlags struct {
	fs *flag.FlagSet

	workloadPath, variable, outDir, model string
	stream                                bool
	repetitions                           int
	maxSlip, reqTimeout                   time.Duration
	arms                                  []arm
}

func newCompareFlagSet(name string) *compareFlags {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	cf := &compareFlags{fs: fs}
	fs.StringVar(&cf.workloadPath, "workload", "", "workload file, shared byte-for-byte by every arm (same workload/seed)")
	fs.StringVar(&cf.variable, "variable", "", "the single declared manifest field allowed to differ across arms (dotted path, e.g. target_topology)")
	fs.StringVar(&cf.outDir, "out", "", "output comparison directory (comparison.json, <arm-id>/rep-N/)")
	fs.StringVar(&cf.model, "model", "mock-8b", "model id sent in every request")
	fs.BoolVar(&cf.stream, "stream", false, "use streaming (SSE) requests")
	fs.IntVar(&cf.repetitions, "repetitions", 1, "repetitions per arm (experiments.md rule 4 recommends >= 3 for published claims)")
	fs.DurationVar(&cf.maxSlip, "max-slip", 100*time.Millisecond, "schedule-slip watchdog threshold")
	fs.DurationVar(&cf.reqTimeout, "request-timeout", 60*time.Second, "per-request end-to-end timeout")
	fs.Var(armsFlag{&cf.arms}, "arm", "id=factsPath@targetURL; repeat for each arm (>= 2 required)")
	return cf
}

func (cf *compareFlags) validate() error {
	if cf.workloadPath == "" || cf.outDir == "" {
		return errors.New("--workload and --out are required")
	}
	if cf.variable == "" {
		return errors.New("--variable is required (the single declared field arms may differ on)")
	}
	if len(cf.arms) < 2 {
		return errors.New("--arm must be given at least twice (>= 2 arms required for a comparison)")
	}
	seen := map[string]bool{}
	for _, a := range cf.arms {
		if seen[a.ID] {
			return fmt.Errorf("compare: duplicate arm id %q", a.ID)
		}
		seen[a.ID] = true
	}
	if cf.repetitions < 1 {
		return errors.New("--repetitions must be >= 1")
	}
	return nil
}

func cmdCompare(args []string) error {
	cf := newCompareFlagSet("compare")
	if err := cf.fs.Parse(args); err != nil {
		return err
	}
	if err := cf.validate(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cmp, err := runCompare(ctx, cf, nil)
	if cmp != nil {
		fmt.Printf("comparison: variable=%s diff_fields=%v\n", cf.variable, cmp.DiffFields)
		for _, r := range cmp.Arms {
			fmt.Printf("  arm %s: target=%s reps=%d sent=%d ok=%d errors=%d shed=%d canceled=%d\n",
				r.ID, r.Target, r.Repetitions, r.Sent, r.OK, r.Errors, r.Shed, r.Canceled)
		}
	}
	if err != nil {
		return err
	}
	fmt.Printf("comparison manifest: %s\n", filepath.Join(cf.outDir, "comparison.json"))
	return nil
}

// comparisonArmResult is one arm's aggregated outcome in comparison.json.
type comparisonArmResult struct {
	ID          string   `json:"id"`
	Target      string   `json:"target"`
	FactsPath   string   `json:"facts_path"`
	Repetitions int      `json:"repetitions"`
	Sent        int      `json:"sent"`
	OK          int      `json:"ok"`
	Errors      int      `json:"errors"`
	Shed        int      `json:"shed"`
	Canceled    int      `json:"canceled"`
	RunDirs     []string `json:"run_dirs"`
}

// comparison is the comparison-level manifest tying paired arms together
// (docs/tasks.md IB-T008: "emits paired results"). Repo-local JSON format,
// like sweep.Manifest — not a contracts-owned schema.
type comparison struct {
	Workload         string                `json:"workload"`
	WorkloadVersion  string                `json:"workload_version"`
	Seed             int64                 `json:"seed"`
	DeclaredVariable string                `json:"declared_variable"`
	DiffFields       []string              `json:"diff_fields"`
	Arms             []comparisonArmResult `json:"arms"`
	CreatedAt        string                `json:"created_at"`
}

// preValidateArms loads every arm's facts file and refuses (before any
// request is sent) unless the only fields that differ pairwise (vs the
// first arm) are the declared variable and its structurally implied
// companions (manifest.ImpliedFields) — the single-variable rule, checked
// on the FACTS as authored (run_id/started_at/rtt_ms do not exist yet, so
// there is nothing bookkeeping-related to exempt beyond manifest.Diff's
// standing exemptions).
func preValidateArms(arms []arm, variable string) ([]string, error) {
	allowed := map[string]bool{variable: true}
	for _, f := range manifest.ImpliedFields(variable) {
		allowed[f] = true
	}
	for i := range arms {
		m, err := manifest.LoadFacts(arms[i].FactsPath)
		if err != nil {
			return nil, err
		}
		arms[i].factsCache = m
	}
	var allDiffs []string
	seen := map[string]bool{}
	varied := false
	for i := 1; i < len(arms); i++ {
		diffs, err := manifest.Diff(arms[0].factsCache, arms[i].factsCache)
		if err != nil {
			return nil, err
		}
		for _, d := range diffs {
			if !allowed[d] {
				return nil, fmt.Errorf("compare: arm %q differs from arm %q in %q, outside the declared variable %q — no full-matrix comparisons (experiments.md rule 10 / §5)",
					arms[i].ID, arms[0].ID, d, variable)
			}
			if d == variable {
				varied = true
			}
			if !seen[d] {
				seen[d] = true
				allDiffs = append(allDiffs, d)
			}
		}
	}
	if !varied {
		return nil, fmt.Errorf("compare: declared variable %q never actually differs across arms — not a real comparison", variable)
	}
	return allDiffs, nil
}

// runCompare validates every arm's facts (refusing before any traffic on a
// multi-variable arm set), then executes each arm's repetitions. hypothesis
// is nil for plain `compare`; `experiment compare` passes its loaded
// hypothesis so the run_id / manifest.Hypothesis field can cite it (kept
// generic here — the governance CHECK itself lives in cmdExperimentCompare).
func runCompare(ctx context.Context, cf *compareFlags, idPrefix *string) (*comparison, error) {
	diffFields, err := preValidateArms(cf.arms, cf.variable)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(cf.outDir, 0o755); err != nil {
		return nil, err
	}
	cmp := &comparison{
		DeclaredVariable: cf.variable,
		DiffFields:       diffFields,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	prefix := "cmp"
	if idPrefix != nil {
		prefix = *idPrefix
	}

	for _, a := range cf.arms {
		armDir := filepath.Join(cf.outDir, a.ID)
		res := comparisonArmResult{ID: a.ID, Target: a.Target, FactsPath: a.FactsPath, Repetitions: cf.repetitions}
		for rep := 1; rep <= cf.repetitions; rep++ {
			repDir := filepath.Join(armDir, fmt.Sprintf("rep-%d", rep))
			runID := fmt.Sprintf("%s-%s-r%d", prefix, a.ID, rep)
			out, err := runOnce(ctx, onceParams{
				WorkloadPath: cf.workloadPath,
				ManifestPath: a.FactsPath,
				Target:       a.Target,
				OutDir:       repDir,
				RunID:        runID,
				Model:        cf.model,
				Stream:       cf.stream,
				Repetition:   rep,
				MaxSlip:      cf.maxSlip,
				ReqTimeout:   cf.reqTimeout,
			})
			if err != nil {
				cmp.Arms = append(cmp.Arms, res)
				_ = writeComparison(cf.outDir, cmp)
				return cmp, fmt.Errorf("arm %s rep %d: %w", a.ID, rep, err)
			}
			if cmp.Workload == "" {
				cmp.Workload = out.Workload.Name
				cmp.WorkloadVersion = out.Workload.Version
				cmp.Seed = *out.Workload.Seed
			}
			res.RunDirs = append(res.RunDirs, repDir)
			res.Sent += out.Result.Sent
			res.OK += out.Result.OK
			res.Errors += out.Result.Errors
			res.Shed += out.Result.Shed
			res.Canceled += out.Result.Canceled
		}
		cmp.Arms = append(cmp.Arms, res)
	}
	if err := writeComparison(cf.outDir, cmp); err != nil {
		return cmp, err
	}
	return cmp, nil
}

func writeComparison(outDir string, cmp *comparison) error {
	data, err := json.MarshalIndent(cmp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "comparison.json"), append(data, '\n'), 0o644)
}
