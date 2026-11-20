package approval

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/panelock"
)

// maxFormMoves bounds the Tab, arrow and Space presses of one step.
const maxFormMoves = 64

// otherPrefixCells is what an Other row draws before its typed words:
// "❯ ○ Other: ".
const otherPrefixCells = 11

// answerForm answers omp's form dialog (detect.ParseForm) with keys, one
// field at a time:
//
//   - Tab (Shift+Tab when the field lies behind) moves to the field whose
//     question the answer names; fields with no answer keep what they hold;
//   - on a group, Up and Down move the cursor and Space picks a radio or
//     flips a check box; typed words go into the Other row, which Enter
//     takes; a check-box group's Other that holds words it should not is
//     emptied;
//   - on a dropdown, Right and Left step to the option;
//   - on a text field, the words are typed (a field that holds other text
//     is emptied first with End and C-u).
//
// Then the whole form is read back, field by field from the first, and
// every answered field must show exactly the answer (checkForm); only then
// is Enter pressed on Submit, and only when the dialog has left the screen
// and, with open, the agent has recorded the answer (the question is no
// longer open) is the answer taken (formGone).
//
// Before every key the screen is read: the form must be there and its focus
// where the driver left it, or nothing is sent. Escape, Ctrl+C and Tab on
// Submit are never sent: they hand the keys to omp's composer. Typed words
// that did not show as typed are removed again, only while their field
// still has the focus. The caller holds the pane lock.
func (s *Store) answerForm(ctx context.Context, sessionID string, answers []AskAnswer) (formDriver, error) {
	for _, a := range answers {
		t := a.Text
		if strings.IndexFunc(t, unicode.IsControl) >= 0 {
			return formDriver{}, &AskError{"A typed answer with a line break or a tab can only be sent from the terminal"}
		}
		if t != strings.TrimSpace(t) {
			return formDriver{}, &AskError{"A typed answer that starts or ends with a space can only be sent from the terminal"}
		}
	}
	d := formDriver{s: s, ctx: ctx, target: target(sessionID), id: sessionID, gen: s.lockFor(sessionID).Gen()}
	cur, ok := d.settled()
	if !ok {
		return d, &AskError{"The question is not waiting at the terminal"}
	}
	for _, a := range answers {
		// Typed words must fit their row to be read back: an input scrolls
		// longer text and shows no sign of it.
		if cells(a.Text) > cur.Width-otherPrefixCells-1 {
			return d, &AskError{"An answer this long can only be typed in the terminal"}
		}
		if !cur.Scrolled && !asksAny(cur, []AskAnswer{a}) {
			// The whole form shows: a question it does not ask is refused
			// before any key.
			return d, &AskError{"The terminal shows a different question; nothing was sent"}
		}
	}
	var err error
	for _, a := range answers {
		if a.Keep {
			continue // Tab passes it on the way to the next one
		}
		if cur, err = d.seek(cur, func(f detect.Form) bool {
			return f.Stop != detect.FormSubmit && samePromptExact(f.Prompt, a.Prompt)
		}); err != nil {
			return d, err
		}
		switch cur.Stop {
		case detect.FormOption, detect.FormOther:
			cur, err = d.group(cur, a)
		case detect.FormSelect:
			cur, err = d.dropdown(cur, a)
		case detect.FormText:
			cur, err = d.text(cur, a)
		}
		if err != nil {
			return d, err
		}
	}
	if cur, err = d.checkForm(cur, answers); err != nil {
		return d, err
	}
	return d, d.press(isSubmit, false, "Enter")
}

func isSubmit(f detect.Form) bool { return f.Stop == detect.FormSubmit && f.Fix == 0 }

func samePromptExact(a, b string) bool { return strings.EqualFold(normalise(a), normalise(b)) }

// asksAny reports a form whose body shows one of the answers' questions:
// the same form, not a new one.
func asksAny(f detect.Form, answers []AskAnswer) bool {
	body := strings.ToLower(normalise(strings.Join(f.Lines, " ")))
	for _, a := range answers {
		if a.Prompt != "" && strings.Contains(body, strings.ToLower(normalise(a.Prompt))) {
			return true
		}
	}
	return false
}

func normalise(s string) string { return strings.Join(strings.Fields(s), " ") }

