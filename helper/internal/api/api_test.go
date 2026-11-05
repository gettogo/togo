package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/fsread"
	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/push"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/upload"
)

const fixturesDir = "../../../protocol/v1/fixtures"

// fixtureNeedsFake names fixtures that deviate from the shared harness and
// run in their own test.
var fixtureNeedsFake = map[string]bool{
	"tmux-unavailable.json": true, "ports.list.json": true,
	"events.subscribe.json": true, "events.keepalive.json": true, "session.setMute.json": true,
	"approval.list.json": true, "approval.answer-conflict.json": true,
}

type fixture struct {
	Setup    []json.RawMessage `json:"setup"`
	Request  json.RawMessage   `json:"request"`
	Response json.RawMessage   `json:"response"`
}

// harness returns an API over a fresh HOME holding the directories and file
// the fixtures expect. HOME is symlink-resolved so tmux's pane path equals it.
func harness(t *testing.T, tm *tmux.Client) (a *api.API, home string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"alpha", "Beta", "proj", ".cache"} {
		if err := os.Mkdir(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")

	togoHome := t.TempDir()
	recent := &state.Recent{File: filepath.Join(togoHome, "recent.json")}
	svc := &session.Service{
		Tmux:     tm,
		Recent:   recent,
		LockFile: filepath.Join(togoHome, "run", "bib.lock"),
		Shell:    "/bin/sh",
		// An empty directory holds none of the agent commands, so only the shell is installed.
		Path:  t.TempDir(),
		NewID: func() string { return "s_abcdefghij" },
		Now:   func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) },
	}
	g := &gitx.Service{Sessions: svc}
	f := &fsread.Service{Sessions: svc}
	ext := g.Methods()
	maps.Copy(ext, f.Methods())
	maps.Copy(ext, (&upload.Service{Home: togoHome, Sessions: svc}).Methods())
	// The pairing fixtures fix the host secrets: mailbox secret bytes
	// 0x00..0x1f, then X25519 private key bytes 0x20..0x3f.
	seq := make([]byte, 64)
	for i := range seq {
		seq[i] = byte(i)
	}
	p := &push.Service{Home: togoHome, Rand: bytes.NewReader(seq)}
	maps.Copy(ext, p.Methods())
	return &api.API{Sessions: svc, Recent: recent, Home: home, Ext: ext}, home
}

// exchange sends the setup requests then the request over one connection and
// returns every response in order.
func exchange(t *testing.T, a *api.API, fx fixture) []json.RawMessage {
	t.Helper()
	var in bytes.Buffer
	for _, req := range append(append([]json.RawMessage{}, fx.Setup...), fx.Request) {
		if err := rpc.WriteFrame(&in, req); err != nil {
			t.Fatal(err)
		}
	}
	info := rpc.HelperInfo{
		Version: "0.0.0-test", OS: runtime.GOOS, Arch: runtime.GOARCH, Hostname: "testhost",
		Capabilities: []string{"session.v1", "agents.v1", "fs.v1", "capture.v1"},
	}
	var out bytes.Buffer
	if err := rpc.Serve(context.Background(), &in, &out, a, info); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resps []json.RawMessage
	for out.Len() > 0 {
		raw, err := rpc.ReadFrame(&out)
		if err != nil {
			t.Fatal(err)
		}
		resps = append(resps, raw)
	}
	return resps
}

func runFixture(t *testing.T, a *api.API, home, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fx fixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	resps := exchange(t, a, fx)
	if len(resps) != len(fx.Setup)+1 {
		t.Fatalf("got %d responses, want %d", len(resps), len(fx.Setup)+1)
	}
	for i, r := range resps[:len(fx.Setup)] {
		var m map[string]any
		_ = json.Unmarshal(r, &m)
		if _, ok := m["result"]; !ok {
			t.Fatalf("setup request %d failed: %s", i, r)
		}
	}
	var got, want any
	if err := json.Unmarshal(resps[len(resps)-1], &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fx.Response, &want); err != nil {
		t.Fatal(err)
	}
	if msg := match(want, got, home, "$"); msg != "" {
		t.Errorf("%s\nresponse: %s", msg, resps[len(resps)-1])
	}
}

