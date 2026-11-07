package source

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func testEnv(t *testing.T) (harness.Env, string) {
	t.Helper()
	h, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(h, "proj")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	return harness.Env{SessionID: "s_abcdefghij", Cwd: cwd, Vars: map[string]string{VarHome: h}}, h
}

func sspec(name string, v any) harness.SourceSpec {
	b, _ := json.Marshal(v)
	return harness.SourceSpec{Name: name, Raw: b}
}

func writeExe(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestCLIFormatsAndLimits(t *testing.T) {
	env, h := testEnv(t)
	bin := filepath.Join(h, "bin")
	_ = os.Mkdir(bin, 0o755)
	env.Vars[VarPath] = bin + ":/usr/bin:/bin"
	writeExe(t, bin, "fakejson", `echo '{"cwd":"'"$(pwd)"'"}'; echo noise >&2`)
	writeExe(t, bin, "fakejsonl", "printf '{\"a\":1}\\nbad\\n{\"a\":2}\\n'")
	writeExe(t, bin, "faketoml", "printf 'model = \"o3\"\\n'")
	writeExe(t, bin, "slow", "sleep 5")
	writeExe(t, bin, "big", "head -c 2000000 /dev/zero | tr '\\0' 'x'")
	writeExe(t, bin, "fails", "exit 3")
	ctx := context.Background()
	out, err := CLI{}.Fetch(ctx, sspec("s", map[string]any{"argv": []string{"fakejson"}}), env)
	if err != nil || !strings.Contains(string(out), env.Cwd) {
		t.Fatalf("json: %s %v", out, err)
	}
	out, err = CLI{}.Fetch(ctx, sspec("s", map[string]any{"argv": []string{"fakejsonl"}, "format": "jsonl"}), env)
	if err != nil || string(out) != `[{"a":1},{"a":2}]` {
		t.Fatalf("jsonl: %s %v", out, err)
	}
	out, err = CLI{}.Fetch(ctx, sspec("s", map[string]any{"argv": []string{"faketoml"}, "format": "toml"}), env)
	if err != nil || string(out) != `{"model":"o3"}` {
		t.Fatalf("toml: %s %v", out, err)
	}
	tctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := (CLI{}).Fetch(tctx, sspec("s", map[string]any{"argv": []string{"slow"}}), env); err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("timeout not honoured: %v", err)
	}
	if _, err := (CLI{}).Fetch(ctx, sspec("s", map[string]any{"argv": []string{"big"}, "format": "text"}), env); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("cap: %v", err)
	}
	if _, err := (CLI{}).Fetch(ctx, sspec("s", map[string]any{"argv": []string{"fails"}}), env); err == nil {
		t.Fatal("exit status ignored")
	}
	if _, err := (CLI{}).Fetch(ctx, sspec("s", map[string]any{"argv": []string{"missing-prog"}}), env); err == nil {
		t.Fatal("missing program ran")
	}
	if err := (CLI{}).Watch(ctx, sspec("s", nil), env, nil); !errors.Is(err, harness.ErrNotStreaming) {
		t.Fatal("cli streams")
	}
}

func TestFileConfinement(t *testing.T) {
	env, h := testEnv(t)
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	secret := filepath.Join(outside, "secret.json")
	_ = os.WriteFile(secret, []byte(`{"x":1}`), 0o600)
	_ = os.WriteFile(filepath.Join(h, "ok.json"), []byte(`{"ok":true}`), 0o600)
	_ = os.Symlink(secret, filepath.Join(env.Cwd, "link.json"))
	f := File{kind: "file"}
	ctx := context.Background()
	if out, err := f.Fetch(ctx, sspec("s", map[string]any{"path": "~/ok.json"}), env); err != nil || string(out) != `{"ok":true}` {
		t.Fatalf("home file: %s %v", out, err)
	}
	if _, err := f.Fetch(ctx, sspec("s", map[string]any{"path": secret}), env); !errors.Is(err, ErrConfined) {
		t.Fatalf("outside path: %v", err)
	}
	if _, err := f.Fetch(ctx, sspec("s", map[string]any{"path": "link.json"}), env); !errors.Is(err, ErrConfined) {
		t.Fatalf("symlink escape: %v", err)
	}
	if _, err := f.Fetch(ctx, sspec("s", map[string]any{"path": "~/../" + filepath.Base(outside) + "/secret.json"}), env); err == nil {
		t.Fatal("dot-dot escape")
	}
	big := filepath.Join(h, "big.json")
	_ = os.WriteFile(big, make([]byte, MaxFileRead+1), 0o600)
	if _, err := f.Fetch(ctx, sspec("s", map[string]any{"path": big}), env); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("read cap: %v", err)
	}
}

