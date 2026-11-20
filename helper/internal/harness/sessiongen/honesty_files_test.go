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

// The honesty tests of the profiles that write files or databases hold
// each one to the shapes its agent's real captures have: every key path
// of every record, row or file a profile writes must be one the captures
// hold, and every path the mapper reads must be written.

// pathsOf collects the key paths of a JSON value. An entry object (a value
// under the key "entry", or an element of a snapshot chunk) is told apart by
// its type, so the entry kinds do not share paths; arrays are flattened.
func pathsOf(v any, prefix string, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			p := prefix + "." + k
			if prefix == "" {
				p = k
			}
			out[p] = true
			pathsOf(c, p, out)
		}
	case []any:
		for _, c := range x {
			pathsOf(c, prefix+"[]", out)
		}
	}
}

// entryPaths is pathsOf for a history entry, keyed by the entry's type.
func entryPaths(e map[string]any, out map[string]bool) {
	typ, _ := e["type"].(string)
	pathsOf(e, "entry:"+typ, out)
}

// vibePaths reads the shapes of one Vibe journal record (or snapshot
// chunk entry list) into the set.
func vibeRecord(rec map[string]any, out map[string]bool) {
	typ, _ := rec["type"].(string)
	p, _ := rec["payload"].(map[string]any)
	for k := range rec {
		out["record:"+typ+"."+k] = true
	}
	switch typ {
	case "projection_delta":
		ds, _ := p["delta"].([]any)
		for k := range p {
			out["delta."+k] = true
		}
		for _, d := range ds {
			op, _ := d.(map[string]any)
			opName, _ := op["op"].(string)
			for k := range op {
				out["op:"+opName+"."+k] = true
			}
			if e, ok := op["entry"].(map[string]any); ok {
				entryPaths(e, out)
			}
			if st, ok := op["state"].(map[string]any); ok {
				pathsOf(st, "envelope", out)
			}
		}
	case "callback_registered", "callback_resolved":
		pathsOf(p, "callback:"+typ, out)
	}
}

func realVibe(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join("..", "adapters", "vibe", "testdata", "journal-*.jsonl"))
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			var rec map[string]any
			if json.Unmarshal(sc.Bytes(), &rec) == nil {
				vibeRecord(rec, out)
			}
		}
		fh.Close()
	}
	root := filepath.Join("..", "adapters", "vibe", "testdata", "home", ".vibe", "logs", "session", "unified")
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		raw, _ := os.ReadFile(p)
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return nil
		}
		switch {
		case strings.HasSuffix(p, "meta.json"):
			pathsOf(v, "meta", out)
		case strings.HasSuffix(p, "CURRENT"):
			pathsOf(v, "current", out)
		case strings.HasSuffix(p, "manifest.json"):
			pathsOf(v, "manifest", out)
		case strings.HasSuffix(p, "projection-state.json"):
			pathsOf(v, "state", out)
		case strings.Contains(p, "/chunks/"):
			for _, e := range v.([]any) {
				entryPaths(e.(map[string]any), out)
			}
		}
		return nil
	})
	return out
}

// vibeReads are the paths the mapper reads (adapters/vibe: session.go
// :60-141, ask.go:38-63 and :188-206 and :313-320, history.go:48-86), each
// as the profile's shape names it.
var vibeReads = []string{
	"record:projection_delta.type", "record:projection_delta.payload", "delta.delta",
	"op:append_entry.entry", "op:replace_entry.entry", "op:set_envelope.state",
	"entry:message.role", "entry:message.content[].text", "entry:message.turnId", "entry:message.createdAt", "entry:message.id",
	"entry:message.sessionId", "entry:message.outcome.type",
	"entry:reasoning.text", "entry:effect.detail.toolName", "entry:effect.detail.input", "entry:effect.state.status",
	"entry:effect.state.outputText", "entry:effect.state.output.structured_content", "entry:effect.state.error.code",
	"entry:effect.updatedAt", "entry:effect.title",
	"envelope.latestTurn.id", "envelope.latestTurn.status", "envelope.latestTurn.startedAt", "envelope.latestTurn.completedAt",
	"envelope.latestTurn.error.code", "envelope.latestTurn.error.message", "envelope.latestTurn.error.details.httpStatus",
	"envelope.latestTurn.error.details.reason", "envelope.latestTurn.error.details.exceptionType", "envelope.session.id",
	"callback:callback_registered.routing.detail.kind", "callback:callback_registered.routing.detail.choices",
	"callback:callback_registered.routing.relatedEntryId", "callback:callback_registered.routing.detail.effect.toolName",
	"callback:callback_registered.routing.detail.effect.input", "callback:callback_registered.routing.detail.requiredPermissions[].label",
	"callback:callback_resolved.result.output.decision.type", "callback:callback_resolved.result.output.type",
	"meta.environment.working_directory", "meta.origin_directory", "meta.parent_session_id", "meta.config.active_model",
	"current.generation", "manifest.projection_state.chunks", "state.snapshot.latestTurn.id",
}

