package surface

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func waitNode(t *testing.T, r *renderer, id string) {
	t.Helper()
	for i := 0; ; i++ {
		found := false
		_ = r.cache.Inspect(func(scene *Scene) error {
			found = scene.View("one") != nil && scene.View("one").Document.nodes[id] != nil
			return nil
		})
		if found {
			return
		}
		if i > 2000 {
			t.Fatal("the frame was not applied")
		}
		time.Sleep(time.Millisecond)
	}
}

// The lease says the scene carries births, and an event passed on comes
// back with the view's sequence as it was before the event was written to
// the program: what a later frame adds came after it.
func TestAnEventComesBackWithTheSequenceItWasSentAt(t *testing.T) {
	s, f, owner, lease := nativeFixture(t)
	if !slices.Contains(lease.Features, "births") {
		t.Fatalf("lease features %v", lease.Features)
	}
	r := s.runs[lease.ID]
	event := json.RawMessage(`{"ev":"focus","sf":"one","id":"input"}`)
	in := InputParams{ID: lease.ID, Lease: lease.Token, Surface: "one", Instance: 1, Event: event}
	res, err := s.Input(context.Background(), owner, in)
	if err != nil || res.Sequence == nil || *res.Sequence != 1 {
		t.Fatalf("first event: %+v, %v", res, err)
	}
	f.data <- apc(`f;{"sf":"one","s":5,"ops":[["add","later","one",null,{"id":"later","k":"text","p":{"text":"x"}}]]}`)
	waitNode(t, r, "later")
	res, err = s.Input(context.Background(), owner, in)
	if err != nil || res.Sequence == nil || *res.Sequence != 5 {
		t.Fatalf("after frame 5: %+v, %v", res, err)
	}
	// Refused by the program, it still says when it was sent.
	f.refuse = true
	res, err = s.Input(context.Background(), owner, in)
	if err != nil || res.Refused == nil || res.Sequence == nil || *res.Sequence != 5 {
		t.Fatalf("refused event: %+v, %v", res, err)
	}
}

// The actions a node lists are what it offers on its screen, not all it
// takes: omp's session picker takes "cancel" while its rename field is
// open and lists no such action. So the helper passes on an action the
// list does not name; what to offer is the app's to check.
func TestAnActionTheListDoesNotNameIsStillPassedOn(t *testing.T) {
	s, f, owner, lease := nativeFixture(t)
	f.data <- apc(`f;{"sf":"one","s":2,"ops":[["add","p","one",null,{"id":"p","k":"col","p":{"actions":[{"id":"go","label":"Go"},{"id":"close","end":true}]}}]]}`)
	waitNode(t, s.runs[lease.ID], "p")
	for _, id := range []string{"go", "close", "cancel"} {
		event, _ := json.Marshal(map[string]any{"ev": "action", "sf": "one", "id": "p", "act": id})
		if _, err := s.Input(context.Background(), owner, InputParams{ID: lease.ID, Lease: lease.Token, Surface: "one", Instance: 1, Event: event}); err != nil {
			t.Errorf("action %q was refused: %v", id, err)
		}
	}
}
