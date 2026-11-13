package engine

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

type blockedReplayMapper struct {
	harness.Mapper
	entered chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func (m *blockedReplayMapper) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	m.once.Do(func() { close(m.entered); <-m.proceed })
	return m.Mapper.Map(rec)
}

type loadingReplaySub struct {
	*fakeSub
	reset chan struct{}
	once  sync.Once
}

func (s *loadingReplaySub) Notify(n harness.Notification) error {
	if n.Kind == harness.NoteReset && n.Reason == "history" {
		s.once.Do(func() { close(s.reset) })
	}
	return s.fakeSub.Notify(n)
}

func TestHistoryResetPrecedesReplayWork(t *testing.T) {
	const id = "s_loading01"
	e, s := replaySession(t, id, "omp.session")
	sub := &loadingReplaySub{fakeSub: &fakeSub{done: make(chan struct{})}, reset: make(chan struct{})}
	defer close(sub.done)
	if _, err := e.Open(context.Background(), sub, id, 0, 200); err != nil {
		t.Fatal(err)
	}
	e.Release(sub, id)
	fixture, err := os.ReadFile("../adapters/omp/testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	mapper := &blockedReplayMapper{Mapper: s.mappers["omp.session"], entered: make(chan struct{}), proceed: make(chan struct{})}
	s.mappers["omp.session"] = mapper
	s.mu.Unlock()
	var release sync.Once
	defer release.Do(func() { close(mapper.proceed) })
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.record(context.Background(), "omp.session", harness.Record{Source: "session", At: time.Now(), Replay: source.Records(fixture, 1<<30)})
	}()
	select {
	case <-mapper.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("replay did not start")
	}
	announced := false
	select {
	case <-sub.reset:
		announced = true
	case <-time.After(time.Second):
	}
	release.Do(func() { close(mapper.proceed) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("replay did not complete")
	}
	if !announced {
		t.Fatal("history loading was not announced until after replay finished")
	}
	opened, err := e.Open(context.Background(), sub, id, 0, 200)
	if err != nil || len(opened.Items) == 0 {
		t.Fatalf("completed snapshot: %v, items=%d", err, len(opened.Items))
	}
}
