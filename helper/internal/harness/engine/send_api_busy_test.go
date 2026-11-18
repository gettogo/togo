package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakebusy", []byte(`
adapter: fakebusy
name: Fake API
revision: 1
match: { agents: [fakebusy] }
capabilities: [chat, send.api, send.queue, interrupt]
actions:
  send: { cli: { argv: [rec, send, '{{.text}}'] }, stability: documented }
`))
}

// Seen live on Kilo 7.8 and OpenCode 2.3: a prompt sent through the harness
// API while a turn ran went straight in (OpenCode recorded it as steer) and
// came back queued=false. The API transport follows the same rule as typing:
// "now" is refused while the agent works, "queue" waits in the helper and
// goes out, in order, once the turn ends.
func TestAPISendFollowsBusyAndQueueRules(t *testing.T) {
	const id = "s_fakebusy01"
	bin, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "rec"), []byte("#!/bin/sh\necho \"$@\" >> rec.log\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var status atomic.Value
	status.Store("working")
	pane := &seqPane{}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakebusy", Managed: true, Cwd: home, Command: []string{"fakebusy"}}},
		Typist:   pane, Marker: &fakeMarker{}, Home: home,
		Status:    func(string) string { v, _ := status.Load().(string); return v },
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	sent := func() string {
		b, _ := os.ReadFile(filepath.Join(home, "rec.log"))
		return string(b)
	}

	if _, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "right now", Mode: harness.SendNow}); !errors.Is(err, harness.ErrBusy) {
		t.Fatalf("now while working = %v, want ErrBusy", err)
	}
	first := queue(t, e, id, "first")
	queue(t, e, id, "second")
	if got := sent(); got != "" {
		t.Fatalf("sent while the agent works: %q", got)
	}

	status.Store("idle")
	waitFor(t, func() bool { return strings.Contains(sent(), "send first") })
	// The request returns before the item is published as sent.
	waitFor(t, func() bool {
		it, ok := e.log.Get(id, first)
		return ok && it.User != nil && !it.User.Queued
	})
	// The first prompt starts a turn; the second waits for that one too.
	status.Store("working")
	if strings.Contains(sent(), "second") {
		t.Fatalf("second went out with the first: %q", sent())
	}
	status.Store("idle")
	waitFor(t, func() bool { return strings.Contains(sent(), "send second") })
	if got := sent(); strings.Index(got, "first") > strings.Index(got, "second") {
		t.Errorf("order = %q", got)
	}
	if _, typed := pane.snapshot(); len(typed) != 0 {
		t.Errorf("typed %q, want the prompts sent through the API only", typed)
	}
}

// A queued prompt whose API request fails may still have reached the
// harness, so it is tried once: it leaves the queue and a notice carries
// its text.
func TestQueuedAPISendIsNotRepeatedAfterAFailure(t *testing.T) {
	const id = "s_fakebusy02"
	bin, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "rec"), []byte("#!/bin/sh\necho \"$@\" >> rec.log\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var status atomic.Value
	status.Store("working")
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakebusy", Managed: true, Cwd: home, Command: []string{"fakebusy"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		Status:    func(string) string { v, _ := status.Load().(string); return v },
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	held := queue(t, e, id, "only once")
	status.Store("idle")
	waitFor(t, func() bool {
		it, ok := e.log.Get(id, held)
		return ok && it.User != nil && it.User.Withdrawn
	})
	notice, ok := e.log.Get(id, "unsent-"+held)
	if !ok || notice.Notice == nil || !strings.Contains(notice.Notice.Text, "only once") {
		t.Fatalf("notice = %+v, want one that carries the prompt", notice)
	}
	// Long enough for several retries of the typed path.
	time.Sleep(600 * time.Millisecond)
	b, _ := os.ReadFile(filepath.Join(home, "rec.log"))
	if n := strings.Count(string(b), "only once"); n != 1 {
		t.Errorf("sent %d times, want 1", n)
	}
}
