package watch

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// TestReconcileSettlesDeskKills kills a session outside togo while the
// server is up: the next reconcile drops its record, so when the server
// dies later only the session that was still running is offered.
func TestReconcileSettlesDeskKills(t *testing.T) {
	c := tmuxtest.New(t)
	if p, err := exec.LookPath(c.Bin); err == nil {
		c.Bin = p
	}
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	svc := &session.Service{
		Tmux: c, Shell: "/bin/sh",
		Recent:   &state.Recent{File: filepath.Join(dir, "recent.json")},
		LockFile: filepath.Join(dir, "run", "bib.lock"),
		Manifest: &session.Manifest{Path: filepath.Join(dir, "sessions.json")},
	}
	ctx := context.Background()
	ended, err := svc.Create(ctx, session.CreateOpts{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	kept, err := svc.Create(ctx, session.CreateOpts{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	w, err := New(Config{Tmux: c, Sessions: svc, TogoPath: "/nonexistent/togo", Home: dir})
	if err != nil {
		t.Fatal(err)
	}
	w.Reconcile(ctx)
	if err := exec.Command(c.Bin, append(append([]string{}, c.Socket...), "kill-session", "-t", "="+ended.ID)...).Run(); err != nil {
		t.Fatal(err)
	}
	w.Reconcile(ctx)
	all, err := svc.Manifest.All()
	if err != nil || len(all) != 1 || all[0].ID != kept.ID {
		t.Fatalf("record %+v %v, want only %s", all, err, kept.ID)
	}
	if err := exec.Command(c.Bin, append(append([]string{}, c.Socket...), "kill-server")...).Run(); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Restorable(ctx)
	if err != nil || len(got) != 1 || got[0].ID != kept.ID {
		t.Fatalf("restorable %+v %v, want only %s", got, err, kept.ID)
	}
}