func vibeScript(home string) (Script, []Turn) {
	cwd := filepath.Join(home, "proj")
	_ = os.MkdirAll(cwd, 0o755)
	s := Script{Seed: 9, Start: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Cwd: cwd}
	return s, []Turn{
		{Say: 30, Tools: 3, Output: 40, Hook: &HookTurn{Approve: 2, Fail: 1}},
		{Say: 30, Tools: 1, Hook: &HookTurn{Ask: &HookAsk{Prompt: "Which one?", Options: []string{"A", "B"}, Answer: "A"}}},
		{Say: 30, Hook: &HookTurn{Ask: &HookAsk{Prompt: "Skip?", Options: []string{"A"}, Cancel: true}}},
		{Say: 30, Tools: 1, Interrupt: true},
		{Say: 30, Tools: 1, Error: true},
		{Say: 30, New: true},
		{Say: 30, Tools: 1, Hook: &HookTurn{Pending: true, Approve: 1}},
		{Prompt: Continue, Say: 20},
	}
}

func TestVibeWritesOnlyWhatVibeWrites(t *testing.T) {
	home := t.TempDir()
	s, turns := vibeScript(home)
	if _, err := Write(home, &Vibe{}, Script{Seed: s.Seed, Start: s.Start, Cwd: s.Cwd, Turns: turns}); err != nil {
		t.Fatal(err)
	}
	real := realVibe(t)
	got := map[string]bool{}
	root := filepath.Join(home, ".vibe", "logs", "session", "unified")
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		raw, _ := os.ReadFile(p)
		switch {
		case strings.Contains(p, "/journal/"):
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				var rec map[string]any
				if line != "" && json.Unmarshal([]byte(line), &rec) == nil {
					vibeRecord(rec, got)
				}
			}
			return nil
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Errorf("%s is not JSON: %v", p, err)
			return nil
		}
		switch {
		case strings.HasSuffix(p, "meta.json"):
			pathsOf(v, "meta", got)
		case strings.HasSuffix(p, "CURRENT"):
			pathsOf(v, "current", got)
		case strings.HasSuffix(p, "manifest.json"):
			pathsOf(v, "manifest", got)
		case strings.HasSuffix(p, "projection-state.json"):
			pathsOf(v, "state", got)
		case strings.Contains(p, "/chunks/"):
			for _, e := range v.([]any) {
				entryPaths(e.(map[string]any), got)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var extra []string
	for p := range got {
		// The snapshot's state is the public session state of the journal's
		// envelopes: its latest turn can fail or end interrupted there too.
		if rest, ok := strings.CutPrefix(p, "state.snapshot."); ok && real["envelope."+rest] {
			continue
		}
		if !real[p] {
			extra = append(extra, p)
		}
	}
	sort.Strings(extra)
	for _, p := range extra {
		t.Errorf("the profile writes %q, which no real Vibe capture has", p)
	}
	for _, p := range vibeReads {
		if !got[p] {
			t.Errorf("the mapper reads %q, which the profile does not write", p)
		}
		if !real[p] {
			t.Errorf("%q is not in the captures (fix the list of reads)", p)
		}
	}
}

// A script with a feature no capture holds is refused, not invented.
func TestVibeRefusesWhatNoCaptureHolds(t *testing.T) {
	for name, turn := range map[string]Turn{
		"compaction": {Compact: true}, "background job": {Jobs: 1}, "subagent": {Agents: &Batch{Size: 1}},
		"model change": {Hook: &HookTurn{Model: "x"}}, "queued prompt": {Hook: &HookTurn{Steer: true}},
	} {
		home := t.TempDir()
		s, _ := vibeScript(home)
		if _, err := Write(home, &Vibe{}, Script{Seed: 1, Start: s.Start, Cwd: s.Cwd, Turns: []Turn{turn}}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
