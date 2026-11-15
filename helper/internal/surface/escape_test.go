package surface

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"
)

// Recorded from omp 18.20.2: its debug tools (a card holding a plain
// selector) say no way to close them; its text prompt has a Cancel button;
// its choose-one prompt has an action marked as its end. Only the first is
// a screen for Escape, and the program is told that screen's own nodes.
func TestEscapeIsOnlyForAScreenThatSaysNoWayToClose(t *testing.T) {
	raw, err := os.ReadFile("testdata/omp-overlays.json")
	if err != nil {
		t.Fatal(err)
	}
	var nodes map[string]*Node
	if err := json.Unmarshal(raw, &nodes); err != nil {
		t.Fatal(err)
	}
	within, err := escapeTarget(nodes["select_overlay"])
	if err != nil {
		t.Fatal(err)
	}
	overlay := nodes["select_overlay"]
	if len(within) < 10 || within[0] != overlay.ID || !slices.Contains(within, overlay.Children[1].Children[0].ID) {
		t.Fatalf("nodes named for the debug tools: %v", within)
	}
	for name, n := range map[string]*Node{
		"a prompt with a Cancel button": nodes["hook_input"],
		"a list with its own end":       nodes["hook_select"],
		"a composer":                    {ID: "dock", Kind: "col", Children: []*Node{{ID: "e", Kind: "editor", Props: map[string]any{"sendable": true}}}},
		"a screen with a Close button":  {ID: "dlg", Kind: "col", Children: []*Node{{ID: "b", Kind: "button", Props: map[string]any{"actions": map[string]any{"click": "close"}}}}},
	} {
		if _, err := escapeTarget(n); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s was taken for a screen to escape: %v", name, err)
		}
	}
	wide := &Node{ID: "w", Kind: "col"}
	for i := 0; i < maxEscapeNodes; i++ {
		wide.Children = append(wide.Children, &Node{ID: "n" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Kind: "text"})
	}
	if _, err := escapeTarget(wide); !errors.Is(err, ErrLimit) {
		t.Errorf("a screen of %d nodes: %v", maxEscapeNodes+1, err)
	}
}

