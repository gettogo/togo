package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// A native screen and the engine's typing take the same lock for a session,
// so keys already on their way finish before the terminal is handed to the
// phone, and none start after. The daemon wires the two together.
func TestNativeScreenAndTypingShareOneLock(t *testing.T) {
	togoHome := t.TempDir()
	recent := &state.Recent{File: filepath.Join(togoHome, "recent.json")}
	svc := &session.Service{
		Tmux: tmuxtest.New(t), Recent: recent, Shell: "/bin/sh", Path: t.TempDir(),
		LockFile: filepath.Join(togoHome, "run", "bib.lock"),
	}
	c, err := Compose(&api.API{Sessions: svc, Recent: recent, Home: togoHome}, ComposeOptions{Home: togoHome})
	if err != nil {
		t.Fatal(err)
	}
	if c.Surface == nil || c.Surface.BeforeOpen == nil {
		t.Fatal("the native surface service is not told how to wait for typing")
	}
	const id = "s_aaaaaaaaaa"
	typing := engine.LockTyping(id)
	taken := make(chan struct{})
	go func() {
		c.Surface.BeforeOpen(id)()
		close(taken)
	}()
	select {
	case <-taken:
		t.Fatal("a native screen was admitted while keys were being typed")
	case <-time.After(150 * time.Millisecond):
	}
	typing()
	select {
	case <-taken:
	case <-time.After(2 * time.Second):
		t.Fatal("a native screen was never admitted after the typing ended")
	}
	// Another session's typing does not hold this one up.
	other := engine.LockTyping("s_bbbbbbbbbb")
	defer other()
	done := make(chan struct{})
	go func() { c.Surface.BeforeOpen(id)(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("one session's typing held up another session's native screen")
	}
	if c.Surface.Held(id) {
		t.Error("a session nobody opened is reported held")
	}
}
