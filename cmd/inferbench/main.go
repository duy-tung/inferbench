// Command inferbench is the load-generation CLI: `run` (IB-T002), `sweep`,
// `replay`, `compare` (IB-T008), and `experiment` (IB-T009).
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = cmdRun(os.Args[2:])
	case "sweep":
		err = cmdSweep(os.Args[2:])
	case "replay":
		err = cmdReplay(os.Args[2:])
	case "compare":
		err = cmdCompare(os.Args[2:])
	case "experiment":
		err = cmdExperiment(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "inferbench: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: inferbench <command> [flags]

commands:
  run         one run: workload + target + manifest -> run directory
  sweep       rate sweep: >=6 points, 10%-120% of an estimated capacity
  replay      deterministic re-issue of a recorded workload
  compare     A/B (or N-arm) comparison; refuses multi-variable arm sets
  experiment  hypothesis-gated execution of run/sweep/compare

run 'inferbench <command> -h' for command-specific flags.`)
}
