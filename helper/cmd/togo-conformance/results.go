package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"
)

// interrupted marks a scenario the run never reached because the harness
// ran out of time or the run was stopped. It is neither a pass nor a fail.
const interrupted = "INTERRUPTED"

// resultsSchema is the version of the -json file.
const resultsSchema = 1

// Report is the -json file: one run of the suite.
type Report struct {
	Schema    int             `json:"schema"`
	Started   time.Time       `json:"started"`
	Finished  time.Time       `json:"finished"`
	Helper    string          `json:"helper"` // the commit the suite was built from
	OS        string          `json:"os"`
	Model     string          `json:"model,omitempty"` // the OpenRouter model, when -openrouter was given
	Harnesses []HarnessResult `json:"harnesses"`
	Cleanup   []string        `json:"cleanup"`
}

// HarnessResult is one agent's part of a run.
type HarnessResult struct {
	Harness string            `json:"harness"`
	Version string            `json:"version"`
	Account string            `json:"account"` // "openrouter" or "own"
	Seconds int               `json:"seconds"`
	Results map[string]result `json:"results"`
	Notes   []string          `json:"notes"`
}

// buildReport gathers the runners' results. Text that came from the machine
// (reasons, notes, cleanup lines) has the run folder, the home folder and
// the host name replaced, so the file can be shared.
func buildReport(runners []*runner, cleanup []string, started, finished time.Time, o *options) Report {
	tidy := tidier(o.root)
	rep := Report{Schema: resultsSchema, Started: started.UTC(), Finished: finished.UTC(),
		Helper: builtFrom(), OS: runtime.GOOS + "/" + runtime.GOARCH}
	if o.router != nil {
		rep.Model = o.router.model
	}
	for _, r := range runners {
		h := HarnessResult{Harness: r.id, Version: r.version, Account: "own",
			Seconds: int(r.took.Round(time.Second) / time.Second), Results: map[string]result{}}
		if o.router != nil && !r.ownAccount {
			h.Account = "openrouter"
		}
		for _, s := range scenarios {
			if len(o.scenarios) > 0 && !o.scenarios[s] {
				continue
			}
			res, ok := r.res[s]
			switch {
			case ok && res.Status != "":
				h.Results[s] = result{Status: res.Status, Reason: tidy(res.Reason)}
			case r.cutShort:
				h.Results[s] = result{Status: interrupted, Reason: "the run stopped before this scenario"}
			}
		}
		for _, n := range r.notes {
			h.Notes = append(h.Notes, tidy(n))
		}
		rep.Harnesses = append(rep.Harnesses, h)
	}
	for _, c := range cleanup {
		rep.Cleanup = append(rep.Cleanup, tidy(c))
	}
	return rep
}

// tidier returns a function that removes the run folder, the user's home
// and the host name from a line of text.
func tidier(root string) func(string) string {
	var pairs []string
	add := func(from, to string) {
		if len(from) > 1 {
			pairs = append(pairs, from, to)
		}
	}
	add("/private"+root, "<run>")
	add(root, "<run>")
	if home, err := os.UserHomeDir(); err == nil {
		add("/private"+home, "~")
		add(home, "~")
	}
	if host, err := os.Hostname(); err == nil {
		add(host, "<host>")
		add(strings.TrimSuffix(host, ".local"), "<host>")
	}
	rep := strings.NewReplacer(pairs...)
	return rep.Replace
}

// builtFrom is the commit this binary was built from, or "unknown".
func builtFrom() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "", ""
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "+changes"
			}
		}
	}
	if rev == "" {
		return "unknown"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	return rev + dirty
}

func (r Report) write(path string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func readReport(path string) (Report, error) {
	var r Report
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

// Change is one scenario whose outcome differs between two runs.
type Change struct {
	Harness, Scenario      string
	From, To               string
	VersionFrom, VersionTo string
	Reason                 string // the newer run's reason
}

// Upstream reports whether the agent itself changed between the runs, so a
// new failure points at the agent's release before it points at Togo.
func (c Change) Upstream() bool { return c.VersionFrom != c.VersionTo && c.VersionFrom != "" }

// Regressed reports whether a passing scenario now fails.
func (c Change) Regressed() bool { return c.From == pass && c.To == fail }

// diffReports lists what changed from old to new. A scenario that was
// interrupted in either run says nothing and is left out; so is an agent
// missing from the old run. Upstream changes come first.
func diffReports(old, cur Report) []Change {
	before := map[string]HarnessResult{}
	for _, h := range old.Harnesses {
		before[h.Harness] = h
	}
	var out []Change
	for _, h := range cur.Harnesses {
		was, ok := before[h.Harness]
		if !ok {
			continue
		}
		for _, s := range scenarios {
			a, b := was.Results[s].Status, h.Results[s].Status
			if a == "" || b == "" || a == b || a == interrupted || b == interrupted {
				continue
			}
			out = append(out, Change{Harness: h.Harness, Scenario: s, From: a, To: b,
				VersionFrom: was.Version, VersionTo: h.Version, Reason: h.Results[s].Reason})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Upstream() != out[j].Upstream() {
			return out[i].Upstream()
		}
		return out[i].Regressed() && !out[j].Regressed()
	})
	return out
}

// printDiff writes one line per change and returns 1 when a passing
// scenario now fails.
func printDiff(w io.Writer, changes []Change) int {
	code := 0
	for _, c := range changes {
		kind := "changed"
		switch {
		case c.Regressed() && c.Upstream():
			kind = "agent updated"
		case c.Regressed():
			kind = "regressed"
		case c.To == pass:
			kind = "fixed"
		}
		if c.Regressed() {
			code = 1
		}
		version := c.VersionTo
		if c.Upstream() {
			version = c.VersionFrom + " to " + c.VersionTo
		}
		fmt.Fprintf(w, "%-13s %-9s %-12s %s to %s  (%s)  %s\n", kind, c.Harness, c.Scenario, c.From, c.To, version, c.Reason)
	}
	if len(changes) == 0 {
		fmt.Fprintln(w, "no change")
	}
	return code
}
