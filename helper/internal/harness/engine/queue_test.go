package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// blockPane holds every Type until release is closed.
type blockPane struct {
	seqPane
	started chan string
	release chan struct{}
}

func (p *blockPane) Type(ctx context.Context, id, text string, submit, paste bool) error {
	p.started <- text
	<-p.release
	return p.seqPane.Type(ctx, id, text, submit, paste)
}

func statusEngine(t *testing.T, id string, pane harnessTypist, status *atomic.Value) *Engine {
	t.Helper()
	bin := fakeBin(t, "1.2.3")
	home := t.TempDir()
	e := New(Config{
		Sessions: fakeSessions{id: {ID: id, Agent: "fake", Managed: true, Cwd: home, Command: []string{"fake"}}},
		Typist:   pane, Marker: &fakeMarker{}, Home: home,
		Status:    func(string) string { v, _ := status.Load().(string); return v },
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return e
}

type harnessTypist interface {
	Type(ctx context.Context, id, text string, submit, paste bool) error
	Keys(ctx context.Context, id string, keys ...string) error
}

func queue(t *testing.T, e *Engine, id, text string) string {
	t.Helper()
	r, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: text, Mode: harness.SendQueue})
	if err != nil || r.Item.User == nil || !r.Item.User.Queued {
		t.Fatalf("queue %s: %+v %v", text, r, err)
	}
	return r.Item.ID
}

// A cancel that arrives while its prompt is being typed is told the prompt
// was sent; one that arrives first keeps it from being typed.
func TestCancelDuringDrainIsNotApplied(t *testing.T) {
	const id = "s_queuecan01"
	var status atomic.Value
	status.Store("working")
	pane := &blockPane{seqPane: seqPane{mode: "a"}, started: make(chan string, 4), release: make(chan struct{})}
	e := statusEngine(t, id, pane, &status)
	first := queue(t, e, id, "first")
	second := queue(t, e, id, "second")
	status.Store("idle")
	if got := <-pane.started; got != "first" {
		t.Fatalf("typing %q", got)
	}
	res, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: harness.ActionStop, Target: first})
	if err != nil || res.Result != harness.ResultFailed {
		t.Fatalf("cancel while typing = %+v %v", res, err)
	}
	if res, _ := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: harness.ActionStop, Target: second}); res.Result != harness.ResultApplied {
		t.Fatalf("cancel of a waiting prompt = %+v", res)
	}
	close(pane.release)
	time.Sleep(300 * time.Millisecond)
	if _, typed := pane.snapshot(); len(typed) != 1 || typed[0] != "first" {
		t.Fatalf("typed %v", typed)
	}
}

// The queue drains when only the hub status says the turn ended (no
// mapper record flips working), and a prompt sent while others wait goes
// behind them.
func TestQueueDrainsOnHubIdleAndKeepsOrder(t *testing.T) {
	const id = "s_queuehub01"
	var status atomic.Value
	status.Store("working")
	pane := &seqPane{mode: "a"}
	e := statusEngine(t, id, pane, &status)
	queue(t, e, id, "first")
	status.Store("idle")
	waitFor(t, func() bool { _, typed := pane.snapshot(); return len(typed) == 1 })
	status.Store("working")
	queue(t, e, id, "second")
	status.Store("idle")
	waitFor(t, func() bool { _, typed := pane.snapshot(); return len(typed) == 2 })
	if _, typed := pane.snapshot(); typed[0] != "first" || typed[1] != "second" {
		t.Fatalf("typed %v", typed)
	}
}

// An interrupt is sent at once even while another write holds the typing
// lock.
func TestInterruptSkipsTheTypingLock(t *testing.T) {
	const id = "s_queueint01"
	var status atomic.Value
	status.Store("idle")
	pane := &seqPane{mode: "a"}
	e := statusEngine(t, id, pane, &status)
	s, _ := e.served(context.Background(), id)
	l := typingLock(id)
	l.Lock()
	defer l.Unlock()
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan error, 1)
	go func() { defer wg.Done(); done <- e.sendKeys(context.Background(), s, false, "Escape") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("interrupt waited for the typing lock")
	}
	wg.Wait()
}
