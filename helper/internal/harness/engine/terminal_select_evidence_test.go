package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// attachList takes up the list on a fake session's screen and returns its
// screen, as the phone would get it.
func attachList(t *testing.T, e *Engine, id, title string) *harness.Screen {
	t.Helper()
	e.menuMu.Lock()
	e.leftOpen = map[string]string{id: title}
	e.menuMu.Unlock()
	res, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "terminal.attach"})
	if err != nil || res.Screen == nil || res.Screen.Picker == nil {
		t.Fatalf("attach: %+v, %v", res, err)
	}
	return res.Screen
}

func chooseRow(t *testing.T, e *Engine, id string, sc *harness.Screen, row int) harness.ActionResult {
	t.Helper()
	token, _ := json.Marshal(sc.Interaction)
	res, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "terminal.select",
		Target: sc.Picker.Items[row].ID, Args: map[string]json.RawMessage{"interaction": token}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// Found in review: as many rows on the screen as were read is not the same
// list. The list scrolled; the first row now is the grouped "Same", its
// heading gone, and it reads exactly like the ungrouped "Same" that was
// offered. Enter there would choose another row. No key is pressed.
func TestTheSameNumberOfRowsIsNotTheSameList(t *testing.T) {
	const id = "s_fakefake61"
	e, pane := newFake(t, id, "1.2.3")
	shown := listScreen("Pick one", []listLine{{label: "Same", cursor: true}, {heading: "Popular"}, {label: "Same"}, {label: "Other"}, {label: "Third"}, {label: "Fourth"}})
	e.cfg.StyledScreen = func(context.Context, string) (string, error) { return shown, nil }
	sc := attachList(t, e, id, "Pick one")
	if len(sc.Picker.Items) != 5 || sc.Picker.Items[0].Title != "Same" || sc.Picker.Items[1].Title != "Same" {
		t.Fatalf("rows = %+v", sc.Picker.Items)
	}
	shown = listScreen("Pick one", []listLine{{label: "Same", cursor: true}, {label: "Other"}, {label: "Third"}, {label: "Fourth"}, {label: "Fifth"}})
	res := chooseRow(t, e, id, sc, 0)
	pane.mu.Lock()
	defer pane.mu.Unlock()
	if res.Result != harness.ResultFailed || len(pane.keys) != 0 {
		t.Fatalf("choice on a scrolled list = %+v, keys %v", res, pane.keys)
	}
}

// A whole list still on the screen as it was read keeps its use: of two
// rows that say the same, the one meant is reached by its place.
func TestTwinsInAWholeUnchangedListAreToldApartByPlace(t *testing.T) {
	const id = "s_fakefake62"
	e, pane := newFake(t, id, "1.2.3")
	labels := []string{"First", "Twin", "Twin", "Last"}
	e.cfg.StyledScreen = func(context.Context, string) (string, error) {
		pane.mu.Lock()
		at, entered := 0, false
		for _, k := range pane.keys {
			switch k {
			case "Down":
				at++
			case "Up":
				at--
			case "Enter":
				entered = true
			}
		}
		pane.mu.Unlock()
		if entered {
			return "", nil
		}
		var lines []listLine
		for i, l := range labels {
			lines = append(lines, listLine{label: l, cursor: i == at})
		}
		return listScreen("Pick one", lines), nil
	}
	sc := attachList(t, e, id, "Pick one")
	chooseRow(t, e, id, sc, 2)
	pane.mu.Lock()
	defer pane.mu.Unlock()
	if got := strings.Join(pane.keys, ","); got != "Down,Down,Enter" {
		t.Fatalf("keys = %s, want Down,Down,Enter", got)
	}
}
