package surface

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var birthKinds = RenderOptions{Cols: 80, Dark: true, Kinds: []string{"col", "row", "text", "toast", "input", "picker", "icon", "button"}}

// birthCache is a cache with one inline view "s:1" holding main and layer.
func birthCache(t *testing.T) (*Cache, string) {
	t.Helper()
	root := t.TempDir()
	c, err := OpenCache(root, CacheIdentity{Generation: "run", Birth: "process", Conversation: "chat"}, birthKinds)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	cacheApply(t, c, 'q', `{"q":"hello","v":[1],"app":"example","features":["send"]}`)
	cacheApply(t, c, 'o', `{"id":"s:1","mode":"inline"}`)
	cacheApply(t, c, 'f', `{"sf":"s:1","s":1,"ops":[["add","main","s:1",null,{"id":"main","k":"col","c":[{"id":"m0","k":"text","p":{"text":"hello"}}]}],["add","layer","s:1",null,{"id":"layer","k":"col"}]]}`)
	return c, root
}

type servedView struct {
	ID       string `json:"id"`
	Document struct {
		Root     *Node `json:"root"`
		Sequence int64 `json:"sequence"`
	} `json:"document"`
	Recent      []RecentResult `json:"recent"`
	RecentFloor *int64         `json:"recentFloor"`
}

func served(t *testing.T, c *Cache) map[string]servedView {
	t.Helper()
	raw, _, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var scene struct {
		Views []servedView `json:"views"`
	}
	if err := json.Unmarshal(raw, &scene); err != nil {
		t.Fatal(err)
	}
	out := map[string]servedView{}
	for _, v := range scene.Views {
		out[v.ID] = v
	}
	return out
}

func born(n *Node, id string) int64 {
	if n.ID == id {
		return n.Born
	}
	for _, child := range n.Children {
		if b := born(child, id); b != -1 {
			return b
		}
	}
	return -1
}

func frame(t *testing.T, c *Cache, seq int, ops string) {
	t.Helper()
	cacheApply(t, c, 'f', fmt.Sprintf(`{"sf":"s:1","s":%d,"ops":[%s]}`, seq, ops))
}

