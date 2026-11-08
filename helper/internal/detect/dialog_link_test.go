package detect

import (
	"reflect"
	"testing"
)

// Cline 3.0.74's "Introducing Cline Desktop" splash (captured live at 100
// and 47 columns): Enter opens https://cline.bot/desktop in the browser,
// any other key closes it. It is a dialog whose only safe key is Escape.
func TestClineSplashDismissesWithEscape(t *testing.T) {
	for _, name := range []string{"cline/real-desktop-splash", "cline/real-desktop-splash-47"} {
		screen := fixture(t, name)
		if _, ok := Detect(screen); ok {
			t.Errorf("%s: splash offered as a prompt", name)
		}
		d, ok := Dialog(screen)
		if !ok || d.Title != "Introducing Cline Desktop" {
			t.Fatalf("%s: dialog = %+v, %v", name, d, ok)
		}
		if !reflect.DeepEqual(d.Dismiss, []string{"Escape"}) {
			t.Errorf("%s: dismiss = %v, want [Escape]", name, d.Dismiss)
		}
		if !EnterOpensLink(screen) {
			t.Errorf("%s: Enter-opens-link not seen", name)
		}
	}
}

// A link splash that names no other key is still a dialog, with nothing
// to dismiss it: the user answers it at the terminal.
func TestLinkSplashWithoutSafeKey(t *testing.T) {
	d, ok := Dialog("\n  Introducing Foo Cloud\n  https://foo.dev\n  Press Enter to open\n")
	if !ok || d.Dismiss != nil || d.Prompt != nil {
		t.Fatalf("dialog = %+v, %v", d, ok)
	}
}

// No option that presses Enter is offered on a screen whose Enter opens a
// link: a phone tap must never launch the Mac's browser.
func TestEnterNeverOfferedWhenItOpensALink(t *testing.T) {
	screen := `
  Open the docs?
  ❯ Open docs
    Not now
  Press Enter to open in your browser
`
	if p, ok := Detect(screen); ok {
		for _, o := range p.Options {
			if hasEnter(p.Keys[o.N]) {
				t.Fatalf("option %+v presses Enter: %v", o, p.Keys[o.N])
			}
		}
	}
	if d, ok := Dialog(screen); !ok || d.Prompt != nil {
		t.Fatalf("dialog = %+v, %v; want a guard-only dialog", d, ok)
	}
}

// Copilot 1.0.107's second start-up question (text from the conformance
// run; the dialog needs folder trust first, which the capture may not
// answer): its "No (Esc)" option is the decline.
func TestCopilotKeyBindingQuestion(t *testing.T) {
	screen := `
╭──────────────────────────────────────────────────────────────────────╮
│ Would you like to add this key binding to your terminal configuration? │
│                                                                        │
│   1. Yes                                                               │
│ ❯ 2. No (Esc)                                                          │
│                                                                        │
│ ↑/↓ to navigate · enter to select · esc to cancel                      │
╰──────────────────────────────────────────────────────────────────────╯
`
	p := mustDetect(t, screen)
	if len(p.Options) != 2 || p.Options[1].Role != "deny" || !reflect.DeepEqual(p.Keys[2], []string{"Escape"}) {
		t.Fatalf("got %+v keys %v", p.Options, p.Keys)
	}
}

// Cursor Agent's folder trust: one-tap approve is "Trust this workspace",
// pressed with its own key, and Quit declines.
func TestCursorTrustOneTap(t *testing.T) {
	for _, name := range []string{"cursor/real-workspace-trust", "cursor/real-workspace-trust-47"} {
		p := mustDetect(t, fixture(t, name))
		if p.Options[0].Role != "approve" || !reflect.DeepEqual(p.Keys[1], []string{"a"}) {
			t.Errorf("%s: trust = %+v keys %v", name, p.Options[0], p.Keys[1])
		}
		if p.Options[1].Role != "deny" {
			t.Errorf("%s: quit = %+v", name, p.Options[1])
		}
	}
}
