package engine

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/termmenu"
)

// A harness may ask for one line of text in its terminal: a key, a name, a
// code. The phone shows it as one write-only field. What is entered is
// pasted into the pane from standard input, never as a process argument,
// is not read back from the screen and is not logged.

const (
	promptField  = "terminal.input"
	promptAction = "terminal.input"
	maxPromptLen = 4096
)

func (e *Engine) readPrompt(ctx context.Context, s *sess) (termmenu.Prompt, bool) {
	if e.cfg.StyledScreen == nil {
		return termmenu.Prompt{}, false
	}
	screen, err := e.cfg.StyledScreen(ctx, s.id)
	if err != nil {
		return termmenu.Prompt{}, false
	}
	p, ok := termmenu.ReadPrompt(screen)
	if !ok {
		return termmenu.Prompt{}, false
	}
	// A box that only looks like a prompt (an example printed in a reply)
	// takes no typing. This one must hold the cursor: the one the harness
	// draws, or the terminal's own, shown, on the input line.
	if !p.Focused {
		if e.cfg.Cursor == nil {
			return termmenu.Prompt{}, false
		}
		x, y, visible, err := e.cfg.Cursor(ctx, s.id)
		if err != nil || !visible || y != p.Row {
			return termmenu.Prompt{}, false
		}
		// Empty means the cursor stands right after the mark: spaces typed
		// at the desk are text too.
		if p.Empty && e.cfg.Screen != nil {
			p.Empty = false
			if plain, err := e.cfg.Screen(ctx, s.id); err == nil {
				if lines := strings.Split(plain, "\n"); y < len(lines) {
					before := strings.TrimLeft(beforeCursor(lines[y], x), " │┃")
					switch before {
					case "> ", "❯ ", ">", "❯":
						p.Empty = true
					default:
						p.Empty = p.Label != "" && (before == p.Label+": " || before == p.Label+":")
					}
				}
			}
		}
	}
	return p, true
}

// openPrompt holds the prompt for the caller and returns its screen.
func (e *Engine) openPrompt(ctx context.Context, s *sess, title string, p termmenu.Prompt) harness.ActionResult {
	m := &terminalMenu{client: e.client(ctx), sess: s, conv: menuConv(ctx, s), token: newToken(), title: title,
		panel: p.Title, prompt: true, keys: hintKeys(p.Hint)}
	e.setMenu(s.id, m)
	return harness.ActionResult{Result: harness.ResultApplied, Screen: e.promptScreen(m, p)}
}

func (e *Engine) promptScreen(m *terminalMenu, p termmenu.Prompt) *harness.Screen {
	sc := &harness.Screen{
		ID: "terminal.prompt/" + m.token, Primitive: harness.PrimSettings, Title: p.Title, FetchedAt: e.cfg.Now(),
		Interaction: m.token, Actions: []harness.ActionRef{},
		Settings: &harness.Settings{Sections: []harness.SettingsSection{{Fields: []harness.Field{{
			ID: promptField, Label: promptLabel(p), Kind: harness.FieldSecret, Value: json.RawMessage("null"),
			Action: promptAction, ActionLabel: "Continue", Presentation: "prompt",
		}}}}},
	}
	// The title of a prompt that follows an entry may repeat the entry.
	if m.sess != nil {
		e.redactScreen(m.sess.id, sc)
	}
	return sc
}

// answerPrompt types the entry into the prompt and submits it. It types
// only into the prompt it was shown, and only while that prompt's field is
// empty: text someone typed there at the desk is not added to.
func (e *Engine) answerPrompt(ctx context.Context, s *sess, m *terminalMenu, value string) (harness.ActionResult, error) {
	failed := func(msg string) (harness.ActionResult, error) {
		return harness.ActionResult{Result: harness.ResultFailed, Message: msg}, nil
	}
	if value == "" || len(value) > maxPromptLen ||
		strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 }) {
		return failed("That entry cannot be typed.")
	}
	l := typingLock(s.id)
	l.Lock()
	defer l.Unlock()
	p, is := e.readPrompt(ctx, s)
	if !is || p.Title != m.panel {
		e.setMenu(s.id, nil)
		return failed("This prompt has closed. Nothing was typed.")
	}
	if !p.Empty {
		return failed("The prompt already holds text typed at the desk. Nothing was typed.")
	}
	if err := e.menuStillOurs(ctx, s); err != nil {
		return harness.ActionResult{}, err
	}
	e.menuKeyAt.Store(s.id, e.cfg.Now())
	if !e.noteEntry(s.id, value) {
		return failed("This session has taken too many entries to keep them out of what is shown. Use the terminal.")
	}
	if err := e.cfg.Typist.Type(ctx, s.id, value, false, false); err != nil {
		return harness.ActionResult{}, err
	}
	// Submitted only once the field is seen to hold something: Enter on a
	// field the text never reached would submit it empty.
	took := false
	for deadline := time.Now().Add(e.scaled(1500 * time.Millisecond)); time.Now().Before(deadline); {
		if now, is := e.readPrompt(ctx, s); is && now.Title == m.panel && !now.Empty {
			took = true
			break
		}
		time.Sleep(e.scaled(60 * time.Millisecond))
	}
	if !took {
		e.setMenu(s.id, nil)
		return e.notShown(s, composerMark{}), nil
	}
	if err := e.menuKeys(ctx, s, "Enter"); err != nil {
		return harness.ActionResult{}, err
	}
	// What follows: the prompt goes (a list, another prompt or the input
	// line takes its place), or the harness refuses the entry and shows
	// the same prompt empty again.
	for deadline := time.Now().Add(e.scaled(2500 * time.Millisecond)); time.Now().Before(deadline); {
		now, is := e.readPrompt(ctx, s)
		if !is || now.Title != m.panel {
			return e.afterKey(ctx, s, m.title, m)
		}
		if now.Empty {
			// The message never repeats the entry or what the harness
			// said about it.
			return harness.ActionResult{Result: harness.ResultFailed,
				Message: s.ad.Name + " did not accept this entry.", Screen: e.promptScreen(m, now)}, nil
		}
		time.Sleep(e.scaled(80 * time.Millisecond))
	}
	// Still showing the entry: not known to be taken or refused.
	e.setMenu(s.id, nil)
	return e.notShown(s, composerMark{}), nil
}

// promptLabel names the field: the input line's own label where the
// program draws one, else the box's title.
func promptLabel(p termmenu.Prompt) string {
	if p.Label != "" {
		return p.Label
	}
	return p.Title
}
