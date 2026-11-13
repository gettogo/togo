package engine

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

func replaySession(t *testing.T, id, mapper string) (*Engine, *sess) {
	t.Helper()
	e, _ := newFake(t, id, "1.2.3")
	// Use the recorded mapper in the adapter's actual feed order, so
	// working and the final status are read from it as in production.
	a := *e.adapters["fake"]
	definition := *a.Adapter
	definition.Feeds = maps.Clone(definition.Feeds)
	for name, feed := range definition.Feeds {
		feed.Mapper = mapper
		definition.Feeds[name] = feed
	}
	a.Adapter = &definition
	e.adapters["fake"] = &a
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return e, s
}

func TestRestoredTranscriptPublishesOnlyItsFinalState(t *testing.T) {
	for _, adapter := range []struct{ mapper, fixture string }{
		{"omp.session", "../adapters/omp/testdata/session.jsonl"},
		{"pi.session", "../adapters/pisession/testdata/session.jsonl"},
	} {
		t.Run(adapter.mapper, func(t *testing.T) {
			const id = "s_replay001"
			e, s := replaySession(t, id, adapter.mapper)
			sub := &fakeSub{done: make(chan struct{})}
			defer close(sub.done)
			if _, err := e.Open(context.Background(), sub, id, 0, 200); err != nil {
				t.Fatal(err)
			}
			fixture, err := os.ReadFile(adapter.fixture)
			if err != nil {
				t.Fatal(err)
			}
			records := source.Records(fixture, 1<<30)
			began := time.Now()
			s.record(context.Background(), adapter.mapper, harness.Record{Source: "session", At: time.Now(), Replay: records})
			t.Logf("restored %d records in %s", len(records), time.Since(began))
			e.Release(sub, id)
			waitFor(t, func() bool { return len(sub.notes()) > 0 })
			notes := sub.notes()
			resets := 0
			for _, note := range notes {
				if note.Kind == harness.NoteReset && note.Reason == "history" {
					resets++
				}
				if note.Kind == harness.NoteItems {
					t.Fatalf("history escaped as live items: %+v", note)
				}
				if note.Mission != nil {
					for _, op := range note.Mission.Ops {
						if op.Summary != nil && op.Summary.Working {
							t.Fatal("historical working state escaped")
						}
					}
				}
			}
			if resets != 1 {
				t.Fatalf("got %d restoration boundaries", resets)
			}
			opened, err := e.Open(context.Background(), sub, id, 0, 200)
			if err != nil || len(opened.Items) == 0 || opened.Mission.Summary.Working {
				t.Fatalf("restored snapshot: %+v, %v", opened, err)
			}
			// Subsequent live output still uses the ordinary notification path.
			factory, _ := harness.LookupMapper("fake.events")
			s.mu.Lock()
			s.mappers["fake.events"] = factory(s.env)
			s.mu.Unlock()
			s.record(context.Background(), "fake.events", harness.Record{Source: "events", Data: json.RawMessage(`{"items":[{"id":"live","kind":"notice","notice":{"text":"new output"}}]}`)})
			e.Release(sub, id)
			waitFor(t, func() bool {
				for _, note := range sub.notes() {
					if note.Kind == harness.NoteItems && len(note.Items) > 0 && note.Items[0].ID == "live" {
						return true
					}
				}
				return false
			})
		})
	}
}

func TestRestoredUnfinishedTurnRemainsWorking(t *testing.T) {
	const id = "s_replay001"
	e, s := replaySession(t, id, "omp.session")
	fixture, err := os.ReadFile("../adapters/omp/testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	// The recording has a completed first turn and then a new user
	// message. Restore before that second turn's final assistant record.
	records := source.Records(fixture, 1<<30)
	s.record(context.Background(), "omp.session", harness.Record{Source: "session", At: time.Now(), Replay: records[:18]})
	sub := &fakeSub{done: make(chan struct{})}
	defer close(sub.done)
	opened, err := e.Open(context.Background(), sub, id, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !opened.Mission.Summary.Working {
		t.Fatal("restoration hid a genuinely unfinished turn")
	}
}
