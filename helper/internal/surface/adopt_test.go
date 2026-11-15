package surface

import (
	"encoding/json"
	"os"
	"testing"
)

// Recorded from omp 18.20.2: its list of conversations was opened natively,
// the phone let go with the list still open, the list was closed at the
// desk, and the native screen was opened again. On adopting its view the
// program adds "dock" and "layer" again, empty, and fills the dock. The
// helper refused every add of a name it already had, so the phone was shown
// the list that had been closed, and what was typed for it went nowhere.
func TestAnAdoptedViewTakesWhatTheProgramDrawsAgain(t *testing.T) {
	raw, err := os.ReadFile("testdata/omp-adopt-frames.json")
	if err != nil {
		t.Fatal(err)
	}
	var journal []struct {
		Verb   byte            `json:"verb"`
		Params json.RawMessage `json:"params"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	scene := testScene()
	scene.Options.Kinds = []string{"col", "row", "card", "section", "rule", "spacer", "text", "md", "code", "diff", "badge", "kbd",
		"icon", "spinner", "progress", "list", "item", "tabs", "editor", "input", "button", "status", "seg",
		"overlay", "toast", "picker", "prefs", "image", "meter", "kv", "table"}
	kinds := func() map[string]int {
		found := map[string]int{}
		var walk func(*Node)
		walk = func(n *Node) {
			found[n.Kind]++
			for _, child := range n.Children {
				walk(child)
			}
		}
		for _, v := range scene.Views {
			if !v.Closed {
				walk(v.Document.Root)
			}
		}
		return found
	}
	rejected := 0
	for i, entry := range journal {
		var params map[string]string
		_ = json.Unmarshal(entry.Params, &params)
		replies, err := scene.Handle(Message{Verb: entry.Verb, Params: params, Body: entry.Body})
		if err != nil {
			t.Fatalf("entry %d (%c): %v", i, entry.Verb, err)
		}
		for _, reply := range replies {
			var head struct {
				Event string `json:"ev"`
			}
			if json.Unmarshal(reply.Body, &head) == nil && head.Event == "error" {
				rejected++
			}
		}
		if i == 3 && kinds()["picker"] != 1 {
			t.Fatalf("the list is not in the scene after it opened: %v", kinds())
		}
	}
	if rejected != 0 {
		t.Errorf("%d operations of the recorded session were refused", rejected)
	}
	found := kinds()
	if found["picker"] != 0 {
		t.Errorf("the closed list is still in the scene: %v", found)
	}
	view := scene.View("s:1")
	if view == nil || view.Closed || view.Adopting || view.Document.Focus != "m.line/input" {
		t.Fatalf("view after adopt: %+v", view)
	}
	var top []string
	for _, child := range view.Document.Root.Children {
		top = append(top, child.ID)
	}
	if len(top) != 3 || top[0] != "main" || top[1] != "dock" || top[2] != "layer" {
		t.Errorf("the view's parts are %v, want main, dock, layer", top)
	}
	if n := len(view.Document.nodes["main"].node.Children); n == 0 {
		t.Error("the conversation the view kept is gone")
	}
	if n := len(view.Document.nodes["layer"].node.Children); n != 0 {
		t.Errorf("the layer holds %d nodes, want none", n)
	}
}

// Outside an adopt, an add of a name the document has is still refused,
// and an adopt replaces only under the same parent.
func TestAddReplacesOnlyInTheFrameAfterAnAdopt(t *testing.T) {
	doc, err := NewDocument("one", testScene().Options.Kinds)
	if err != nil {
		t.Fatal(err)
	}
	frame := func(seq int, ops string) []OperationError {
		bad, err := doc.Apply([]byte(`{"sf":"one","s":` + string(rune('0'+seq)) + `,"ops":[` + ops + `]}`))
		if err != nil {
			t.Fatal(err)
		}
		return bad
	}
	if bad := frame(1, `["add","a","one",null,{"id":"a","k":"col","c":[{"id":"a1","k":"text","p":{"text":"old"}}]}],["add","b","one",null,{"id":"b","k":"col"}]`); len(bad) != 0 {
		t.Fatal(bad)
	}
	if bad := frame(2, `["add","a","one",null,{"id":"a","k":"col"}]`); len(bad) != 1 || doc.nodes["a1"] == nil {
		t.Fatalf("a second add outside an adopt: %v", bad)
	}
	doc.replace = true
	if bad := frame(3, `["add","a","b",null,{"id":"a","k":"col"}]`); len(bad) != 1 || doc.nodes["a1"] == nil || doc.nodes["a"].parent != "one" {
		t.Fatalf("an add under another parent replaced the node: %v", bad)
	}
	// A replacement that cannot be taken leaves what was there.
	if bad := frame(4, `["add","a","one",null,{"id":"a","k":"col","c":[{"id":"b","k":"col"}]}]`); len(bad) != 1 || doc.nodes["a1"] == nil {
		t.Fatalf("a refused replacement lost the old node: %v", bad)
	}
	before := doc.bytes
	if bad := frame(5, `["add","a","one",null,{"id":"a","k":"col","c":[{"id":"a2","k":"text","p":{"text":"new"}}]}]`); len(bad) != 0 {
		t.Fatal(bad)
	}
	if doc.nodes["a1"] != nil || doc.nodes["a2"] == nil || doc.bytes != before {
		t.Fatalf("after replacing: a1 %v a2 %v bytes %d -> %d", doc.nodes["a1"] != nil, doc.nodes["a2"] != nil, before, doc.bytes)
	}
	if ids := []string{doc.Root.Children[0].ID, doc.Root.Children[1].ID}; ids[0] != "b" || ids[1] != "a" {
		t.Errorf("order after replacing: %v", ids)
	}
}