// cells counts the terminal cells s takes, a wide character as two.
func cells(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x1100 && (r <= 0x115f || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 ||
			r >= 0xf900 && r <= 0xfaff || r >= 0xfe30 && r <= 0xfe4f || r >= 0xff00 && r <= 0xff60 ||
			r >= 0xffe0 && r <= 0xffe6 || r >= 0x1f300 && r <= 0x1faff || r >= 0x20000) {
			n++
		}
	}
	return n
}

type formDriver struct {
	s      *Store
	ctx    context.Context
	target string
	id     string
	gen    uint64 // the pane's stop generation when the answer began
}

func (d formDriver) read() (detect.Form, bool) {
	screen, err := d.s.cfg.Tmux.CapturePane(d.ctx, d.target)
	if err != nil {
		return detect.Form{}, false
	}
	return detect.ParseForm(screen)
}

// press reads the screen and sends keys only while the form is there and
// expect holds for it: the focus is where the driver left it.
func (d formDriver) press(expect func(detect.Form) bool, literal bool, keys ...string) error {
	if panelock.Stopped(d.ctx, d.id, d.gen) {
		return &AskError{"A stop was sent to the terminal; nothing more was typed"}
	}
	f, found := d.read()
	if !found {
		return &AskError{"The form left the screen; nothing more was sent"}
	}
	if !expect(f) {
		return &AskError{"The form changed at the terminal; stopped there. Finish in the terminal"}
	}
	if err := d.s.cfg.Tmux.SendKeys(d.ctx, d.target, literal, keys...); err != nil {
		return &AskError{"Sending keys to the terminal failed"}
	}
	return nil
}

// await waits for the form to show what ok wants.
func (d formDriver) await(ok func(detect.Form) bool) (detect.Form, bool) {
	deadline := time.Now().Add(askWait)
	for {
		f, found := d.read()
		if found && ok(f) {
			return f, true
		}
		if time.Now().After(deadline) || d.ctx.Err() != nil {
			return f, false
		}
		time.Sleep(askPoll)
	}
}

// settled waits for the form, lets it stand for askSettle so omp takes
// keys, and reads it again.
func (d formDriver) settled() (detect.Form, bool) {
	any := func(f detect.Form) bool { return f.Stop != detect.FormNone }
	if _, ok := d.await(any); !ok {
		return detect.Form{}, false
	}
	if askSettle > 0 {
		select {
		case <-d.ctx.Done():
			return detect.Form{}, false
		case <-time.After(askSettle):
		}
	}
	return d.await(any)
}

// same reports that two reads show the focus on the same row.
func same(a, b detect.Form) bool {
	return a.Stop == b.Stop && a.Prompt == b.Prompt && a.Cursor == b.Cursor
}

// at is the check that the focus is still on cur's row.
func at(cur detect.Form) func(detect.Form) bool {
	return func(f detect.Form) bool { return same(f, cur) }
}

// seek moves the focus field by field until want holds: Tab forward, then
// Shift+Tab back from Submit. Tab is never pressed on Submit.
func (d formDriver) seek(cur detect.Form, want func(detect.Form) bool) (detect.Form, error) {
	for _, key := range []string{"Tab", "BTab"} {
		for n := 0; n < maxFormMoves; n++ {
			if want(cur) {
				return cur, nil
			}
			if key == "Tab" && cur.Stop == detect.FormSubmit {
				break
			}
			prev := cur
			if err := d.press(at(prev), false, key); err != nil {
				return cur, err
			}
			next, ok := d.await(func(f detect.Form) bool { return !same(f, prev) })
			if !ok {
				break // the first or last field: the focus stays
			}
			cur = next
		}
	}
	if want(cur) {
		return cur, nil
	}
	return cur, &AskError{"The terminal shows a different question; stopped there"}
}

// leave moves past the field cur is on with key (Tab, or Enter on an Other
// row) and waits for the focus to leave it.
func (d formDriver) leave(cur detect.Form, key string) (detect.Form, error) {
	if err := d.press(at(cur), false, key); err != nil {
		return cur, err
	}
	next, ok := d.await(func(f detect.Form) bool { return f.Stop == detect.FormSubmit || f.Prompt != cur.Prompt })
	if !ok {
		return cur, &AskError{"The terminal did not move on from " + cur.Prompt}
	}
	return next, nil
}

