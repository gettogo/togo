package surface

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// Found in review: a scene restored from disk, and a view the program has
// taken up again but not yet redrawn, still hold what an earlier renderer
// left. Served in that window, the phone would see a list that is no
// longer there. Each step of a recorded omp session is applied in turn and
// the cache is asked after every one: nothing is read until the program
// has greeted this renderer and drawn the view it adopted.
func TestNothingIsReadUntilTheProgramHasDrawnForThisRenderer(t *testing.T) {
	raw, err := os.ReadFile("testdata/omp-adopt-frames.json")
	if err != nil {
		t.Fatal(err)
	}
	var journal []struct {
		Verb byte            `json:"verb"`
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &journal); err != nil || len(journal) != 7 {
		t.Fatal("recorded session", err, len(journal))
	}
	options := RenderOptions{Cols: 100, Dark: true, Kinds: []string{"col", "row", "card", "section", "rule", "spacer", "text", "md", "code", "diff", "badge", "kbd",
		"icon", "spinner", "progress", "list", "item", "tabs", "editor", "input", "button", "status", "seg",
		"overlay", "toast", "picker", "prefs", "image", "meter", "kv", "table"}}
	identity := CacheIdentity{Generation: "run", Birth: "process", Conversation: "chat"}
	root := t.TempDir()
	pickers := func(c *Cache) (n int, err error) {
		err = c.Inspect(func(s *Scene) error {
			var walk func(*Node)
			walk = func(node *Node) {
				if node.Kind == "picker" {
					n++
				}
				for _, child := range node.Children {
					walk(child)
				}
			}
			for _, v := range s.Views {
				if !v.Closed {
					walk(v.Document.Root)
				}
			}
			return nil
		})
		return n, err
	}
	readable := func(c *Cache) bool {
		_, _, snapErr := c.Snapshot()
		_, inspectErr := pickers(c)
		if errors.Is(snapErr, ErrCachePending) != errors.Is(inspectErr, ErrCachePending) {
			t.Fatalf("reading and acting disagree: %v, %v", snapErr, inspectErr)
		}
		if snapErr != nil && !errors.Is(snapErr, ErrCachePending) {
			t.Fatal(snapErr)
		}
		return snapErr == nil
	}
	apply := func(c *Cache, i int) {
		if _, err := c.Apply(Message{Verb: journal[i].Verb, Body: journal[i].Body}); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
	}

	// The first renderer: greeting, view, its content, the list opening.
	first, err := OpenCache(root, identity, options)
	if err != nil {
		t.Fatal(err)
	}
	if !readable(first) {
		t.Fatal("a cache with nothing restored cannot be read")
	}
	for i := 0; i < 4; i++ {
		apply(first, i)
		if !readable(first) {
			t.Fatalf("the first renderer cannot be read after entry %d", i)
		}
	}
	if n, _ := pickers(first); n != 1 {
		t.Fatalf("the list is not in the first renderer's scene: %d", n)
	}
	first.Close()

	// The next renderer opens the same cache. The list was closed at the
	// desk meanwhile; the restored scene still holds it.
	second, err := OpenCache(root, identity, options)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if readable(second) {
		t.Fatal("a restored scene was read before the program greeted this renderer")
	}
	apply(second, 4) // greeting: every earlier view is closed
	if !readable(second) {
		t.Fatal("after the greeting, with no live view, the scene cannot be read")
	}
	if n, _ := pickers(second); n != 0 {
		t.Fatalf("a closed view's list is served as live: %d", n)
	}
	apply(second, 5) // the view is adopted, not yet redrawn
	if readable(second) {
		t.Fatal("an adopted view was read before its first frame")
	}
	apply(second, 6) // its first frame
	if !readable(second) {
		t.Fatal("the adopted view cannot be read after its first frame")
	}
	if n, _ := pickers(second); n != 0 {
		t.Fatalf("the closed list is still served: %d", n)
	}
}