func TestGlobListsNewestFirst(t *testing.T) {
	env, h := testEnv(t)
	dir := filepath.Join(h, ".claude", "projects", "p")
	_ = os.MkdirAll(dir, 0o755)
	old := filepath.Join(dir, "old.jsonl")
	_ = os.WriteFile(old, []byte(`{"type":"ai-title","aiTitle":"Old"}`+"\n"), 0o600)
	_ = os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	_ = os.WriteFile(filepath.Join(dir, "new.jsonl"), []byte(`{"type":"user"}`+"\n"+`{"type":"ai-title","aiTitle":"New"}`+"\n"), 0o600)
	out, err := File{kind: "file"}.Fetch(context.Background(), sspec("s", map[string]any{"glob": "~/.claude/projects/p/*.jsonl", "format": "jsonl"}), env)
	if err != nil {
		t.Fatal(err)
	}
	var list []GlobEntry
	_ = json.Unmarshal(out, &list)
	if len(list) != 2 || list[0].Name != "new" || len(list[0].Tail) != 2 || len(list[1].Head) != 1 {
		t.Fatalf("glob = %s", out)
	}
	if _, err := (File{kind: "file"}).Fetch(context.Background(), sspec("s", map[string]any{"glob": "~/nothing/*.jsonl"}), env); err == nil {
		t.Fatal("empty glob is not an error")
	}
}

type collector struct {
	mu   sync.Mutex
	recs []string
}

func (c *collector) emit(r harness.Record) {
	c.mu.Lock()
	c.recs = append(c.recs, string(r.Data))
	c.mu.Unlock()
}

