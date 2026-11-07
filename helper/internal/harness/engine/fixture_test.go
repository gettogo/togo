package engine_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/harness"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/claude"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

// The harness.* golden fixtures run here, on a helper whose engine serves
// the claude adapter over a fake `claude` that prints its version and
// idles. The transcript feed reads adapters/claude/testdata/
// transcript-basic.jsonl; hooks and a status line arrive on the hook bus;
// the anchor marker reports epoch "e1" with fixed offsets.

const fixturesDir = "../../../../protocol/v1/fixtures"

const sid = "s_abcdefghij"

var fixtureNow = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

const fakeClaude = "#!/bin/sh\ncase \"$1\" in\n--version) echo '2.1.319 (Claude Code)';;\nagents|stop) exit 1;;\n*) exec sleep 600;;\nesac\n"

// fixedMarker hands out the anchors the fixtures expect, in the order
// items first arrive.
type fixedMarker struct{ n int }

var anchors = []harness.Anchor{
	{Epoch: "e1", Bytes: 1200, Line: 14, Needle: "> fix the failing date test"},
	{Epoch: "e1", Bytes: 2210, Line: 19, Needle: "Bash(go test ./internal/clock/...)"},
	{Epoch: "e1", Bytes: 5120, Line: 41, Needle: "Update(internal/clock/parse.go)"},
	{Epoch: "e1", Bytes: 6011, Line: 48},
}

func (m *fixedMarker) Mark(context.Context, string) (harness.Anchor, error) {
	a := anchors[min(m.n, len(anchors)-1)]
	m.n++
	return a, nil
}

func (m *fixedMarker) Find(_ context.Context, _ string, a harness.Anchor) (harness.Location, error) {
	return harness.Location{Found: true, LinesFromBottom: 52 - a.Line, Needle: a.Needle}, nil
}

type nopTypist struct{}

func (nopTypist) Type(context.Context, string, string, bool, bool) error { return nil }
func (nopTypist) Keys(context.Context, string, ...string) error          { return nil }

// askLine is an AskUserQuestion call added to the transcript for the
// answer fixture: the question the pending request (oneApproval) shows.
var askLine = []byte(`{"parentUuid":"r1","isSidechain":false,"message":{"model":"claude-opus-5-5","id":"msg_09","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_09","name":"AskUserQuestion","input":{"questions":[{"question":"Which channel?","header":"Channel","multiSelect":false,"options":[{"label":"Beta","description":"Early builds"},{"label":"Stable","description":"Tested builds"}]}]}}],"stop_reason":"tool_use"},"type":"assistant","uuid":"a9","timestamp":"2026-11-03T12:00:30.000Z","cwd":"/HOME/proj","sessionId":"0b6e5f8e-1111-4222-8333-944455556666","version":"2.1.319"}
`)

type oneApproval struct{ answered string }

func (a *oneApproval) Pending(string) []wire.Approval {
	return []wire.Approval{{RequestID: "r_question01", Title: "Which channel?", CreatedAt: fixtureNow,
		Options: []wire.Option{{N: 1, Label: "Beta"}, {N: 2, Label: "Stable"}}}}
}

func (a *oneApproval) Answer(_ context.Context, id, raw, _ string) error {
	a.answered = id + " " + raw
	return nil
}

type noApprovals struct{}

func (noApprovals) Pending(string) []wire.Approval                       { return nil }
func (noApprovals) Answer(context.Context, string, string, string) error { return nil }

type fixtureEnv struct {
	approvals engine.Approvals
	home      string
	api       *api.API
	eng       *engine.Engine
}

