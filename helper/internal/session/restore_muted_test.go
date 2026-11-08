package session_test

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/session"
)

// A muted session (every test session) must come back muted after a host
// restart, whether it was created muted or muted later; one unmuted later
// comes back unmuted.
func TestRestoreKeepsMute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	recorder(t, s, dir, "claude")
	ctx := context.Background()
	born := mustCreate(t, s, session.CreateOpts{Agent: "claude", Cwd: dir, Muted: true})
	later := mustCreate(t, s, session.CreateOpts{Agent: "claude", Cwd: dir})
	if _, err := s.SetMuted(ctx, later.ID, true); err != nil {
		t.Fatal(err)
	}
	undone := mustCreate(t, s, session.CreateOpts{Agent: "claude", Cwd: dir, Muted: true})
	if _, err := s.SetMuted(ctx, undone.ID, false); err != nil {
		t.Fatal(err)
	}
	killServer(t, s)
	for id, want := range map[string]bool{born.ID: true, later.ID: true, undone.ID: false} {
		got, err := s.Restore(ctx, id, false)
		if err != nil {
			t.Fatal(err)
		}
		if got.Muted != want {
			t.Errorf("%s restored muted=%v, want %v", id, got.Muted, want)
		}
	}
}