// moveTo puts the cursor on row idx of cur's group, one arrow at a time.
func (d formDriver) moveTo(cur detect.Form, idx int) (detect.Form, error) {
	for n := 0; cur.Cursor != idx; n++ {
		key, want := "Down", cur.Cursor+1
		if idx < cur.Cursor {
			key, want = "Up", cur.Cursor-1
		}
		if n >= maxFormMoves {
			return cur, &AskError{"The cursor did not move; finish in the terminal"}
		}
		if err := d.press(at(cur), false, key); err != nil {
			return cur, err
		}
		prompt := cur.Prompt
		next, ok := d.await(func(f detect.Form) bool {
			return (f.Stop == detect.FormOption || f.Stop == detect.FormOther) && f.Prompt == prompt && f.Cursor == want
		})
		if !ok {
			return cur, &AskError{"The cursor did not move; finish in the terminal"}
		}
		cur = next
	}
	return cur, nil
}

// dropdown steps a dropdown to the chosen option: Right to the end, then Left.
func (d formDriver) dropdown(cur detect.Form, a AskAnswer) (detect.Form, error) {
	if a.Text != "" || len(a.Labels) != 1 {
		return cur, &AskError{"Pick one answer"}
	}
	want := normalise(a.Labels[0])
	for _, key := range []string{"Right", "Left"} {
		for n := 0; n < maxFormMoves && normalise(cur.Value) != want; n++ {
			prev := cur
			if err := d.press(func(f detect.Form) bool { return same(f, prev) && f.Value == prev.Value }, false, key); err != nil {
				return cur, err
			}
			next, ok := d.await(func(f detect.Form) bool {
				return f.Stop == detect.FormSelect && f.Prompt == prev.Prompt && f.Value != prev.Value
			})
			if !ok {
				break // the end of the list; the next press reads the screen again
			}
			cur = next
		}
	}
	if normalise(cur.Value) != want {
		return cur, &AskError{"The terminal does not offer " + a.Labels[0]}
	}
	return d.leave(cur, "Tab")
}

// text answers a text field or text area.
func (d formDriver) text(cur detect.Form, a AskAnswer) (detect.Form, error) {
	t, ok := textOf(a)
	if !ok {
		return cur, &AskError{"This question takes typed words"}
	}
	var err error
	if cur, err = d.typeInto(cur, t); err != nil {
		return cur, err
	}
	return d.leave(cur, "Tab")
}

// textOf is the words an answer types into a text field.
func textOf(a AskAnswer) (string, bool) {
	t := a.Text
	if t == "" && len(a.Labels) == 1 {
		t = a.Labels[0]
	}
	return t, t != "" && len(a.Labels) <= 1
}

// shownText is what the focused input holds: "" for an empty one, which
// shows its placeholder after the cursor's cell.
func shownText(f detect.Form) string {
	if strings.HasPrefix(f.Value, " ") {
		return ""
	}
	return f.Value
}

// typeInto types t into the focused input (an Other row or a text field)
// and waits for it to show exactly t. A field that holds other text is
// emptied first; a text area of several lines is not touched. t == ""
// only empties the field.
func (d formDriver) typeInto(cur detect.Form, t string) (detect.Form, error) {
	if shownText(cur) == t {
		return cur, nil
	}
	here := func(f detect.Form) bool {
		return f.Stop == cur.Stop && f.Prompt == cur.Prompt && f.Cursor == cur.Cursor
	}
	if shownText(cur) != "" {
		lines := 0
		for _, v := range cur.Values {
			if strings.TrimSpace(v) != "" {
				lines++
			}
		}
		if lines > 1 {
			return cur, &AskError{"This field already holds text; finish in the terminal"}
		}
		if err := d.press(here, false, "End", "C-u"); err != nil {
			return cur, err
		}
		next, ok := d.await(func(f detect.Form) bool { return here(f) && shownText(f) == "" })
		if !ok {
			return cur, &AskError{"The field could not be emptied; finish in the terminal"}
		}
		cur = next
	}
	if t == "" {
		return cur, nil
	}
	if err := d.press(func(f detect.Form) bool { return here(f) && shownText(f) == "" }, true, t); err != nil {
		return cur, err
	}
	next, ok := d.await(func(f detect.Form) bool { return here(f) && shownText(f) == t })
	if !ok {
		// Never leave half an answer in the field, and never erase
		// anything else: only while this field still has the focus.
		_ = d.press(here, false, "End", "C-u")
		return cur, &AskError{"The typed answer did not show as sent; finish in the terminal"}
	}
	return next, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
