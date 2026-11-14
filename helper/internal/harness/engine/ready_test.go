package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakeready", []byte(`
adapter: fakeready
name: Ready
revision: 1
match: { agents: [fakeready] }
capabilities: [chat, send.queue]
ready: '^┃  \S+ · \S'
`))
}

// readyPane shows its composer only after it has "started".
type readyPane struct {
	mu      sync.Mutex
	started bool
	typed   []string
	// early counts text typed before the composer showed.
	early int
}

func (p *readyPane) Type(_ context.Context, _ string, text string, _, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started {
		p.early++
	}
	p.typed = append(p.typed, text)
	return nil
}
func (p *readyPane) Keys(context.Context, string, ...string) error { return nil }
func (p *readyPane) Screen(context.Context, string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started {
		return "  starting…\n", nil
	}
	return "┃\n┃  Build · Big Pickle OpenCode\n", nil
}

// Seen live on OpenCode 2.2.5: a model picked before the first prompt
// restarted the terminal, the prompt was typed while it started, and
// nothing arrived. A prompt is typed once the composer shows.
func TestPromptWaitsForTheComposer(t *testing.T) {
	const id = "s_fakereadyy"
	home := t.TempDir()
	pane := &readyPane{}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakeready", Managed: true, Cwd: home, Command: []string{"fakeready"}}},
		Typist:   pane, Screen: pane.Screen, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.2,
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		pane.mu.Lock()
		pane.started = true
		pane.mu.Unlock()
	}()
	if _, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "first prompt", Mode: harness.SendQueue}); err != nil {
		t.Fatal(err)
	}
	pane.mu.Lock()
	defer pane.mu.Unlock()
	if pane.early != 0 || len(pane.typed) == 0 {
		t.Errorf("typed %q, %d of it before the composer showed", pane.typed, pane.early)
	}
}
