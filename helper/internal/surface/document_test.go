package surface

import (
	"encoding/json"
	"strings"
	"testing"
)

func testDocument(t *testing.T) *Document {
	t.Helper()
	d, err := NewDocument("s:1", []string{"col", "row", "text", "input", "picker"})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := d.Apply([]byte(`{"sf":"s:1","s":1,"ops":[["add","main","s:1",null,{"id":"main","k":"col","c":[{"id":"a","k":"text","p":{"text":"A😀B","tone":"muted"}},{"id":"b","k":"input","p":{"text":"draft"}}]}],["focus","b"]]}`))
	if err != nil || len(bad) != 0 {
		t.Fatalf("initial frame: %v %v", bad, err)
	}
	return d
}

func TestDocumentRejectsOnlyInvalidOperations(t *testing.T) {
	d := testDocument(t)
	bad, err := d.Apply([]byte(`{"sf":"s:1","s":2,"ops":[["text","a","append","; x=1"],["add","bad","main",null,{"id":"bad","k":"script"}],["set","b",{"text":"still editable","tone":null}]]}`))
	if err != nil || len(bad) != 1 || bad[0].Index != 1 {
		t.Fatalf("frame: %v %v", bad, err)
	}
	if d.nodes["a"].node.Props["text"] != "A😀B; x=1" || d.nodes["b"].node.Props["text"] != "still editable" || d.Focus != "b" {
		t.Fatal("a rejected operation lost surrounding updates or focus")
	}
	if _, ok := d.nodes["bad"]; ok {
		t.Fatal("unsupported node was retained")
	}
}

func TestDocumentSpliceUsesUTF16WithoutSplittingSurrogates(t *testing.T) {
	for _, tc := range []struct {
		at, del int
		want    string
		bad     bool
	}{
		{1, 2, "AXB", false},
		{2, 0, "A😀B", true},
		{1, 1, "A😀B", true},
		{4, 0, "A😀BX", false},
		{5, 0, "A😀B", true},
	} {
		d := testDocument(t)
		raw, _ := json.Marshal(map[string]any{"sf": "s:1", "s": 2, "ops": []any{[]any{"splice", "a", tc.at, tc.del, "X"}}})
		bad, err := d.Apply(raw)
		if err != nil || (len(bad) != 0) != tc.bad || d.nodes["a"].node.Props["text"] != tc.want {
			t.Fatalf("splice %d/%d: text=%v errors=%v %v", tc.at, tc.del, d.nodes["a"].node.Props["text"], bad, err)
		}
	}
}

func TestDocumentDuplicateSubtreeIsAtomic(t *testing.T) {
	d := testDocument(t)
	bad, err := d.Apply([]byte(`{"sf":"s:1","s":2,"ops":[["add","new","main",null,{"id":"new","k":"col","c":[{"id":"a","k":"text"}]}]]}`))
	if err != nil || len(bad) != 1 || d.nodes["new"] != nil || len(d.nodes["main"].node.Children) != 2 {
		t.Fatalf("partial duplicate subtree retained: %v %v", bad, err)
	}
}

func TestDocumentMoveCannotCreateCyclesAndDeleteClearsFocus(t *testing.T) {
	d := testDocument(t)
	bad, err := d.Apply([]byte(`{"sf":"s:1","s":2,"ops":[["move","main","a",null],["move","b","main","a"],["del","b"]]}`))
	if err != nil || len(bad) != 1 || bad[0].Index != 0 || d.Focus != "" {
		t.Fatalf("move/delete: %v %v focus=%q", bad, err, d.Focus)
	}
	if cs := d.nodes["main"].node.Children; len(cs) != 1 || cs[0].ID != "a" {
		t.Fatalf("wrong remaining children: %v", cs)
	}
}

func TestDocumentSizeFailureKeepsLastGoodNode(t *testing.T) {
	d := testDocument(t)
	d.maxBytes = d.bytes + 32
	raw, _ := json.Marshal(map[string]any{"sf": "s:1", "s": 2, "ops": []any{[]any{"set", "b", map[string]any{"text": strings.Repeat("x", 1024)}}}})
	bad, err := d.Apply(raw)
	if err != nil || len(bad) != 1 || d.nodes["b"].node.Props["text"] != "draft" {
		t.Fatalf("size failure changed the good node: %v %v", bad, err)
	}
}

func TestDocumentRejectsWrongSurfaceAndMalformedFrame(t *testing.T) {
	d := testDocument(t)
	for _, raw := range []string{
		`{"sf":"other","s":2,"ops":[]}`,
		`{"sf":"s:1","ops":[]}`,
		`{"sf":"s:1","s":2,"ops":{}}`,
		`{"sf":"s:1","s":2,"ops":null}`,
	} {
		if _, err := d.Apply([]byte(raw)); err == nil || d.Sequence != 1 {
			t.Fatalf("accepted malformed frame %s", raw)
		}
	}
}

func TestDocumentPropsMergeRemovesNullWithoutLosingOtherFields(t *testing.T) {
	d := testDocument(t)
	bad, err := d.Apply([]byte(`{"sf":"s:1","s":2,"ops":[["set","a",{"tone":null,"wrap":true}]]}`))
	if err != nil || len(bad) != 0 {
		t.Fatalf("set: %v %v", bad, err)
	}
	p := d.nodes["a"].node.Props
	if _, ok := p["tone"]; ok || p["wrap"] != true || p["text"] != "A😀B" {
		t.Fatalf("wrong merged props: %v", p)
	}
}

func TestDocumentNullTextAndSpliceOffsetsAreRejected(t *testing.T) {
	d := testDocument(t)
	bad, err := d.Apply([]byte(`{"sf":"s:1","s":2,"ops":[["text","a","replace",null],["splice","a",null,0,"X"],["splice","a",0,null,"X"]]}`))
	if err != nil || len(bad) != 3 || d.nodes["a"].node.Props["text"] != "A😀B" {
		t.Fatalf("null arguments mutated the document: %v %v", bad, err)
	}
}

func TestDocumentDeepSubtreeMoveDoesNotExceedDepthLimit(t *testing.T) {
	d := testDocument(t)
	node := &Node{ID: "bottom", Kind: "col"}
	for i := 0; i < MaxDepth-2; i++ {
		node = &Node{ID: strings.Repeat("x", i+1), Kind: "col", Children: []*Node{node}}
	}
	raw, _ := json.Marshal(map[string]any{"sf": "s:1", "s": 2, "ops": []any{[]any{"add", node.ID, "s:1", nil, node}}})
	bad, err := d.Apply(raw)
	if err != nil || len(bad) != 0 {
		t.Fatalf("valid deep subtree rejected: %v %v", bad, err)
	}
	raw, _ = json.Marshal(map[string]any{"sf": "s:1", "s": 3, "ops": []any{[]any{"move", "main", "bottom", nil}}})
	bad, err = d.Apply(raw)
	if err != nil || len(bad) != 1 || d.nodes["main"].parent != "s:1" {
		t.Fatalf("over-depth move changed hierarchy: %v %v", bad, err)
	}
}
