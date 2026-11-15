package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../termmenu/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A sign-in panel is shown as its lines, with the address as a link the
// phone opens itself. It offers no key, and reading it presses none.
func TestASignInPanelIsShownAsItsLinesWithItsLink(t *testing.T) {
	const id = "s_fakefake51"
	e, pane := newFake(t, id, "1.2.3")
	shown := readFixture(t, "oc-device-login.ansi")
	e.cfg.StyledScreen = func(context.Context, string) (string, error) { return shown, nil }
	ctx := context.Background()
	s, err := e.served(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := e.readPanel(ctx, s)
	if !ok {
		t.Fatal("the panel is not read")
	}
	sc := e.openPanel(withMenuConv(ctx, s), s, "/connect", p).Screen
	if sc.Primitive != harness.PrimDetail || !strings.HasPrefix(sc.ID, panelScreenPrefix) || sc.Interaction == "" ||
		sc.Title != "Login with GitHub Copilot" || sc.Detail.Body.Kind != harness.BodyText ||
		sc.Detail.Body.Text != "https://github.com/login/device\nEnter code: WXYZ-1234\nWaiting for authorization…" {
		t.Fatalf("panel screen = %+v, detail %+v", sc, sc.Detail)
	}
	if len(sc.Detail.Actions) != 1 {
		t.Fatalf("actions = %+v", sc.Detail.Actions)
	}
	a := sc.Detail.Actions[0]
	token, _ := json.Marshal(sc.Interaction)
	if a.ID != "terminal.openLink.0" || a.Label != "Open link" || a.Role != "primary" ||
		string(a.Args["url"]) != `"https://github.com/login/device"` || string(a.Args["interaction"]) != string(token) {
		t.Errorf("link action = %+v", a)
	}
	// The app requires the screen's actions to be a list, never null.
	if wire, _ := json.Marshal(sc); !strings.Contains(string(wire), `"actions":[]`) {
		t.Errorf("panel screen on the wire: %s", wire)
	}
	// Read again under the same interaction it is the same panel.
	again, err := e.Screen(ctx, id, "terminal.menu", map[string]string{"interaction": sc.Interaction})
	if err != nil || again.ID != sc.ID || again.Detail == nil {
		t.Fatalf("read again: %+v, %v", again, err)
	}
	// Taken up from a screen nobody is driving, it is the same panel too.
	e.setMenu(id, nil)
	res, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: "terminal.attach"})
	if err != nil || res.Screen == nil || res.Screen.Primitive != harness.PrimDetail {
		t.Fatalf("attach: %+v, %v", res, err)
	}
	pane.mu.Lock()
	defer pane.mu.Unlock()
	if len(pane.typed) != 0 || len(pane.keys) != 0 {
		t.Errorf("showing a panel typed %v and pressed %v", pane.typed, pane.keys)
	}
}

// When the panel goes, what took its place is handed over under a new
// interaction; with nothing in its place the panel has closed.
func TestAPanelThatGoesHandsOverWhatTookItsPlace(t *testing.T) {
	const id = "s_fakefake52"
	e, _ := newFake(t, id, "1.2.3")
	shown := readFixture(t, "oc-device-login.ansi")
	e.cfg.StyledScreen = func(context.Context, string) (string, error) { return shown, nil }
	ctx := context.Background()
	s, err := e.served(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := e.readPanel(ctx, s)
	sc := e.openPanel(withMenuConv(ctx, s), s, "/connect", p).Screen
	shown = readFixture(t, "oc-models.ansi")
	next, err := e.Screen(ctx, id, "terminal.menu", map[string]string{"interaction": sc.Interaction})
	if err != nil || next.Primitive != harness.PrimPicker || next.Interaction == "" || next.Interaction == sc.Interaction {
		t.Fatalf("after the panel: %+v, %v", next, err)
	}
	var fb *harness.FallbackError
	if _, err := e.Screen(ctx, id, "terminal.menu", map[string]string{"interaction": sc.Interaction}); !errors.As(err, &fb) || fb.Reason != "closed" {
		t.Errorf("the old interaction still answers: %v", err)
	}

	const id2 = "s_fakefake53"
	e2, _ := newFake(t, id2, "1.2.3")
	shown2 := readFixture(t, "oc-device-login.ansi")
	e2.cfg.StyledScreen = func(context.Context, string) (string, error) { return shown2, nil }
	s2, err := e2.served(ctx, id2)
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := e2.readPanel(ctx, s2)
	sc2 := e2.openPanel(withMenuConv(ctx, s2), s2, "/connect", p2).Screen
	shown2 = ""
	if _, err := e2.Screen(ctx, id2, "terminal.menu", map[string]string{"interaction": sc2.Interaction}); !errors.As(err, &fb) || fb.Reason != "closed" {
		t.Errorf("a panel with nothing after it: %v", err)
	}
}

// Closing presses Escape once, on that panel only. A panel left open holds
// typing until it is gone: its keys act the moment they arrive.
func TestAPanelIsClosedByEscapeAndHoldsTypingWhileOpen(t *testing.T) {
	const id = "s_fakefake54"
	e, pane := newFake(t, id, "1.2.3")
	shown := readFixture(t, "oc-device-login.ansi")
	e.cfg.StyledScreen = func(context.Context, string) (string, error) { return shown, nil }
	ctx := context.Background()
	s, err := e.served(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	keys := func() []string {
		pane.mu.Lock()
		defer pane.mu.Unlock()
		return append([]string(nil), pane.keys...)
	}
	cancel := func(token string) harness.ActionResult {
		tok, _ := json.Marshal(token)
		res, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: "terminal.cancel", Args: map[string]json.RawMessage{"interaction": tok}})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	p, _ := e.readPanel(ctx, s)
	// The screen is a fixed capture: Escape is pressed and the panel is
	// seen to stay, which is said.
	sc := e.openPanel(withMenuConv(ctx, s), s, "/connect", p).Screen
	if res := cancel(sc.Interaction); res.Result != harness.ResultFailed || strings.Join(keys(), ",") != "Escape" {
		t.Fatalf("cancel = %+v, keys %v", res, keys())
	}
	// Another screen in its place takes no key.
	sc = e.openPanel(withMenuConv(ctx, s), s, "/connect", p).Screen
	shown = readFixture(t, "oc-models.ansi")
	if res := cancel(sc.Interaction); res.Result != harness.ResultApplied || len(keys()) != 1 {
		t.Fatalf("cancel on another screen = %+v, keys %v", res, keys())
	}
	shown = readFixture(t, "oc-device-login.ansi")
	e.openPanel(withMenuConv(ctx, s), s, "/connect", p)
	e.setMenu(id, nil) // released
	if _, held := e.menuLeftOpen(s); !held {
		t.Fatal("a released panel still on the screen does not hold typing")
	}
	if e.heldNatively(s) == nil {
		t.Error("typing is allowed into a panel left open")
	}
	shown = ""
	if _, held := e.menuLeftOpen(s); held {
		t.Error("the hold outlived the panel")
	}
}