func newFixtureEnv(t *testing.T, name string) *fixtureEnv {
	t.Helper()
	source.PollEvery = 20 * time.Millisecond
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin")
	for _, d := range []string{bin, filepath.Join(home, "proj", "internal", "clock"), filepath.Join(home, ".claude", "transcripts")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, body string, mode os.FileMode) {
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(bin, "claude"), fakeClaude, 0o755)
	write(filepath.Join(home, "proj", "internal", "clock", "parse.go"), "package clock\n", 0o644)
	write(filepath.Join(home, "proj", "internal", "clock", "clock.go"), "package clock\n", 0o644)
	tr, err := os.ReadFile("../adapters/claude/testdata/transcript-basic.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if name == "harness.action-answer.json" {
		tr = append(tr, askLine...)
	}
	write(filepath.Join(home, ".claude", "transcripts", "basic.jsonl"), strings.ReplaceAll(string(tr), "/HOME", home), 0o600)
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	// The pane's shell outlives the test by a moment; keep it from writing
	// history into the folder being removed.
	t.Setenv("HISTFILE", "/dev/null")

	togoHome := t.TempDir()
	svc := &session.Service{
		Tmux: tmuxtest.New(t), Recent: &state.Recent{File: filepath.Join(togoHome, "recent.json")},
		LockFile: filepath.Join(togoHome, "run", "bib.lock"), Shell: "/bin/sh", Path: bin + ":/usr/bin:/bin",
		NewID: func() string { return sid }, Now: func() time.Time { return fixtureNow },
	}
	status := func(string) string { return "" }
	if strings.HasPrefix(name, "harness.send-") {
		status = func(string) string { return "working" }
	}
	var approvals engine.Approvals = noApprovals{}
	if name == "harness.action-answer.json" {
		approvals = &oneApproval{}
	}
	eng := engine.New(engine.Config{
		Sessions: svc, Typist: nopTypist{}, Marker: &fixedMarker{}, Approvals: approvals,
		Status: status, LoginPath: func(context.Context) string { return svc.Path }, Home: home,
		Log: &engine.Log{NewEpoch: func() string { return "e1" }}, Now: func() time.Time { return fixtureNow },
		NewID: func() string { return "q1" }, Scale: 0.05,
	})
	t.Cleanup(func() { eng.Forget(sid) })
	return &fixtureEnv{home: home, eng: eng, approvals: approvals, api: &api.API{Sessions: svc, Home: home, Ext: harness.Methods(eng)}}
}

// warm feeds the session the hooks and status line the fixtures assume,
// one at a time so the mission revisions are the same on every run.
func (f *fixtureEnv) warm(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	d, err := f.eng.Describe(ctx, sid)
	if err != nil || d.Adapter == nil {
		return // a session no adapter serves
	}
	tr := filepath.Join(f.home, ".claude", "transcripts", "basic.jsonl")
	hook := func(v map[string]any) {
		b, _ := json.Marshal(v)
		source.Hooks.Deliver(sid, source.Payload{Kind: source.BusHook, Event: v["hook_event_name"].(string), Data: b})
	}
	hook(map[string]any{"hook_event_name": "SessionStart", "session_id": "0b6e5f8e", "transcript_path": tr, "source": "startup"})
	f.waitItems(t, "u1.turn")
	for _, ev := range []string{"TaskCreated", "TaskCompleted"} {
		for i, title := range []string{"Find the failing test", "Fix the offset handling"} {
			hook(map[string]any{"hook_event_name": ev, "task_id": string(rune('1' + i)), "task_subject": title})
			time.Sleep(30 * time.Millisecond)
		}
	}
	status, err := os.ReadFile("../adapters/claude/testdata/statusline.json")
	if err != nil {
		t.Fatal(err)
	}
	source.Hooks.Deliver(sid, source.Payload{Kind: source.BusStatusline, Data: status})
	f.waitMission(t, func(m harness.Mission) bool {
		return m.Summary.Plan != nil && m.Summary.Plan.Done == 2 && m.Sections[len(m.Sections)-1].Status != nil
	})
}

type nopSub struct{ done chan struct{} }

func (nopSub) Notify(harness.Notification) error { return nil }
func (s nopSub) Done() <-chan struct{}           { return s.done }

