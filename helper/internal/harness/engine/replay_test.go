package engine

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

func TestRestoredTranscriptPublishesOnlyItsFinalState(t *testing.T) {
	for _, adapter := range []struct{ mapper, fixture string }{
		{"omp.session", "../adapters/omp/testdata/session.jsonl"},
		{"pi.session", "../adapters/pisession/testdata/session.jsonl"},
	} {
		t.Run(adapter.mapper, func(t *testing.T) {
			const id = "s_replay001"
			e, _ := newFake(t, id, "1.2.3")
			s, err := e.served(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			factory, ok := harness.LookupMapper(adapter.mapper)
			if !ok {
				t.Fatal("mapper not registered")
			}
			s.mu.Lock()
			s.mappers[adapter.mapper] = factory(s.env)
			s.mu.Unlock()
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
