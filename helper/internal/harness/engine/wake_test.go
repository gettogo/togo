package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func init() {
	harness.RegisterAdapterFile("fakewake", []byte(`
adapter: fakewake
name: Woken
revision: 1
match: { agents: [fakewake] }
capabilities: [chat]
feeds:
  events:
    hook: { events: [Event] }
    mapper: fake.wake
    stability: documented
`))
	harness.RegisterMapper("fake.wake", func(harness.Env) harness.Mapper { return &wakeMapper{} })
}

type wakeMapper struct {
	mu      sync.Mutex
	working bool
	wake    func()
}

func (w *wakeMapper) Name() string                                      { return "fake.wake" }
func (w *wakeMapper) Map(harness.Record) ([]harness.Item, []harness.Op) { return nil, nil }
func (w *wakeMapper) Wake(f func())                                     { w.mu.Lock(); w.wake = f; w.mu.Unlock() }
func (w *wakeMapper) Woken() ([]harness.Item, []harness.Op) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.working {
		return nil, nil
	}
	return []harness.Item{{ID: "w1", TurnID: "w1", Kind: harness.KindUser, User: &harness.UserBody{Text: "learned off a record"}}}, nil
}

func (w *wakeMapper) State() map[string]string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.working {
		return map[string]string{"working": "true"}
	}
	return map[string]string{"working": "false"}
}

// A mapper that learned something away from the session's lock wakes the
// engine, which publishes what Woken returns and reads its state again
// without waiting for a record.
func TestWakeReadsTheMapperStateAgain(t *testing.T) {
	const id = "s_fakewake01"
	e := New(Config{Sessions: fakeSessions{id: {ID: id, Agent: "fakewake", Managed: true, Cwd: t.TempDir()}}, Home: t.TempDir(), Log: &Log{}})
	t.Cleanup(func() { e.Forget(id) })
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	s := e.sessions[id]
	e.mu.Unlock()
	s.waitReady(context.Background())
	s.mu.Lock()
	w, _ := s.mappers["fake.wake"].(*wakeMapper)
	s.mu.Unlock()
	if w == nil {
		t.Fatal("no mapper")
	}
	w.mu.Lock()
	w.working = true
	wake := w.wake
	w.mu.Unlock()
	if wake == nil {
		t.Fatal("the engine gave the mapper no wake")
	}
	wake()
	waitFor(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.working })
	waitFor(t, func() bool { _, ok := e.log.Get(id, "w1"); return ok })
}
