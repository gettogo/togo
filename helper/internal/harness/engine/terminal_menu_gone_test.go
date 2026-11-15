package engine

import (
	"context"
	"errors"
	"testing"
)

// A list is driven for a phone. When the phone has gone (its connection
// ended, or the call was cancelled) no further key is pressed, Enter
// included, and the list counts as one the phone let go of.
func TestNoKeyIsPressedInAListForAPhoneThatHasGone(t *testing.T) {
	const id = "s_fakefake03"
	e, pane := newFake(t, id, "1.2.3")
	ctx := context.Background()
	s, err := e.served(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	pressed := func() int {
		pane.mu.Lock()
		defer pane.mu.Unlock()
		return len(pane.keys)
	}
	before := pressed()
	e.setMenu(id, &terminalMenu{token: "t", panel: "Select model"})
	gone := false
	e.cfg.ClientGone = func(context.Context) bool { return gone }
	if err := e.menuKeys(ctx, s, "Down"); err != nil || pressed() != before+1 {
		t.Fatalf("with the phone there: err %v, keys %d", err, pressed()-before)
	}
	gone = true
	if err := e.menuKeys(ctx, s, "Enter"); !errors.Is(err, errMenuAbandoned) {
		t.Fatalf("with the phone gone: %v", err)
	}
	if pressed() != before+1 {
		t.Errorf("a key was pressed for a phone that had gone")
	}
	if e.drivingMenu(id) {
		t.Error("the list is still held for the phone that left")
	}
	e.menuMu.Lock()
	left := e.leftOpen[id]
	e.menuMu.Unlock()
	if left != "Select model" {
		t.Errorf("the abandoned list is not remembered as left open: %q", left)
	}
	// A cancelled call stops it too.
	gone = false
	e.setMenu(id, &terminalMenu{token: "u", panel: "Select model"})
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := e.menuKeys(cancelled, s, "Enter"); !errors.Is(err, errMenuAbandoned) || pressed() != before+1 {
		t.Errorf("cancelled call: err %v, keys %d", err, pressed()-before)
	}
}
