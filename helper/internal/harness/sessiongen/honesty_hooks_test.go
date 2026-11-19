package sessiongen

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The honesty tests of the hook profiles hold each one to what its agent
// really sends. Every payload a profile writes must name an event, and
// carry only keys, that a recorded call of that event has: the captures
// in adapters/<agent>/testdata and hookstyle/testdata, or the payloads the
// adapter's tests quote where there is no capture (listed here with the
// test that quotes them). Every key the mapper reads for an event must be
// written for it.
//
// The log records of Copilot and the transcript records of Droid are held
// to the same two rules by recordKeys.

type hookSpec struct {
	profile  func() Profile
	fixtures []string
	// quoted are the keys a test quotes for an event no capture has.
	quoted map[string][]string
	// base are the keys every call of the agent's captures carries; an
	// event known only from a quote may carry them too.
	base []string
	// reads are the payload keys the mapper reads, per event, in the
	// agent's own names (mapper file:line in the profile's comment).
	reads map[string][]string
}

var hookSpecs = map[string]hookSpec{}

// realHooks reads the calls of the fixture files: the keys per event.
func realHooks(t *testing.T, files []string) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	for _, f := range files {
		fh, err := os.Open(filepath.Join("..", "adapters", f))
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			var c struct {
				Event   string         `json:"event"`
				Payload map[string]any `json:"payload"`
			}
			if json.Unmarshal(sc.Bytes(), &c) != nil || c.Event == "" {
				continue
			}
			if out[c.Event] == nil {
				out[c.Event] = map[string]bool{}
			}
			for k := range c.Payload {
				out[c.Event][k] = true
			}
		}
		fh.Close()
	}
	return out
}

// everything is a script that makes every call the profiles can write.
func everything(a Script) Script {
	a.Turns = []Turn{
		{Say: 40, Tools: 3, Output: 30, Hook: &HookTurn{Approve: 1, Fail: 1, Ask: &HookAsk{Prompt: "Which one?", Options: []string{"A", "B"}, Answer: "A"}}, Agents: &Batch{Size: 2, Steps: 0}},
		{Say: 10, Tools: 1, Interrupt: true, Hook: &HookTurn{Steer: true}},
		{Say: 10, Tools: 1, Error: true},
		{Say: 10, New: true, Hook: &HookTurn{Ask: &HookAsk{Prompt: "Go?", Options: []string{"Yes", "No"}, Cancel: true, Plan: "Do it"}}},
		{Say: 10, Hook: &HookTurn{Resume: true, Model: "gpt-5"}, Exit: true},
	}
	return a
}

func TestHookProfilesWriteOnlyWhatTheAgentSends(t *testing.T) {
	for name, spec := range hookSpecs {
		t.Run(name, func(t *testing.T) {
			real := realHooks(t, spec.fixtures)
			home := t.TempDir()
			s := Script{Seed: 5, Start: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Cwd: filepath.Join(home, "proj")}
			turns := everything(s).Turns
			var l *Layout
			// A profile errors on a feature its agent has no hook for:
			// drop the turn's field and write the rest.
			for try := 0; ; try++ {
				if try > 2 {
					t.Fatal("the script still fails after its features were dropped")
				}
				var err error
				l, err = Write(home, spec.profile(), Script{Seed: s.Seed, Start: s.Start, Cwd: s.Cwd, Turns: turns})
				if err == nil {
					break
				}
				turns = dropFeature(t, turns, err, try)
				_ = os.RemoveAll(home)
				_ = os.MkdirAll(s.Cwd, 0o755)
			}
			wrote := map[string]map[string]bool{}
			for _, c := range l.Hooks {
				if wrote[c.Event] == nil {
					wrote[c.Event] = map[string]bool{}
				}
				allowed := map[string]bool{}
				for k := range real[c.Event] {
					allowed[k] = true
				}
				q, quoted := spec.quoted[c.Event]
				if real[c.Event] == nil && !quoted {
					t.Fatalf("the profile writes %q, which no capture or quoted payload has", c.Event)
				}
				for _, k := range q {
					allowed[k] = true
				}
				if real[c.Event] == nil {
					for _, k := range spec.base {
						allowed[k] = true
					}
				}
				for k := range c.Payload {
					wrote[c.Event][k] = true
					if !allowed[k] {
						t.Errorf("%s: the profile writes key %q, which no real %s has (real: %v)", c.Event, k, c.Event, keys(allowed))
					}
				}
				for _, k := range spec.reads[c.Event] {
					if _, ok := c.Payload[k]; !ok {
						t.Errorf("%s: the mapper reads %q, which the profile does not write", c.Event, k)
					}
				}
			}
			for ev := range spec.reads {
				if wrote[ev] == nil {
					t.Errorf("the profile never writes %q", ev)
				}
			}
			recordKeys(t, name, l)
		})
	}
}

