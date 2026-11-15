package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/termmenu"
)

// A row's id names the row, not its place. After a search the list holds
// other rows in the same places; an id from before the search must choose
// its own row or nothing, and never the row now in its place.
func TestARowIDNamesTheRowNotItsPlace(t *testing.T) {
	all := []termmenu.Item{{Label: "Big Pickle"}, {Label: "DeepSeek"}, {Label: "GLM"}, {Label: "GLM", Group: "Other"}, {Label: "GLM"}}
	ids := rowIDs(all)
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("ids repeat: %v", ids)
		}
		seen[id] = true
	}
	filtered := []termmenu.Item{{Label: "GLM"}, {Label: "DeepSeek"}}
	after := rowIDs(filtered)
	if after[0] != ids[2] || after[1] != ids[1] {
		t.Errorf("the same rows got other ids after a search: %v then %v", ids, after)
	}
	if after[0] == ids[0] {
		t.Error("the first place kept the id of the row that was there before")
	}
	// A cursor or a mark on the row does not change its id.
	if moved := rowIDs([]termmenu.Item{{Label: "GLM", Cursor: true, Current: true, Detail: "Free"}}); moved[0] != ids[2] {
		t.Errorf("id changed with the cursor: %q, %q", moved[0], ids[2])
	}
}

// Choosing with an id whose row the search removed presses no key.
func TestAnIDWhoseRowIsGoneChoosesNothing(t *testing.T) {
	const id = "s_fakefake04"
	e, pane := newFake(t, id, "1.2.3")
	e.cfg.StyledScreen = func(context.Context, string) (string, error) { return "", nil }
	before := []termmenu.Item{{Label: "Big Pickle"}, {Label: "DeepSeek"}}
	old := rowIDs(before)[0]
	e.setMenu(id, &terminalMenu{token: "tok", panel: "Select model", rows: []termmenu.Item{{Label: "DeepSeek"}}})
	keys := func() int {
		pane.mu.Lock()
		defer pane.mu.Unlock()
		return len(pane.keys)
	}
	n := keys()
	token, _ := json.Marshal("tok")
	res, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "terminal.select", Target: "tok/" + old,
		Args: map[string]json.RawMessage{"interaction": token}})
	if err != nil || res.Result != harness.ResultFailed || !strings.Contains(res.Message, "no longer in the list") {
		t.Fatalf("stale row: %+v, %v", res, err)
	}
	if keys() != n {
		t.Error("a key was pressed for a row that is no longer in the list")
	}
	if !e.drivingMenu(id) {
		t.Error("the list was dropped; it is still the phone's")
	}
}
