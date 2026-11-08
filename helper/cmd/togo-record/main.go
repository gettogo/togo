// Command togo-record records one scripted scenario on a real agent session
// as a replay fixture: every frame the app would see for that session, with
// secrets, paths and names scrubbed. It drives the installed helper the way
// the app does (`togo serve --stdio`), always creates the session muted, and
// kills the session when it is done.
//
//	togo-record -agent claude -scenario approve -args=--model,haiku -out protocol/v1/replays/claude.approve.jsonl
//
// It is a developer tool; nothing ships it.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/replay"
)

var scenarios = map[string]string{
	"turn":      "Reply with exactly one word: ok",
	"error":     "Reply with exactly one word: ok",
	"signin":    "Reply with exactly one word: ok",
	"approve":   "Use your shell tool to run exactly this command and nothing else: touch replay-probe.txt . Then reply with one word: done",
	"deny":      "Use your shell tool to run exactly this command and nothing else: touch replay-probe.txt . Then reply with one word: done",
	"question":  "Use the AskUserQuestion tool once, with two questions in the same call: 'Color?' with options Red and Blue, and 'Size?' with options Small and Large. After I answer, reply with my two answers on one line and nothing else.",
	"interrupt": "Write the numbers from 1 to 400, one per line, with no other text.",
	"model":     "Reply with exactly one word: ok",
	// A real piece of work in a real folder (-cwd, -prompt): every request the agent makes is
	// approved, as its owner would, short of a sign-in.
	"task":     "-prompt is required",
	"subagent": "Use your Task (subagent) tool once, with run_in_background set to true, to start a subagent whose only job is to reply with the word pong. Wait for its result, then reply with what it said and nothing else.",
}

func main() {
	agent := flag.String("agent", "", "agent id for session.create")
	scenario := flag.String("scenario", "turn", "turn, approve, deny, question, interrupt, model, subagent, task, error or signin")
	args := flag.String("args", "", "comma-separated agent arguments (a cheap model)")
	prompt := flag.String("prompt", "", "the prompt, instead of the scenario's own")
	then := flag.String("then", "", "a second prompt, sent once the first turn has settled")
	cwdAs := flag.String("cwd-as", "/tmp/replay", "how the session folder reads in the fixture")
	model := flag.String("model", "", "for -scenario model: the model id to switch to")
	out := flag.String("out", "", "fixture path (.jsonl)")
	togo := flag.String("togo", filepath.Join(os.Getenv("HOME"), ".togo/bin/togo"), "helper binary")
	cwd := flag.String("cwd", "", "session folder (default: a scratch folder)")
	timeout := flag.Duration("timeout", 150*time.Second, "longest the scenario may take")
	flag.Parse()
	if *agent == "" || *out == "" || scenarios[*scenario] == "" {
		flag.Usage()
		os.Exit(2)
	}
	text := *prompt
	if *scenario == "task" && text == "" {
		fmt.Fprintln(os.Stderr, "togo-record: -scenario task needs -prompt")
		os.Exit(2)
	}
	if text == "" {
		text = scenarios[*scenario]
	}
	r := &recording{agent: *agent, scenario: *scenario, prompt: text, then: *then, cwdAs: *cwdAs, model: *model,
		deadline: time.Now().Add(*timeout)}
	if *args != "" {
		r.args = strings.Split(*args, ",")
	}
	r.cwd = *cwd
	if r.cwd == "" {
		dir, err := os.MkdirTemp("", "togo-replay-")
		if err != nil {
			fatal(err)
		}
		defer os.RemoveAll(dir)
		r.cwd = dir
	}
	guard := snapshotConfigs(*agent)
	defer guard.restore()
	r.tape = newTape()
	c, err := start(*togo, r.tape.onFrame)
	if err != nil {
		fatal(err)
	}
	r.c = c
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		// Never leave a test session running.
		r.cleanup()
		guard.restore()
		os.Exit(130)
	}()
	runErr := r.run()
	r.cleanup()
	c.close()
	r.changed = guard.restore()
	if err := r.write(*out, *togo, runErr); err != nil {
		fatal(err)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "togo-record: scenario incomplete:", runErr)
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "togo-record:", err)
	os.Exit(1)
}

// write scrubs the frames, checks nothing identifying survived and writes
// the fixture: a header line, then one line per frame.
func (r *recording) write(path, togo string, runErr error) error {
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	short := strings.SplitN(host, ".", 2)[0]
	user := filepath.Base(home)
	realCwd, _ := filepath.EvalSymlinks(r.cwd)
	as := r.cwdAs
	s := replay.Scrubber{
		Paths: map[string]string{home: "~", r.cwd: as, realCwd: as, os.TempDir(): "/tmp/",
			// Claude and others name per-folder state after the folder with dashes for slashes.
			dashed(r.cwd): dashed(as), dashed(realCwd): dashed(as), dashed(home): "-Users-user",
			tilde(r.cwd, home): as},
		Words: map[string]string{user: "user", host: "host", short: "host"},
	}
	forbidden := []string{home, user, host, short}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if len(v) >= 12 && looksSecret(k) {
			forbidden = append(forbidden, v)
		}
	}
	note := ""
	if runErr != nil {
		note = runErr.Error()
	}
	version, _ := exec.Command(togo, "version").Output()
	header := map[string]any{
		"replay": 1, "agent": r.agent, "scenario": r.scenario, "args": r.args, "prompt": r.prompt, "then": r.then,
		"session": r.session, "recorded": time.Now().UTC().Format("2006-01-02"),
		"helper": strings.TrimSpace(string(version)), "complete": runErr == nil, "note": note,
		"configChanged": r.changed,
	}
	var b strings.Builder
	hb, _ := json.Marshal(s.Value(header))
	b.Write(hb)
	b.WriteByte('\n')
	for _, l := range r.tape.snapshotLines() {
		l.Frame = s.Frame(l.Frame)
		if l.Frame == nil {
			continue
		}
		lb, err := json.Marshal(l)
		if err != nil {
			return err
		}
		b.Write(lb)
		b.WriteByte('\n')
	}
	if hits := replay.Leaks(b.String(), forbidden); len(hits) > 0 {
		return fmt.Errorf("fixture not written: %d identifying strings survived scrubbing", len(hits))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	_, _ = w.WriteString(b.String())
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "togo-record: wrote %s (%d frames)\n", path, len(r.tape.snapshotLines()))
	return f.Close()
}

// tilde is path with the home folder written as ~, as agents print it.
func tilde(path, home string) string {
	if rest, ok := strings.CutPrefix(path, home); ok && home != "" {
		return "~" + rest
	}
	return ""
}

func dashed(path string) string {
	if path == "" {
		return ""
	}
	return strings.NewReplacer("/", "-", ".", "-").Replace(path)
}

func looksSecret(k string) bool {
	k = strings.ToUpper(k)
	for _, w := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}