// dropFeature removes from the script every feature a hook-less agent
// cannot write.
func dropFeature(t *testing.T, turns []Turn, err error, level int) []Turn {
	t.Helper()
	if !strings.Contains(err.Error(), "sessiongen:") {
		t.Fatal(err)
	}
	out := make([]Turn, len(turns))
	copy(out, turns)
	for i := range out {
		h := HookTurn{}
		if out[i].Hook != nil {
			h = *out[i].Hook
		}
		h.Ask, h.Approve = nil, 0
		if level > 0 {
			out[i].Agents = nil
		}
		out[i].Hook = &h
	}
	return out
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// recordKeys checks the file lines a profile attaches to hook calls
// against the real log (Copilot's events.jsonl) and transcript (Droid's
// session file).
func recordKeys(t *testing.T, name string, l *Layout) {
	t.Helper()
	var real map[string]map[string]bool
	var reads map[string][]string
	switch name {
	case "copilot":
		real = realRecords(t, "type", "copilot/testdata/ask.events.jsonl", "copilot/testdata/plan.events.jsonl")
		// The mapper reads type and data.content, data.toolRequests,
		// data.parentToolCallId (copilot/replies.go:75-88).
		reads = map[string][]string{"assistant.message": {"content", "toolRequests"}}
	case "droid":
		real = realRecords(t, "type", "hookstyle/testdata/droid-transcript-unreachable.jsonl")
		// droidOutcome reads type, id, message, turnId and reason
		// (hookstyle/failures.go:103-162).
		reads = map[string][]string{"agent_turn_outcome": {"turnId", "reason"}}
	default:
		return
	}
	got := map[string]bool{}
	for _, c := range l.Hooks {
		for _, w := range c.Writes {
			rec := w.Record.(map[string]any)
			typ, _ := rec["type"].(string)
			got[typ] = true
			allowed, ok := real[typ]
			if !ok {
				t.Errorf("the profile writes a %q record, which no real log has", typ)
				continue
			}
			top := map[string]bool{}
			for k := range rec {
				top[k] = true
			}
			for k := range top {
				if !allowed[k] {
					t.Errorf("%s: key %q is not in a real record", typ, k)
				}
			}
			if d, ok := rec["data"].(map[string]any); ok {
				for k := range d {
					if !allowed["data."+k] {
						t.Errorf("%s: key data.%s is not in a real record", typ, k)
					}
				}
			}
			for _, k := range reads[typ] {
				if _, ok := rec[k]; !ok {
					if d, ok := rec["data"].(map[string]any); !ok || d[k] == nil {
						t.Errorf("%s: the mapper reads %q, which the profile does not write", typ, k)
					}
				}
			}
		}
	}
	for typ := range reads {
		if !got[typ] {
			t.Errorf("the profile never writes a %q record", typ)
		}
	}
}

// realRecords collects, per record type, its top-level keys and, under
// "data.", the keys of its data object.
func realRecords(t *testing.T, typeKey string, files ...string) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	for _, f := range files {
		fh, err := os.Open(filepath.Join("..", "adapters", f))
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			var r map[string]any
			if json.Unmarshal(sc.Bytes(), &r) != nil {
				continue
			}
			typ, _ := r[typeKey].(string)
			if typ == "" {
				continue
			}
			if out[typ] == nil {
				out[typ] = map[string]bool{}
			}
			for k, v := range r {
				out[typ][k] = true
				if d, ok := v.(map[string]any); ok && k == "data" {
					for dk := range d {
						out[typ]["data."+dk] = true
					}
				}
			}
			// A message's own object holds role and content in Droid's.
			if m, ok := r["message"].(map[string]any); ok {
				for k := range m {
					out[typ]["message."+k] = true
				}
			}
		}
		fh.Close()
	}
	return out
}