// The helper asks for an Escape only from a companion that says it can
// give one, and names the screen's nodes itself: what the app sends as
// `within` is not passed on.
func TestEscapeNeedsTheFeatureAndNamesTheScreenItself(t *testing.T) {
	s, f, owner, lease := nativeFixture(t)
	f.data <- apc(`f;{"sf":"one","s":2,"ops":[["add","dlg","one",null,{"id":"dlg","k":"col","c":[{"id":"dlg.t","k":"text","p":{"text":"x"}}]}]]}`)
	r := s.runs[lease.ID]
	for i := 0; ; i++ {
		found := false
		_ = r.cache.Inspect(func(scene *Scene) error {
			found = scene.View("one") != nil && scene.View("one").Document.nodes["dlg"] != nil
			return nil
		})
		if found {
			break
		}
		if i > 2000 {
			t.Fatal("the frame was not applied")
		}
		time.Sleep(time.Millisecond)
	}
	p := InputParams{ID: lease.ID, Lease: lease.Token, Surface: "one", Instance: 1,
		Event: json.RawMessage(`{"ev":"action","sf":"one","id":"dlg","act":"togo.escape","within":["input"]}`)}
	if _, err := s.Input(context.Background(), owner, p); !errors.Is(err, ErrMalformed) {
		t.Fatalf("an Escape was asked of a companion without the feature: %v", err)
	}
	r.lease.Features = []string{"escape"}
	before := len(f.sent)
	res, err := s.Input(context.Background(), owner, p)
	if err != nil || !res.Dispatched {
		t.Fatalf("escape: %+v, %v", res, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) != before+1 {
		t.Fatalf("%d messages sent", len(f.sent)-before)
	}
	var event struct {
		Act    string   `json:"act"`
		ID     string   `json:"id"`
		Within []string `json:"within"`
	}
	if err := json.Unmarshal(f.sent[len(f.sent)-1].Body, &event); err != nil {
		t.Fatal(err)
	}
	if event.Act != "togo.escape" || event.ID != "dlg" || !slices.Equal(event.Within, []string{"dlg", "dlg.t"}) {
		t.Fatalf("event sent to the program: %+v", event)
	}
	// The composer is never a screen to escape.
	p.Event = json.RawMessage(`{"ev":"action","sf":"one","id":"input","act":"togo.escape"}`)
	if _, err := s.Input(context.Background(), owner, p); !errors.Is(err, ErrMalformed) {
		t.Fatalf("the composer was taken for a screen to escape: %v", err)
	}
}

// A row is chosen, or opened, through its list. The helper passes it on
// only for a row that list has now and that can be chosen.
func TestARowIsChosenOnlyFromItsOwnListAsItIsNow(t *testing.T) {
	list := &Node{ID: "pick", Kind: "list", Children: []*Node{
		{ID: "pick/a", Kind: "item"},
		{ID: "pick/off", Kind: "item", Props: map[string]any{"disabled": true}},
		{ID: "pick/gone", Kind: "item", Props: map[string]any{"hidden": true}},
	}}
	if !hasRow(list, "pick/a") {
		t.Error("a row of the list was refused")
	}
	for name, item := range map[string]string{
		"another list's row": "other/x", "a row that is gone": "pick/zz", "a disabled row": "pick/off", "a hidden row": "pick/gone",
	} {
		if hasRow(list, item) {
			t.Errorf("%s was passed on", name)
		}
	}
	// Through the service: the same event forms, on a node the rule does
	// not judge, still reach the program; an item that is not a name does
	// not.
	s, f, owner, lease := nativeFixture(t)
	f.data <- apc(`f;{"sf":"one","s":2,"ops":[["add","pick","one",null,{"id":"pick","k":"col","c":[{"id":"pick/a","k":"text","p":{"text":"A"}}]}]]}`)
	r := s.runs[lease.ID]
	for i := 0; ; i++ {
		found := false
		_ = r.cache.Inspect(func(scene *Scene) error {
			found = scene.View("one") != nil && scene.View("one").Document.nodes["pick/a"] != nil
			return nil
		})
		if found {
			break
		}
		if i > 2000 {
			t.Fatal("the frame was not applied")
		}
		time.Sleep(time.Millisecond)
	}
	for _, kind := range []string{"select", "activate"} {
		event, _ := json.Marshal(map[string]any{"ev": kind, "sf": "one", "id": "pick", "item": "pick/a"})
		if _, err := s.Input(context.Background(), owner, InputParams{ID: lease.ID, Lease: lease.Token, Surface: "one", Instance: 1, Event: event}); err != nil {
			t.Errorf("%s was refused: %v", kind, err)
		}
	}
	event := json.RawMessage(`{"ev":"select","sf":"one","id":"pick","item":7}`)
	if _, err := s.Input(context.Background(), owner, InputParams{ID: lease.ID, Lease: lease.Token, Surface: "one", Instance: 1, Event: event}); !errors.Is(err, ErrMalformed) {
		t.Errorf("a row that is not a name: %v", err)
	}
}

// Found in review: a picker keeps its rows in its items, not as child
// nodes, so the rule for a list's rows refused every highlight and open
// in omp's own pickers. Recorded resume picker: a row its order shows is
// passed on; one filtered out by a search, or not among its items, is not.
func TestAPickersRowsAreItsItemsAsItsOrderShowsThem(t *testing.T) {
	raw, err := os.ReadFile("testdata/omp-resume-nomatch.json")
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Nodes map[string]*Node `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatal(err)
	}
	opened, filtered := recorded.Nodes["1 opened"], recorded.Nodes["query 'zzzz-no-match-qqq'"]
	if opened == nil || filtered == nil {
		t.Fatal("recorded picker states missing")
	}
	row := opened.Props["items"].([]any)[0].(map[string]any)["id"].(string)
	if !hasRow(opened, row) {
		t.Error("a row the picker shows was refused")
	}
	if hasRow(filtered, row) {
		t.Error("a row the search filtered out was passed on")
	}
	if hasRow(opened, "not-a-row") {
		t.Error("a row the picker does not have was passed on")
	}
	// A row disabled with a reason, as the program marks its sign-in
	// rows, or hidden, is not passed on; an empty reason disables nothing.
	marked := &Node{ID: "p", Kind: "picker", Props: map[string]any{"items": []any{
		map[string]any{"id": "ok"}, map[string]any{"id": "reason", "disabled": "Sign in with /login"},
		map[string]any{"id": "flag", "disabled": true}, map[string]any{"id": "hid", "hidden": true},
		map[string]any{"id": "blank", "disabled": ""}, map[string]any{"id": "no", "disabled": false},
	}}}
	for id, want := range map[string]bool{"ok": true, "reason": false, "flag": false, "hid": false, "blank": true, "no": true} {
		if hasRow(marked, id) != want {
			t.Errorf("picker row %q: passed on = %v, want %v", id, !want, want)
		}
	}
	rows := &Node{ID: "l", Kind: "list", Children: []*Node{{ID: "l/r", Kind: "item", Props: map[string]any{"disabled": "Not available"}}}}
	if hasRow(rows, "l/r") {
		t.Error("a list row disabled with a reason was passed on")
	}
	// Other kinds are not judged by this rule.
	if !hasRow(&Node{ID: "t", Kind: "tabs"}, "anything") {
		t.Error("a node that is neither a list nor a picker was judged as one")
	}
}