// match compares got to want, honouring the "<any>" and "<home>" rules of
// the fixture format. It returns "" on a match.
func match(want, got any, home, at string) string {
	switch w := want.(type) {
	case string:
		if w == "<any>" {
			return ""
		}
		if strings.HasPrefix(w, "<home>") {
			w = home + strings.TrimPrefix(w, "<home>")
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
			if msg := match(wv, gv, home, at+"."+k); msg != "" {
				return msg
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return at + ": got " + toJSON(got) + ", want " + toJSON(want)
		}
		for i := range w {
			if msg := match(w[i], g[i], home, at+"[]"); msg != "" {
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

func TestFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if fixtureNeedsFake[name] {
			continue
		}
		t.Run(strings.TrimSuffix(name, ".json"), func(t *testing.T) {
			a, home := harness(t, tmuxtest.New(t))
			runFixture(t, a, home, path)
		})
	}
}

func TestTmuxUnavailable(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'tmux 2.9'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, home := harness(t, &tmux.Client{Bin: bin, Socket: []string{"-L", "togo-test-fake"}})
	runFixture(t, a, home, filepath.Join(fixturesDir, "tmux-unavailable.json"))
}

func call(t *testing.T, a *api.API, method, params string) (any, *rpc.Error) {
	t.Helper()
	return a.Handle(context.Background(), method, json.RawMessage(params))
}

func TestMalformedParamsAreInvalidParams(t *testing.T) {
	a, _ := harness(t, &tmux.Client{Bin: "tmux-not-installed"})
	for name, c := range map[string][2]string{
		"not an object":   {"fs.listDir", `[1]`},
		"wrong type":      {"session.create", `{"cols":"wide"}`},
		"missing path":    {"fs.listDir", `{}`},
		"missing id":      {"session.kill", `{}`},
		"fractional rows": {"session.create", `{"rows":1.5}`},
	} {
		t.Run(name, func(t *testing.T) {
			_, e := call(t, a, c[0], c[1])
			if e == nil || e.Code != rpc.CodeInvalidParams {
				t.Fatalf("got %+v, want -32602", e)
			}
			data, _ := e.Data.(map[string]string)
			if data["reason"] == "" {
				t.Errorf("missing data.reason: %+v", e)
			}
		})
	}
}

func TestUnknownMethod(t *testing.T) {
	a, _ := harness(t, &tmux.Client{Bin: "tmux-not-installed"})
	_, e := call(t, a, "ports.list", `{}`)
	if e == nil || e.Code != rpc.CodeMethodNotFound || e.Message != "method not found" {
		t.Fatalf("got %+v", e)
	}
}

func TestTmuxMissingReportsNullFound(t *testing.T) {
	a, _ := harness(t, &tmux.Client{Bin: filepath.Join(t.TempDir(), "absent")})
	_, e := call(t, a, "session.list", `{}`)
	if e == nil || e.Code != rpc.CodeTmuxUnavailable {
		t.Fatalf("got %+v", e)
	}
	b, _ := json.Marshal(e)
	if !strings.Contains(string(b), `"data":{"found":null,"required":"3.2"}`) {
		t.Errorf("error JSON: %s", b)
	}
}

// agents.list may run the login shell, which can take seconds; a client
// that gives up on the call must stop it, so the lookup takes the request
// context.
func TestAgentsListUsesRequestContext(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(t.TempDir(), "fakesh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nPATH="+bin+"\neval \"$2\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	t.Setenv("PATH", t.TempDir())
	a := &api.API{Sessions: &session.Service{}}

	claudeInstalled := func(ctx context.Context) bool {
		t.Helper()
		res, rerr := a.Handle(ctx, "agents.list", nil)
		if rerr != nil {
			t.Fatalf("agents.list: %+v", rerr)
		}
		var got struct {
			Agents []struct {
				ID        string `json:"id"`
				Installed bool   `json:"installed"`
			} `json:"agents"`
		}
		if err := json.Unmarshal([]byte(toJSON(res)), &got); err != nil {
			t.Fatal(err)
		}
		for _, ag := range got.Agents {
			if ag.ID == "claude" {
				return ag.Installed
			}
		}
		t.Fatal("agents.list has no claude entry")
		return false
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if claudeInstalled(cancelled) {
		t.Fatal("agents.list with a cancelled request ran the login shell")
	}
	if !claudeInstalled(context.Background()) {
		t.Fatal("agents.list did not find claude on the login PATH")
	}
}
