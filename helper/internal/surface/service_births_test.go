package surface

import (
	"context"
	"encoding/json"
	"errors"
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

// A node that lists its actions takes only one of them, and only while it
// is not disabled. A node whose actions are a map of gestures, and the
// companion's own events, are not judged by that list.
func TestOnlyADeclaredActionIsPassedOn(t *testing.T) {
	s, f, owner, lease := nativeFixture(t)
	f.data <- apc(`f;{"sf":"one","s":2,"ops":[["add","p","one",null,{"id":"p","k":"col","p":{"actions":[{"id":"go","label":"Go"},{"id":"off","label":"Off","disabled":"Select a row"},{"id":"close","end":true}]}}],` +
		`["add","b","one",null,{"id":"b","k":"button","p":{"actions":{"click":"accept"}}}]]}`)
	waitNode(t, s.runs[lease.ID], "b")
	act := func(node, id string) error {
		event, _ := json.Marshal(map[string]any{"ev": "action", "sf": "one", "id": node, "act": id})
		_, err := s.Input(context.Background(), owner, InputParams{ID: lease.ID, Lease: lease.Token, Surface: "one", Instance: 1, Event: event})
		return err
	}
	for id, want := range map[string]error{"go": nil, "close": nil, "off": errLease, "made-up": errLease, "": errLease} {
		if err := act("p", id); !errors.Is(err, want) || (want == nil && err != nil) {
			t.Errorf("action %q on a node listing its actions: %v, want %v", id, err, want)
		}
	}
	for _, id := range []string{"accept", "anything"} {
		if err := act("b", id); err != nil {
			t.Errorf("action %q on a node with a gesture map was judged by a list: %v", id, err)
		}
	}
}
