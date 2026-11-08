// Command togo-conformance runs standard scenarios against every installed
// coding agent on real, muted sessions, through a helper built from this
// tree with its own TOGO_HOME and tmux server per agent, and prints a
// harness × scenario table. scripts/check-conformance.sh builds and runs
// it. It never signs in, never answers a sign-in or update prompt, and
// puts back any agent config file a run changed.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type options struct {
	bin       string
	adapters  string
	only      map[string]bool
	scenarios map[string]bool
	jobs      int
	perAgent  time.Duration
	jsonOut   string
	root      string
	runID     string
}

// order runs the slow, signed-in agents first so the run ends sooner.
var order = []string{"claude", "omp", "opencode", "codex", "kilo", "pi", "gemini", "kimi", "copilot", "crush", "qwen", "droid", "auggie", "vibe", "goose", "aider", "cline", "cursor", "amp", "openhands", "kiro"}

func main() { os.Exit(realMain()) }

func realMain() int {
	var o options
	var only, scen string
	var cleanup bool
	flag.StringVar(&o.bin, "bin", "", "togo binary built from this tree (required)")
	flag.StringVar(&o.adapters, "adapters", "", "adapter folder (helper/internal/harness/adapters) for input-line patterns")
	flag.StringVar(&only, "only", "", "comma-separated harness ids to run")
	flag.StringVar(&scen, "scenario", "", "comma-separated scenarios to report: "+strings.Join(scenarios, ","))
	flag.IntVar(&o.jobs, "jobs", 3, "harnesses run at once")
	flag.DurationVar(&o.perAgent, "timeout", 9*time.Minute, "deadline per harness")
	flag.StringVar(&o.jsonOut, "json", "", "also write results as JSON here")
	flag.StringVar(&o.root, "root", "", "scratch root (default /private/tmp/togo-conformance-<run id>)")
	flag.StringVar(&o.runID, "run-id", "", "run id (default random)")
	flag.BoolVar(&cleanup, "cleanup", false, "with -root and -run-id: kill the run's servers and daemons, restore configs, exit")
	flag.Parse()
	o.only, o.scenarios = set(only), set(scen)
	for s := range o.scenarios {
		if !contains(scenarios, s) {
			fmt.Fprintf(os.Stderr, "unknown scenario %q (have %s)\n", s, strings.Join(scenarios, ", "))
			return 2
		}
	}
	if o.runID == "" {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		o.runID = "tcr" + hex.EncodeToString(b)
	}
	if !regexp.MustCompile(`^[a-z0-9]{4,24}$`).MatchString(o.runID) {
		fmt.Fprintln(os.Stderr, "run id must be 4-24 lowercase letters and digits")
		return 2
	}
	if o.root == "" {
		o.root = "/private/tmp/togo-conformance-" + o.runID
	}
	snapDir := filepath.Join(o.root, "snap")
	if cleanup {
		owned.load(snapDir)
		finish(o.root, o.runID, loadSnapshot(snapDir))
		return 0
	}
	if o.bin == "" {
		fmt.Fprintln(os.Stderr, "-bin is required")
		return 2
	}
	if err := os.MkdirAll(o.root, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ids, err := discover(ctx, &o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "discover:", err)
		teardown(o.root, o.runID)
		return 2
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "no installed harness matches")
		teardown(o.root, o.runID)
		return 2
	}
	snap, err := snapshot(snapDir, ids)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snapshot:", err)
		teardown(o.root, o.runID)
		return 2
	}
	owned.start(snapDir, snap)
	fmt.Fprintf(os.Stderr, "run %s: %d harnesses, %d at a time: %s\n", o.runID, len(ids), o.jobs, strings.Join(ids, " "))
	start := time.Now()
	runners := runAll(ctx, &o, ids)
	notes := finish(o.root, o.runID, snap)
	code := report(os.Stdout, runners, notes, time.Since(start), &o)
	if o.jsonOut != "" {
		writeJSON(o.jsonOut, runners, notes)
	}
	return code
}

// finish tears down every server and daemon, then restores configs and
// removes the harnesses' per-folder records of the run.
func finish(root, runID string, snap []snapEntry) []string {
	teardown(root, runID)
	// Agents write their own state as they exit (Claude records the
	// folder in ~/.claude.json): let them finish before scrubbing.
	time.Sleep(4 * time.Second)
	notes := restore(snap, runID)
	// A second pass catches agents that wrote on exit after the first.
	time.Sleep(5 * time.Second)
	for _, n := range restore(snap, runID) {
		if !contains(notes, n) {
			notes = append(notes, n)
		}
	}
	notes = append(notes, removeArtifacts(runID)...)
	since := time.Now().Add(-6 * time.Hour)
	if len(snap) > 0 {
		since = snap[0].At.Add(-time.Minute)
	}
	notes = append(notes, removeRunFiles(runID, since)...)
	return notes
}

