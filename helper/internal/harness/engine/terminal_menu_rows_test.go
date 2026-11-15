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

// A list one connection is driving cannot be driven by another, even with
// its token.
func TestAListBelongsToTheConnectionThatOpenedIt(t *testing.T) {
	const id = "s_fakefake05"
	e, pane := newFake(t, id, "1.2.3")
	e.cfg.StyledScreen = func(context.Context, string) (string, error) { return "", nil }
	type key struct{}
	e.cfg.Client = func(ctx context.Context) any { return ctx.Value(key{}) }
	phone := context.WithValue(context.Background(), key{}, "phone")
	other := context.WithValue(context.Background(), key{}, "tablet")
	e.setMenu(id, &terminalMenu{client: "phone", token: "tok", panel: "Select model", rows: []termmenu.Item{{Label: "DeepSeek"}}})
	if e.menuOf(other, id, "tok") != nil {
		t.Error("another connection got the list")
	}
	if e.menuOf(phone, id, "tok") == nil {
		t.Error("the connection that opened the list lost it")
	}
	token, _ := json.Marshal("tok")
	pane.mu.Lock()
	n := len(pane.keys)
	pane.mu.Unlock()
	res, err := e.Act(other, harness.ActionRequest{Session: id, Action: "terminal.cancel", Args: map[string]json.RawMessage{"interaction": token}})
	pane.mu.Lock()
	after := len(pane.keys)
	pane.mu.Unlock()
	if err != nil || after != n {
		t.Errorf("another connection's cancel: %+v %v, keys pressed %d", res, err, after-n)
	}
	if !e.drivingMenu(id) {
		t.Error("another connection's cancel ended the list")
	}
	// A second device cannot start a command while the list is open either.
	text, _ := json.Marshal("/models")
	res, err = e.Act(other, harness.ActionRequest{Session: id, Action: "terminal.command", Args: map[string]json.RawMessage{"text": text}})
	if err != nil || res.Result != harness.ResultFailed {
		t.Errorf("second device's command: %+v %v", res, err)
	}
}

// A command for the terminal is one slash command on one line, of bounded
// length, under the same argument limits as every action.
func TestOnlyASingleSlashCommandIsTyped(t *testing.T) {
	const id = "s_fakefake06"
	e, pane := newFake(t, id, "1.2.3")
	e.cfg.StyledScreen = func(context.Context, string) (string, error) { return "", nil }
	typed := func() int {
		pane.mu.Lock()
		defer pane.mu.Unlock()
		return len(pane.typed) + len(pane.keys)
	}
	n := typed()
	for _, text := range []string{"hello", "/", "/models\n/exit", "/models\rrm -rf x", "/a\x1b[2J", "/" + strings.Repeat("x", maxArgString+1), "  ", "/m /exit"} {
		raw, _ := json.Marshal(text)
		res, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "terminal.command", Args: map[string]json.RawMessage{"text": raw}})
		// Refused either by the argument limits every action has (control
		// characters) or as not being a single command.
		if err == nil && res.Result != harness.ResultFailed {
			t.Errorf("%q was accepted: %+v", text, res)
		}
	}
	if typed() != n {
		t.Errorf("something was typed for a refused command")
	}
	// Too many arguments are refused like any action's.
	args := map[string]json.RawMessage{}
	for i := 0; i < 100; i++ {
		args[strings.Repeat("k", i+1)] = json.RawMessage(`1`)
	}
	if _, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "terminal.command", Args: args}); err == nil {
		t.Error("the argument limit does not apply to terminal actions")
	}
}

// A list belongs to the running session and the conversation it was opened
// in. After the chat starts over for another conversation the list is gone:
// its token opens nothing and no key is pressed for it.
func TestAListEndsWithItsConversation(t *testing.T) {
	const id = "s_fakefake07"
	e, pane := newFake(t, id, "1.2.3")
	e.cfg.StyledScreen = func(context.Context, string) (string, error) { return "", nil }
	ctx := context.Background()
	s, err := e.served(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	conv := s.convGen
	s.mu.Unlock()
	e.setMenu(id, &terminalMenu{sess: s, conv: conv, token: "tok", panel: "Select model", rows: []termmenu.Item{{Label: "DeepSeek"}}})
	if !e.drivingMenu(id) || e.menuOf(ctx, id, "tok") == nil {
		t.Fatal("the list is not held in its own conversation")
	}
	call := withMenuConv(ctx, s)
	s.mu.Lock()
	s.convGen++
	s.mu.Unlock()
	pane.mu.Lock()
	n := len(pane.keys)
	pane.mu.Unlock()
	if err := e.menuKeys(call, s, "Enter"); err == nil {
		t.Error("a key was allowed after the conversation changed")
	}
	if err := e.menuType(call, s, "glm"); err == nil {
		t.Error("text was allowed after the conversation changed")
	}
	pane.mu.Lock()
	after := len(pane.keys) + 0
	pane.mu.Unlock()
	if after != n {
		t.Error("a key was pressed after the conversation changed")
	}
	if e.drivingMenu(id) || e.menuOf(ctx, id, "tok") != nil {
		t.Error("the list outlived its conversation")
	}
	e.menuMu.Lock()
	_, left := e.leftOpen[id]
	e.menuMu.Unlock()
	if left {
		t.Error("a list of the old conversation still blocks typing in the new one")
	}
}
