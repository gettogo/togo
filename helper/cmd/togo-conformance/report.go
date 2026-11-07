package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

type harnessItem = harness.Item

var heads = map[string]string{
	"create": "create", "trust": "trust", "signin": "signin", "first_turn": "turn", "working": "working",
	"model_switch": "model", "mode": "mode", "effort": "effort", "approval": "approval",
	"interrupt": "intr", "slash": "slash", "restore": "restore", "close": "close",
}

// report prints the table and one line per result, and returns the exit
// code: 1 when any scenario failed.
func report(w io.Writer, runners []*runner, cleanup []string, took time.Duration, o *options) int {
	cols := []string{}
	for _, s := range scenarios {
		if len(o.scenarios) == 0 || o.scenarios[s] {
			cols = append(cols, s)
		}
	}
	fmt.Fprintf(w, "\nTogo harness conformance — run %s, %s\n\n", o.runID, took.Round(time.Second))
	fmt.Fprintf(w, "%-10s %-10s", "harness", "version")
	for _, c := range cols {
		fmt.Fprintf(w, " %-8s", heads[c])
	}
	fmt.Fprintln(w)
	failed := 0
	counts := map[string]int{}
	for _, r := range runners {
		fmt.Fprintf(w, "%-10s %-10s", r.id, clip(r.version, 10))
		for _, c := range cols {
			st := r.res[c].Status
			if st == "" {
				st = "-"
			}
			counts[st]++
			if st == fail {
				failed++
			}
			fmt.Fprintf(w, " %-8s", st)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "\n%d PASS, %d FAIL, %d SKIP\n", counts[pass], counts[fail], counts[skip])
	for _, r := range runners {
		fmt.Fprintf(w, "\n%s\n", r.id)
		for _, c := range cols {
			res := r.res[c]
			if res.Status == "" {
				continue
			}
			fmt.Fprintf(w, "  %-12s %s  %s\n", c, res.Status, res.Reason)
		}
		for _, n := range r.notes {
			fmt.Fprintf(w, "  · %s\n", n)
		}
	}
	if len(cleanup) > 0 {
		fmt.Fprintf(w, "\ncleanup\n  · %s\n", strings.Join(cleanup, "\n  · "))
	}
	if failed > 0 {
		return 1
	}
	return 0
}