func (c *collector) wait(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		got := append([]string(nil), c.recs...)
		c.mu.Unlock()
		if len(got) >= n {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("got %d records, want %d", len(c.recs), n)
	return nil
}

func TestJSONLTailFollowsAndRestartsOnTruncation(t *testing.T) {
	PollEvery = 20 * time.Millisecond
	env, h := testEnv(t)
	p := filepath.Join(h, "t.jsonl")
	_ = os.WriteFile(p, []byte(`{"n":1}`+"\n"+`{"n":2}`+"\n"+`{"n":3`), 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() {
		_ = File{kind: "jsonl"}.Watch(ctx, sspec("t", map[string]any{"path": p, "follow": true}), env, c.emit)
	}()
	c.wait(t, 2)
	fh, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = fh.WriteString("}\n" + `{"n":4}` + "\n")
	fh.Close()
	got := c.wait(t, 4)
	if got[2] != `{"n":3}` || got[3] != `{"n":4}` {
		t.Fatalf("tail = %v", got)
	}
	_ = os.WriteFile(p, []byte(`{"n":9}`+"\n"), 0o600)
	got = c.wait(t, 5)
	if got[4] != `{"n":9}` {
		t.Fatalf("after truncation = %v", got)
	}
}

func TestJSONLGlobTailsNewestThenNewFiles(t *testing.T) {
	PollEvery, globEvery = 20*time.Millisecond, 20*time.Millisecond
	env, h := testEnv(t)
	old := filepath.Join(h, "a.jsonl")
	_ = os.WriteFile(old, []byte(`{"f":"old"}`+"\n"), 0o600)
	_ = os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	_ = os.WriteFile(filepath.Join(h, "b.jsonl"), []byte(`{"f":"newest"}`+"\n"), 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() {
		_ = File{kind: "jsonl"}.Watch(ctx, sspec("t", map[string]any{"glob": "~/*.jsonl", "follow": true}), env, c.emit)
	}()
	got := c.wait(t, 1)
	if got[0] != `{"f":"newest"}` {
		t.Fatalf("first = %v", got)
	}
	time.Sleep(50 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(h, "c.jsonl"), []byte(`{"f":"c"}`+"\n"), 0o600)
	got = c.wait(t, 2)
	if got[1] != `{"f":"c"}` {
		t.Fatalf("new file = %v", got)
	}
}

// A session started after the newest file was last written belongs to a new
// conversation: an earlier session's file in the same folder is not replayed
// into it, only joined once it changes (a resume that writes to it).
func TestJSONLGlobSkipsFilesOlderThanTheSession(t *testing.T) {
	PollEvery, globEvery = 20*time.Millisecond, 20*time.Millisecond
	env, h := testEnv(t)
	prev := filepath.Join(h, "prev.jsonl")
	_ = os.WriteFile(prev, []byte(`{"f":"previous session"}`+"\n"), 0o600)
	_ = os.Chtimes(prev, time.Now().Add(-time.Minute), time.Now().Add(-time.Minute))
	env.Vars["created"] = time.Now().UTC().Format(time.RFC3339)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() {
		_ = File{kind: "jsonl"}.Watch(ctx, sspec("t", map[string]any{"glob": "~/*.jsonl", "follow": true}), env, c.emit)
	}()
	time.Sleep(150 * time.Millisecond)
	c.mu.Lock()
	early := append([]string(nil), c.recs...)
	c.mu.Unlock()
	if len(early) != 0 {
		t.Fatalf("replayed an earlier session's file: %v", early)
	}
	_ = os.WriteFile(filepath.Join(h, "mine.jsonl"), []byte(`{"f":"mine"}`+"\n"), 0o600)
	if got := c.wait(t, 1); got[0] != `{"f":"mine"}` {
		t.Fatalf("first = %v", got)
	}
}

// After a helper restart the running session's own file, written since the
// session began, is replayed at once.
func TestJSONLGlobReplaysTheSessionsOwnFile(t *testing.T) {
	PollEvery, globEvery = 20*time.Millisecond, 20*time.Millisecond
	env, h := testEnv(t)
	env.Vars["created"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	_ = os.WriteFile(filepath.Join(h, "own.jsonl"), []byte(`{"f":"own"}`+"\n"), 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() {
		_ = File{kind: "jsonl"}.Watch(ctx, sspec("t", map[string]any{"glob": "~/*.jsonl", "follow": true}), env, c.emit)
	}()
	if got := c.wait(t, 1); got[0] != `{"f":"own"}` {
		t.Fatalf("first = %v", got)
	}
}

func TestHookBusBacklogFilterAndProjection(t *testing.T) {
	env, _ := testEnv(t)
	bus := NewBus()
	bus.Deliver(env.SessionID, Payload{Kind: BusHook, Event: "SessionStart", Data: json.RawMessage(`{"transcript_path":"/x","api_key":"k"}`)})
	bus.Deliver(env.SessionID, Payload{Kind: BusHook, Event: "Other", Data: json.RawMessage(`{}`)})
	bus.Deliver(env.SessionID, Payload{Kind: BusStatusline, Data: json.RawMessage(`{"model":{"id":"opus","x":1},"cost":{"total_cost_usd":1},"extra":2}`)})
	bus.Deliver(env.SessionID, Payload{Kind: BusHook, Data: json.RawMessage(`not json`)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hooks, status := &collector{}, &collector{}
	go func() {
		_ = NewHook("hook", bus).Watch(ctx, sspec("hooks", map[string]any{"events": []string{"SessionStart", "Stop"}}), env, hooks.emit)
	}()
	go func() {
		_ = NewHook("statusline", bus).Watch(ctx, sspec("status", map[string]any{"fields": []string{"model.id", "cost.total_cost_usd"}}), env, status.emit)
	}()
	got := hooks.wait(t, 1)
	if got[0] != `{"api_key":null,"transcript_path":"/x"}` {
		t.Fatalf("hook backlog = %v", got)
	}
	got = status.wait(t, 1)
	if got[0] != `{"cost":{"total_cost_usd":1},"model":{"id":"opus"}}` {
		t.Fatalf("status = %v", got)
	}
	time.Sleep(20 * time.Millisecond)
	bus.Deliver(env.SessionID, Payload{Kind: BusHook, Event: "Stop", Data: json.RawMessage(`{"n":2}`)})
	if got := hooks.wait(t, 2); got[1] != `{"n":2}` || len(got) != 2 {
		t.Fatalf("live = %v", got)
	}
}

func TestStatic(t *testing.T) {
	out, err := Static{}.Fetch(context.Background(), harness.SourceSpec{Raw: json.RawMessage(`[1]`)}, harness.Env{})
	if err != nil || string(out) != "[1]" {
		t.Fatal(out, err)
	}
}
