// Command coverage-report prints the test matrix: for each agent, how many
// cells are covered, not applicable or missing, and which are missing. With
// -update it rewrites missing.txt to the current missing set; it refuses
// to add lines unless -allow-add is given, so the list cannot grow by
// accident.
//
//	go run ./internal/harness/coverage/cmd/coverage-report
//	go run ./internal/harness/coverage/cmd/coverage-report -update
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("coverage-report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	update := fs.Bool("update", false, "rewrite missing.txt to the current missing set (it may only shrink)")
	allowAdd := fs.Bool("allow-add", false, "with -update, allow new lines in missing.txt")
	quiet := fs.Bool("counts", false, "print the counts only, not the missing keys")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	m, err := coverage.Load()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *update {
		return rewrite(m, *allowAdd, stdout, stderr)
	}
	report(stdout, m, !*quiet)
	return 0
}

func rewrite(m *coverage.Matrix, allowAdd bool, stdout, stderr io.Writer) int {
	path := coverage.MissingFile()
	old, err := coverage.ReadMissing(path)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(stderr, err)
		return 1
	}
	listed := map[string]bool{}
	for _, k := range old {
		listed[k] = true
	}
	now := m.Missing()
	var added []string
	for _, k := range now {
		if !listed[k] {
			added = append(added, k)
		}
	}
	if len(added) > 0 && !allowAdd {
		fmt.Fprintf(stderr, "refusing to add %d line(s) to missing.txt; write tests for them, or pass -allow-add if the owner agreed:\n  %s\n",
			len(added), strings.Join(added, "\n  "))
		return 1
	}
	if err := coverage.WriteMissing(path, now); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "missing.txt: %d line(s), %d added, %d removed\n", len(now), len(added), len(old)+len(added)-len(now))
	return 0
}

func report(w io.Writer, m *coverage.Matrix, keys bool) {
	type row struct{ total, na, covered, missing, weak int }
	rows := map[string]*row{}
	miss := map[string][]coverage.Cell{}
	var sum row
	for _, c := range m.Cells {
		r := rows[c.Agent]
		if r == nil {
			r = &row{}
			rows[c.Agent] = r
		}
		for _, t := range []*row{r, &sum} {
			t.total++
			switch m.Status(c) {
			case coverage.StatusNotApplicable:
				t.na++
			case coverage.StatusCovered:
				t.covered++
			default:
				t.missing++
				if m.Weak(c) {
					t.weak++
				}
			}
		}
		if m.Status(c) == coverage.StatusMissing {
			miss[c.Agent] = append(miss[c.Agent], c)
		}
	}
	ids := coverage.Agents(m.Cells)
	fmt.Fprintf(w, "%-10s %6s %6s %8s %8s %12s\n", "agent", "total", "n/a", "covered", "missing", "unit/live")
	for _, id := range ids {
		r := rows[id]
		fmt.Fprintf(w, "%-10s %6d %6d %8d %8d %12d\n", id, r.total, r.na, r.covered, r.missing, r.weak)
	}
	fmt.Fprintf(w, "%-10s %6d %6d %8d %8d %12d\n", "all", sum.total, sum.na, sum.covered, sum.missing, sum.weak)
	fmt.Fprintln(w, "\ncovered = tape or generated evidence; unit/live = missing cells that have only unit or live evidence")
	if !keys {
		return
	}
	for _, id := range ids {
		if len(miss[id]) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s missing:\n", id)
		byKind := map[string][]string{}
		for _, c := range miss[id] {
			byKind[c.Kind] = append(byKind[c.Kind], c.Name)
		}
		kinds := make([]string, 0, len(byKind))
		for k := range byKind {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			fmt.Fprintf(w, "  %-10s %s\n", k, strings.Join(byKind[k], ", "))
		}
	}
}