// Every node says which frame added it: the node and all that arrived with
// it, once, whatever comes after. What the program says of a birth is not
// taken, and none of it is written to disk.
func TestANodeSaysWhichFrameAddedIt(t *testing.T) {
	c, root := birthCache(t)
	frame(t, c, 2, `["add","t1","main",null,{"id":"t1","k":"toast","b":99,"p":{"text":"Saved","ttl":2400},"c":[{"id":"t1/i","k":"icon","b":7}]}]`)
	frame(t, c, 3, `["set","t1",{"text":"Saved again"}],["add","m1","main",null,{"id":"m1","k":"text","p":{"text":"later"}}]`)
	frame(t, c, 4, `["settle","m1"]`)
	v := served(t, c)["s:1"]
	for id, want := range map[string]int64{"main": 1, "m0": 1, "layer": 1, "t1": 2, "t1/i": 2, "m1": 3} {
		if got := born(v.Document.Root, id); got != want {
			t.Errorf("node %s born in frame %d, want %d", id, got, want)
		}
	}
	if v.Document.Sequence != 4 || len(v.Recent) != 0 || v.RecentFloor != nil {
		t.Errorf("view: sequence %d, recent %v, floor %v", v.Document.Sequence, v.Recent, v.RecentFloor)
	}
	// Served twice, the same; and the scene held is not left stamped.
	if again := served(t, c)["s:1"]; born(again.Document.Root, "t1") != 2 {
		t.Error("a second read lost the stamp")
	}
	c.mu.Lock()
	leftover := c.scene.Views[0].Document.nodes["t1"].node.Born
	c.mu.Unlock()
	if leftover != 0 {
		t.Errorf("the held scene keeps a stamp of %d", leftover)
	}
	// Nothing of it on disk: neither the journal nor a checkpoint.
	if err := c.checkpoint(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"snapshot.json", "frames.jsonl"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"b":2`) || strings.Contains(string(raw), `"recent"`) {
			t.Errorf("%s holds birth or recent data", name)
		}
	}
}

// A result removed from the transcript is kept for the phone: a toast, or a
// node with a notice role. An id used again is a new node with a new birth.
func TestAResultRemovedFromTheTranscriptIsKept(t *testing.T) {
	c, _ := birthCache(t)
	frame(t, c, 2, `["add","5","main",null,{"id":"5","k":"toast","p":{"role":"omp.toast.status","text":"No stored credentials.","ttl":2400}}]`)
	// The same id again in one frame, as omp does: the old one goes, a
	// new one with the same words comes.
	frame(t, c, 3, `["del","5"],["add","5","main",null,{"id":"5","k":"toast","p":{"role":"omp.toast.status","text":"No stored credentials.","ttl":2400}}]`)
	v := served(t, c)["s:1"]
	if born(v.Document.Root, "5") != 3 {
		t.Errorf("the second toast was born in frame %d, want 3", born(v.Document.Root, "5"))
	}
	if len(v.Recent) != 1 || v.Recent[0] != (RecentResult{ID: "5", Born: 2, Gone: 3, Kind: "toast", Role: "omp.toast.status", Text: "No stored credentials."}) {
		t.Fatalf("recent = %+v", v.Recent)
	}
	// A notice row with an icon and spans; then things that are not kept:
	// ordinary text, a toast inside a dialog, a result holding a field.
	frame(t, c, 4, `["add","n","main",null,{"id":"n","k":"row","p":{"role":"omp.notice.warning","tone":"warning"},"c":[{"id":"n.0","k":"icon","p":{"name":"warning"}},{"id":"n.1","k":"text","p":{"spans":[{"t":"Plan mode "},{"t":"is not active."}]}}]}],`+
		`["add","d","layer",null,{"id":"d","k":"col","c":[{"id":"d/t","k":"toast","p":{"text":"in a dialog"}}]}],`+
		`["add","f","main",null,{"id":"f","k":"toast","p":{"text":"with a field"},"c":[{"id":"f/i","k":"input","p":{"text":"typed"}}]}]`)
	frame(t, c, 5, `["del","n"],["del","m0"],["del","d/t"],["del","f"]`)
	v = served(t, c)["s:1"]
	if len(v.Recent) != 2 || v.Recent[1] != (RecentResult{ID: "n", Born: 4, Gone: 5, Kind: "row", Role: "omp.notice.warning", Tone: "warning", Text: "Plan mode is not active."}) {
		t.Fatalf("recent = %+v", v.Recent)
	}
}

// What is kept is bounded, and what was dropped is said: the newest birth
// among results no longer listed.
func TestKeptResultsAreBoundedAndTheirLossIsSaid(t *testing.T) {
	c, _ := birthCache(t)
	seq := 1
	for i := 0; i < maxRecentResults+3; i++ {
		seq++
		frame(t, c, seq, fmt.Sprintf(`["add","t%d","main",null,{"id":"t%d","k":"toast","p":{"text":"result %d"}}]`, i, i, i))
		seq++
		frame(t, c, seq, fmt.Sprintf(`["del","t%d"]`, i))
	}
	v := served(t, c)["s:1"]
	if len(v.Recent) != maxRecentResults || v.Recent[0].ID != "t3" || v.RecentFloor == nil || *v.RecentFloor != 6 {
		t.Fatalf("recent holds %d from %s, floor %v", len(v.Recent), v.Recent[0].ID, v.RecentFloor)
	}
	// A long text is cut at a character, and said to be cut.
	seq++
	frame(t, c, seq, `["add","big","main",null,{"id":"big","k":"toast","p":{"text":"`+strings.Repeat("é", 1500)+`"}}]`)
	seq++
	frame(t, c, seq, `["del","big"]`)
	v = served(t, c)["s:1"]
	last := v.Recent[len(v.Recent)-1]
	if !last.Cut || len(last.Text) > maxResultText || !strings.HasPrefix(last.Text, "éé") || strings.ContainsRune(last.Text, '�') {
		t.Errorf("cut text: cut %v, %d bytes", last.Cut, len(last.Text))
	}
	// Old results go after enough frames, and that too raises the floor.
	for i := 0; i < recentFrames+2; i++ {
		seq++
		frame(t, c, seq, `["settle","main"]`)
	}
	v = served(t, c)["s:1"]
	if len(v.Recent) != 0 || v.RecentFloor == nil || *v.RecentFloor < int64(seq-recentFrames-4) {
		t.Errorf("after %d frames: %d kept, floor %v", recentFrames+2, len(v.Recent), v.RecentFloor)
	}
}

// Two views keep their own births and results, and a renderer that opens
// the cache again starts with none kept.
func TestBirthsAndResultsBelongToTheirViewAndRenderer(t *testing.T) {
	c, root := birthCache(t)
	cacheApply(t, c, 'o', `{"id":"s:2","mode":"screen"}`)
	cacheApply(t, c, 'f', `{"sf":"s:2","s":7,"ops":[["add","main","s:2",null,{"id":"main","k":"col","c":[{"id":"x","k":"toast","p":{"text":"other view"}}]}]]}`)
	cacheApply(t, c, 'f', `{"sf":"s:2","s":8,"ops":[["del","x"]]}`)
	frame(t, c, 2, `["add","t","main",null,{"id":"t","k":"toast","p":{"text":"here"}}]`)
	frame(t, c, 3, `["del","t"]`)
	views := served(t, c)
	if got := views["s:1"].Recent; len(got) != 1 || got[0].ID != "t" || got[0].Born != 2 {
		t.Errorf("inline view recent = %+v", got)
	}
	// A screen view is not a transcript: nothing of it is kept.
	if got := views["s:2"].Recent; len(got) != 0 {
		t.Errorf("screen view recent = %+v", got)
	}
	if born(views["s:2"].Document.Root, "main") != 7 {
		t.Errorf("the other view's node was born in %d", born(views["s:2"].Document.Root, "main"))
	}
	c.Close()
	again, err := OpenCache(root, CacheIdentity{Generation: "run", Birth: "process", Conversation: "chat"}, birthKinds)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	cacheApply(t, again, 'q', `{"q":"hello","v":[1],"app":"example","features":["send"]}`)
	cacheApply(t, again, 'o', `{"id":"s:1","mode":"inline","adopt":true}`)
	cacheApply(t, again, 'f', `{"sf":"s:1","s":4,"ops":[["add","layer","s:1",null,{"id":"layer","k":"col"}]]}`)
	v := served(t, again)["s:1"]
	if len(v.Recent) != 0 || v.RecentFloor != nil {
		t.Errorf("a new renderer starts with recent %+v, floor %v", v.Recent, v.RecentFloor)
	}
	if born(v.Document.Root, "layer") != 4 {
		t.Errorf("a part drawn again on adopt was born in %d, want 4", born(v.Document.Root, "layer"))
	}
}

// Found in review. A replacement on adopt that is refused leaves the old
// node in place: it is not told as removed. A journal replayed for a new
// renderer does not bring back what an earlier one removed. A node that
// gives its words as text and as spans says them once.
func TestOnlyWhatReallyLeftIsKeptAndOnlyForItsRenderer(t *testing.T) {
	c, root := birthCache(t)
	frame(t, c, 2, `["add","t","main",null,{"id":"t","k":"toast","p":{"text":"Saved","spans":[{"t":"Saved"}]},"c":[{"id":"t/more","k":"text","p":{"text":"twice"}}]}]`)
	// The program adopts its view and adds the toast again with a part
	// this renderer does not take: the add is refused, the toast stays.
	cacheApply(t, c, 'q', `{"q":"hello","v":[1],"app":"example","features":["send"]}`)
	cacheApply(t, c, 'o', `{"id":"s:1","mode":"inline","adopt":true}`)
	frame(t, c, 3, `["add","t","main",null,{"id":"t","k":"toast","p":{"text":"Other"},"c":[{"id":"t/bad","k":"not-a-kind"}]}]`)
	v := served(t, c)["s:1"]
	if len(v.Recent) != 0 || born(v.Document.Root, "t") != 2 {
		t.Fatalf("after a refused replacement: recent %+v, toast born in %d", v.Recent, born(v.Document.Root, "t"))
	}
	// Really removed, it is kept, with its words once and its parts on
	// their own lines.
	frame(t, c, 4, `["del","t"]`)
	v = served(t, c)["s:1"]
	if len(v.Recent) != 1 || v.Recent[0].Text != "Saved\ntwice" || v.Recent[0].Born != 2 || v.Recent[0].Gone != 4 {
		t.Fatalf("recent = %+v", v.Recent)
	}
	// The journal now holds the toast's add and del, not checkpointed.
	c.Close()
	again, err := OpenCache(root, CacheIdentity{Generation: "run", Birth: "process", Conversation: "chat"}, birthKinds)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	again.mu.Lock()
	kept := 0
	for _, view := range again.scene.Views {
		kept += len(view.Document.recent.entries)
		if view.Document.recent.dropped {
			kept++
		}
	}
	again.mu.Unlock()
	if kept != 0 {
		t.Errorf("a new renderer was given %d removed results from the journal", kept)
	}
}

// Births and removed results are extra. A scene that fits is never refused
// because of them: the list of removed results is left out first, and that
// is said by the floor; then the births.
func TestWhatIsAddedToAServedSceneGivesWayToItsSize(t *testing.T) {
	c, _ := birthCache(t)
	frame(t, c, 2, `["add","t","main",null,{"id":"t","k":"toast","p":{"text":"`+strings.Repeat("r", 600)+`"}}]`)
	frame(t, c, 3, `["del","t"],["add","m1","main",null,{"id":"m1","k":"text","p":{"text":"x"}}]`)
	c.mu.Lock()
	defer c.mu.Unlock()
	size := func(level int) int {
		c.scene.stamp(level)
		raw, _ := json.Marshal(c.scene)
		c.scene.stamp(serveNothing)
		return len(raw)
	}
	plain, floors, births, all := size(serveNothing), size(serveFloors), size(serveBirths), size(serveAll)
	if !(plain < floors && floors < births && births < all) {
		t.Fatalf("sizes: plain %d, floors %d, births %d, all %d", plain, floors, births, all)
	}
	for limit, want := range map[int]struct{ recent, floor, born bool }{
		all:        {true, false, true},
		all - 1:    {false, true, true},
		births - 1: {false, true, false},
		floors - 1: {false, false, false},
		plain - 1:  {false, false, false}, // the scene itself is too big: served as it is, for the caller to refuse
	} {
		raw, err := c.scene.served(limit)
		if err != nil {
			t.Fatal(err)
		}
		got := string(raw)
		if strings.Contains(got, `"recent":[`) != want.recent || strings.Contains(got, `"recentFloor":2`) != want.floor || strings.Contains(got, `"b":3`) != want.born {
			t.Errorf("limit %d: recent %v floor %v births %v, want %+v", limit, strings.Contains(got, `"recent":[`), strings.Contains(got, `"recentFloor":2`), strings.Contains(got, `"b":3`), want)
		}
		if limit >= plain && len(raw) > limit {
			t.Errorf("limit %d: served %d bytes", limit, len(raw))
		}
	}
	if c.scene.Views[0].Document.nodes["m1"].node.Born != 0 || c.scene.Views[0].Recent != nil {
		t.Error("the held scene was left stamped")
	}
}
