// Command togo-tape is a developer tool, not shipped. It runs the real
// engine over generated agent sessions and writes what the app would see
// as tapes the app's strict replay plays back:
//
//	TOGO_TAPE_REVISION=$(git rev-parse HEAD) togo-tape -scenario all -agent all -out /tmp/tapes
//
// For each scenario and agent it writes <out>/<agent>/<scenario>.jsonl
// and a manifest <scenario>.json, and prints one line per pair: written,
// skipped with the reason, or failed with the reason. It exits 1 when any
// pair failed and 2 when it could not start. A tape is written only with
// the commit it was made from in TOGO_TAPE_REVISION and from a clean
// tree, unless TOGO_TAPE_ALLOW_DIRTY=1.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest/tape"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("togo-tape", flag.ContinueOnError)
	var scenario, agent, out string
	var pilot, verbose bool
	fs.StringVar(&scenario, "scenario", "all", "scenario id, or all: "+strings.Join(ids(), ", "))
	fs.StringVar(&agent, "agent", "all", "agent profile, or all: "+strings.Join(sessiongen.Names(), ", "))
	fs.StringVar(&out, "out", "", "folder the tapes are written to (required)")
	fs.BoolVar(&pilot, "pilot", false, "write only long-chat-restart for omp, as engine-restart.jsonl and engine-restart.json directly in -out")
	fs.BoolVar(&verbose, "v", false, "print the engine's log")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if out == "" {
		fmt.Fprintln(os.Stderr, "togo-tape: -out is required")
		return 2
	}
	if pilot {
		scenario, agent = "long-chat-restart", "omp"
	}
	list, err := pick(scenario, agent)
	if err != nil {
		fmt.Fprintln(os.Stderr, "togo-tape:", err)
		return 2
	}
	out, err = filepath.Abs(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "togo-tape:", err)
		return 2
	}
	helper := coverage.HelperDir()
	if helper == "" {
		fmt.Fprintln(os.Stderr, "togo-tape: the source tree is not on disk")
		return 2
	}
	src, err := tape.SourceFromEnv(helper)
	if err != nil {
		fmt.Fprintln(os.Stderr, "togo-tape:", err)
		return 2
	}
	// The rig reads the adapters' files and the detector's captures from
	// helper/internal.
	if err := os.Chdir(filepath.Join(helper, "internal")); err != nil {
		fmt.Fprintln(os.Stderr, "togo-tape:", err)
		return 2
	}
	scratch, err := os.MkdirTemp("", "togo-tape-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "togo-tape:", err)
		return 2
	}
	defer os.RemoveAll(scratch)
	runner := tape.Runner{Scratch: scratch, Verbose: verbose}
	failed := 0
	for _, p := range list {
		var result *tape.Tape
		err := runner.Do(p.agent+"/"+p.sc.ID, func(t testing.TB) error {
			var err error
			result, err = tape.Export(t, p.sc, p.agent)
			return err
		})
		var skip tape.Skip
		switch {
		case errors.As(err, &skip):
			fmt.Printf("skipped  %s/%s: %s\n", p.agent, p.sc.ID, skip)
			continue
		case err != nil:
			fmt.Printf("failed   %s/%s: %v\n", p.agent, p.sc.ID, err)
			failed++
			continue
		}
		dir, base := filepath.Join(out, p.agent), p.sc.ID
		if pilot {
			dir, base = out, "engine-restart"
		}
		if err := result.Write(dir, base, src); err != nil {
			fmt.Printf("failed   %s/%s: %v\n", p.agent, p.sc.ID, err)
			failed++
			continue
		}
		fmt.Printf("written  %s/%s: %s\n", p.agent, p.sc.ID, filepath.Join(dir, base+".jsonl"))
	}
	if failed > 0 {
		return 1
	}
	return 0
}

type pair struct {
	sc    tape.Scenario
	agent string
}

// pick expands the flags into the pairs to run, scenarios in catalogue
// order and agents sorted.
func pick(scenario, agent string) ([]pair, error) {
	var scs []tape.Scenario
	if scenario == "all" {
		scs = tape.Catalogue()
	} else {
		sc, ok := tape.Lookup(scenario)
		if !ok {
			return nil, fmt.Errorf("no scenario %q (have %s)", scenario, strings.Join(ids(), ", "))
		}
		scs = []tape.Scenario{sc}
	}
	agents := sessiongen.Names()
	if agent != "all" {
		if !slices.Contains(agents, agent) {
			return nil, fmt.Errorf("no agent profile %q (have %s)", agent, strings.Join(agents, ", "))
		}
		agents = []string{agent}
	}
	var out []pair
	for _, a := range agents {
		for _, sc := range scs {
			out = append(out, pair{sc, a})
		}
	}
	return out, nil
}

func ids() []string {
	var out []string
	for _, s := range tape.Catalogue() {
		out = append(out, s.ID)
	}
	return out
}