func (f *fixtureEnv) open(t *testing.T) harness.OpenResult {
	sub := nopSub{done: make(chan struct{})}
	defer close(sub.done)
	res, err := f.eng.Open(context.Background(), sub, sid, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.eng.Close(context.Background(), sub, sid)
	return res
}

func (f *fixtureEnv) waitItems(t *testing.T, id string) {
	t.Helper()
	f.waitMission(t, func(harness.Mission) bool {
		for _, it := range f.open(t).Items {
			if it.ID == id {
				return true
			}
		}
		return false
	})
}

func (f *fixtureEnv) waitMission(t *testing.T, ok func(harness.Mission) bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !ok(f.open(t).Mission); {
		if time.Now().After(deadline) {
			t.Fatalf("the session never settled: %+v", f.open(t))
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
}

type fixture struct {
	Setup    []json.RawMessage `json:"setup"`
	Request  json.RawMessage   `json:"request"`
	Response json.RawMessage   `json:"response"`
}

// serve runs the fixture's setup, warms the session, then sends the
// request and returns its response.
func (f *fixtureEnv) serve(t *testing.T, fx fixture) json.RawMessage {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	info := rpc.HelperInfo{Version: "0.0.0-test", OS: runtime.GOOS, Arch: runtime.GOARCH, Hostname: "testhost",
		Capabilities: []string{"session.v1", harness.Capability}, Concurrent: harness.Concurrent}
	go func() { _ = rpc.Serve(context.Background(), inR, outW, f.api, info); outW.Close() }()
	defer inW.Close()
	read := func(id any) json.RawMessage {
		for {
			raw, err := rpc.ReadFrame(outR)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var m struct {
				ID     any             `json:"id"`
				Result json.RawMessage `json:"result"`
			}
			_ = json.Unmarshal(raw, &m)
			if m.ID != nil && toJSON(m.ID) == toJSON(id) {
				return raw
			}
		}
	}
	idOf := func(req json.RawMessage) any {
		var m struct {
			ID any `json:"id"`
		}
		_ = json.Unmarshal(req, &m)
		return m.ID
	}
	for i, req := range fx.Setup {
		if err := rpc.WriteFrame(inW, req); err != nil {
			t.Fatal(err)
		}
		if resp := read(idOf(req)); !strings.Contains(string(resp), `"result"`) {
			t.Fatalf("setup request %d failed: %s", i, resp)
		}
		if strings.Contains(string(req), `"session.create"`) {
			f.warm(t)
		}
	}
	var req struct {
		Params struct {
			Action string `json:"action"`
			Target string `json:"target"`
		} `json:"params"`
	}
	if json.Unmarshal(fx.Request, &req) == nil && req.Params.Action == "answer" {
		// The question must have reached the log before it is answered.
		f.waitItems(t, req.Params.Target)
	}
	if err := rpc.WriteFrame(inW, fx.Request); err != nil {
		t.Fatal(err)
	}
	return read(idOf(fx.Request))
}

func TestHarnessFixtures(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join(fixturesDir, "harness.*.json"))
	if len(paths) < 16 {
		t.Fatalf("found %d harness fixtures", len(paths))
	}
	for _, path := range paths {
		name := filepath.Base(path)
		t.Run(strings.TrimSuffix(name, ".json"), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fx fixture
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			env := newFixtureEnv(t, name)
			resp := env.serve(t, fx)
			var full map[string]any
			_ = json.Unmarshal(resp, &full)
			var want any
			_ = json.Unmarshal(fx.Response, &want)
			delete(full, "jsonrpc")
			wantMap, _ := want.(map[string]any)
			delete(wantMap, "jsonrpc")
			if msg := match(wantMap, full, "$"); msg != "" {
				t.Errorf("%s\nresponse: %s", msg, resp)
			}
			if a, ok := env.approvals.(*oneApproval); ok && a.answered != "r_question01 option:2" {
				t.Errorf("answered %q, want r_question01 option:2", a.answered)
			}
		})
	}
}

// match compares got to want; "<any>" matches anything.
func match(want, got any, at string) string {
	switch w := want.(type) {
	case string:
		if w == "<any>" {
			return ""
		}
		if g, ok := got.(string); !ok || g != w {
			return at + ": got " + toJSON(got) + ", want " + toJSON(w)
		}
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			return at + ": got " + toJSON(got) + ", want " + toJSON(want)
		}
		for k, wv := range w {
			gv, present := g[k]
			if !present {
				return at + "." + k + ": missing"
			}
			if msg := match(wv, gv, at+"."+k); msg != "" {
				return msg
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return at + ": got " + toJSON(got) + ", want " + toJSON(want)
		}
		for i := range w {
			if msg := match(w[i], g[i], at+"[]"); msg != "" {
				return msg
			}
		}
	default:
		if toJSON(want) != toJSON(got) {
			return at + ": got " + toJSON(got) + ", want " + toJSON(want)
		}
	}
	return ""
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