func discover(ctx context.Context, o *options) ([]string, error) {
	sb, err := newSandbox(o.root, o.runID, "discover")
	if err != nil {
		return nil, err
	}
	defer func() { sb.killServer(); stopDaemon(sb.home) }()
	c, err := dial(ctx, o.bin, sb.env(), nil)
	if err != nil {
		return nil, err
	}
	defer c.close()
	var res struct {
		Agents []struct {
			ID        string `json:"id"`
			Installed bool   `json:"installed"`
		} `json:"agents"`
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := c.call(cctx, "agents.list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	var ids []string
	for _, a := range res.Agents {
		if !a.Installed || a.ID == "shell" || (len(o.only) > 0 && !o.only[a.ID]) {
			continue
		}
		ids = append(ids, a.ID)
	}
	rank := func(id string) int {
		for i, x := range order {
			if x == id {
				return i
			}
		}
		return len(order)
	}
	sort.SliceStable(ids, func(i, j int) bool { return rank(ids[i]) < rank(ids[j]) })
	return ids, nil
}

func runAll(ctx context.Context, o *options, ids []string) []*runner {
	runners := make([]*runner, len(ids))
	sem := make(chan struct{}, o.jobs)
	var wg sync.WaitGroup
	for i, id := range ids {
		r := &runner{id: id, opts: o, items: map[string]harnessItem{}, res: map[string]result{}}
		runners[i] = r
		// Memory guard: below 20% free, at most two harnesses at once.
		for memFree() < 20 && len(sem) >= 2 && ctx.Err() == nil {
			time.Sleep(5 * time.Second)
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			r.res["create"] = result{skip, "run cancelled"}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			runOne(ctx, o, r)
		}()
	}
	wg.Wait()
	return runners
}

func runOne(ctx context.Context, o *options, r *runner) {
	t0 := time.Now()
	fmt.Fprintf(os.Stderr, "  %-9s start\n", r.id)
	defer func() {
		if p := recover(); p != nil {
			r.set("create", fail, "suite panic: %v", p)
		}
		fmt.Fprintf(os.Stderr, "  %-9s done in %s\n", r.id, time.Since(t0).Round(time.Second))
	}()
	sb, err := newSandbox(o.root, o.runID, r.id)
	if err != nil {
		r.set("create", fail, "sandbox: %v", err)
		return
	}
	r.sb, r.start = sb, time.Now().Add(-2*time.Second)
	r.marker = filepath.Base(sb.work)
	defer func() {
		sb.killServer()
		stopDaemon(sb.home)
		// Harnesses write their state as they exit; that write is still
		// the run's own. Then the run stops counting (see owned.go).
		time.Sleep(2 * time.Second)
		owned.observe(r.id, false)
	}()
	hctx, cancel := context.WithTimeout(ctx, o.perAgent)
	defer cancel()
	c, err := dial(hctx, o.bin, sb.env(), r.onNotify)
	if err != nil {
		r.set("create", fail, "helper: %v", err)
		return
	}
	defer c.close()
	// Config changes count as the suite's only between its first call and
	// the end of its run (see owned.go).
	owned.observe(r.id, false)
	c.before = func() { owned.observe(r.id, true) }
	r.c = c
	r.run(hctx)
}

var memRE = regexp.MustCompile(`free percentage: (\d+)%`)

// memFree is the system-wide free memory percentage (macOS), or 100.
func memFree() int {
	out, err := exec.Command("memory_pressure").Output()
	if err != nil {
		return 100
	}
	m := memRE.FindSubmatch(out)
	if m == nil {
		return 100
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n
}

func set(csv string) map[string]bool {
	m := map[string]bool{}
	for _, s := range strings.Split(csv, ",") {
		if s = strings.TrimSpace(s); s != "" {
			m[s] = true
		}
	}
	return m
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func writeJSON(path string, runners []*runner, notes []string) {
	type row struct {
		Harness string            `json:"harness"`
		Version string            `json:"version"`
		Results map[string]result `json:"results"`
		Notes   []string          `json:"notes"`
	}
	var rows []row
	for _, r := range runners {
		rows = append(rows, row{r.id, r.version, r.res, r.notes})
	}
	b, _ := json.MarshalIndent(map[string]any{"harnesses": rows, "cleanup": notes}, "", "  ")
	_ = os.WriteFile(path, b, 0o600)
}
