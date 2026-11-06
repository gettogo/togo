package daemon

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

// TestComposeServesRestore ends a Claude Code session, loses the tmux
// server under it and restores it through the composed handler: the
// restored session carries no exit from the run before the restart. The
// stand-in claude exits on its first run, so the exit report counts only
// once the agent has really gone, and waits on the resumed run.
func TestComposeServesRestore(t *testing.T) {
	if !slices.Contains(Capabilities, wire.CapRestore) {
		t.Fatalf("capabilities %q lack %s", Capabilities, wire.CapRestore)
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin")
	for _, d := range []string{filepath.Join(home, "proj"), bin} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(lifecycleClaude["session.relaunch"]), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	tm := tmuxtest.New(t)
	if p, err := exec.LookPath(tm.Bin); err == nil {
		tm.Bin = p
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	togoHome := t.TempDir()
	recent := &state.Recent{File: filepath.Join(togoHome, "recent.json")}
	svc := &session.Service{
		Tmux: tm, Recent: recent, Shell: fixtureShell(t), Path: bin,
		LockFile: filepath.Join(togoHome, "run", "bib.lock"),
		Manifest: &session.Manifest{Path: filepath.Join(togoHome, "sessions.json")},
		NewID:    func() string { return "s_abcdefghij" },
		Now:      func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) },
	}
	c, err := Compose(&api.API{Sessions: svc, Recent: recent, Home: togoHome}, ComposeOptions{Home: togoHome})
	if err != nil {
		t.Fatal(err)
	}
	h := c.Handler()
	hello := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":1,"client":{"name":"t","version":"0"},"capabilities":[]}}`)
	serve(t, h, hello, json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"session.create","params":{"agent":"claude","cwd":"`+home+`/proj","args":["--model","opus"]}}`))
	reportExit(t, c, "s_abcdefghij", 0)
	if err := exec.Command(tm.Bin, append(append([]string{}, tm.Socket...), "kill-server")...).Run(); err != nil {
		t.Fatal(err)
	}

	var listed struct {
		Result wire.RestorableResult `json:"result"`
	}
	raw := serve(t, h, hello, json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"session.restorable","params":{}}`))
	if err := json.Unmarshal(raw, &listed); err != nil || len(listed.Result.Sessions) != 1 || !listed.Result.Sessions[0].Resumable {
		t.Fatalf("session.restorable %s", raw)
	}
	var restored struct {
		Result session.Session `json:"result"`
	}
	raw = serve(t, h, hello, json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"session.restore","params":{"id":"s_abcdefghij","resume":true}}`))
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if s := restored.Result; s.ID != "s_abcdefghij" || s.Exit != nil || s.Status == wire.StatusEnded || *s.Bib != "01" {
		t.Fatalf("session.restore %s", raw)
	}
	raw = serve(t, h, hello, json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"session.dismiss","params":{"id":"s_abcdefghij"}}`))
	var dismissed struct {
		Error struct{ Code int } `json:"error"`
	}
	if err := json.Unmarshal(raw, &dismissed); err != nil || dismissed.Error.Code != -32001 {
		t.Fatalf("dismissing a running session %s, want -32001", raw)
	}
}
