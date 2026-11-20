package approval

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/gettogo/togo/helper/internal/detect"
)

// maxFormText bounds a typed form answer: longer text is typed at the
// terminal.
const maxFormText = 4000

// maxFormMoves bounds the Tab, arrow and Space presses of one step.
const maxFormMoves = 64

// answerForm answers omp's form dialog (detect.ParseForm) with keys, one
// field at a time, reading the screen back after every key:
//
//   - Tab (Shift+Tab when the field lies behind) moves to the field whose
//     question the answer names; fields with no answer keep what they hold;
//   - on a group, Up and Down move the cursor and Space picks a radio or
//     flips a check box; typed words go into the Other row, which Enter
//     takes (a radio group's pick is dropped for it);
//   - on a dropdown, Right and Left step to the option;
//   - on a text field, the words are typed (an input that holds text is
//     emptied first with End and C-u);
//   - Enter on Submit, and the dialog must leave the screen.
//
// Escape, Ctrl+C and Tab on Submit are never sent: they hand the keys to
// omp's composer, where Escape cancels the ask. It stops at the first
// mismatch and says so; typed words that did not show as typed are removed
// again. The caller holds the pane lock.
func (s *Store) answerForm(ctx context.Context, sessionID string, answers []AskAnswer) error {
	for _, a := range answers {
		if strings.IndexFunc(a.Text, unicode.IsControl) >= 0 {
			return &AskError{"A typed answer with a line break or a tab can only be sent from the terminal"}
		}
		if len(a.Text) > maxFormText {
			return &AskError{"An answer this long can only be typed in the terminal"}
		}
	}
	d := formDriver{s: s, ctx: ctx, target: target(sessionID)}
	cur, ok := d.settled()
	if !ok {
		return &AskError{"The question is not waiting at the terminal"}
	}
	if !cur.Scrolled {
		// The whole form shows: a question it does not ask is refused
		// before any key.
		for _, a := range answers {
			if !asksAny(cur, []AskAnswer{a}) {
				return &AskError{"The terminal shows a different question; nothing was sent"}
			}
		}
	}
	var err error
	for _, a := range answers {
		if cur, err = d.seek(cur, func(f detect.Form) bool {
			return f.Stop != detect.FormSubmit && strings.EqualFold(normalise(f.Prompt), normalise(a.Prompt))
		}); err != nil {
			return err
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
			return err
		}
	}
	if cur, err = d.seek(cur, func(f detect.Form) bool { return f.Stop == detect.FormSubmit }); err != nil {
		return err
	}
	if err := d.keys(false, "Enter"); err != nil {
		return err
	}
	// Taken: the dialog left, or another form replaced it. A required
	// field still empty keeps it open with the focus on that field.
	if f, ok := d.gone(answers); !ok {
		if f.Stop == detect.FormSubmit {
			return &AskError{"The form is still open; check the terminal"}
		}
		return &AskError{"omp did not take the answers: a field needs something it does not accept. Finish in the terminal"}
	}
	return nil
}

// asksAny reports a form whose body shows one of the answers' questions:
// the same form, not a new one.
func asksAny(f detect.Form, answers []AskAnswer) bool {
	body := normalise(strings.Join(f.Lines, " "))
	for _, a := range answers {
		if a.Prompt != "" && strings.Contains(strings.ToLower(body), strings.ToLower(normalise(a.Prompt))) {
			return true
		}
	}
	return false
}

func normalise(s string) string { return strings.Join(strings.Fields(s), " ") }

type formDriver struct {
	s      *Store
	ctx    context.Context
	target string
}

func (d formDriver) read() (detect.Form, bool) {
	screen, err := d.s.cfg.Tmux.CapturePane(d.ctx, d.target)
	if err != nil {
		return detect.Form{}, false
	}
	return detect.ParseForm(screen)
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

// gone waits for the submitted form to leave the screen: no form, or one
// that asks none of the answered questions.
func (d formDriver) gone(answers []AskAnswer) (detect.Form, bool) {
	deadline := time.Now().Add(askWait)
	for {
		screen, err := d.s.cfg.Tmux.CapturePane(d.ctx, d.target)
		if err == nil {
			f, found := detect.ParseForm(screen)
			if !found || !asksAny(f, answers) {
				return f, true
			}
			if time.Now().After(deadline) || d.ctx.Err() != nil {
				return f, false
			}
		} else if time.Now().After(deadline) || d.ctx.Err() != nil {
			return detect.Form{}, false
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

func (d formDriver) keys(literal bool, keys ...string) error {
	if err := d.s.cfg.Tmux.SendKeys(d.ctx, d.target, literal, keys...); err != nil {
		return &AskError{"Sending keys to the terminal failed"}
	}
	return nil
}

// same reports that two reads show the focus on the same row.
func same(a, b detect.Form) bool {
	return a.Stop == b.Stop && a.Prompt == b.Prompt && a.Cursor == b.Cursor
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
			if err := d.keys(false, key); err != nil {
				return cur, err
			}
			prev := cur
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
	if err := d.keys(false, key); err != nil {
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
		if err := d.keys(false, key); err != nil {
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

func optionIndex(f detect.Form, label string) (int, bool) {
	for i, o := range f.Options {
		if !o.Text && strings.EqualFold(normalise(o.Label), normalise(label)) {
			return i, true
		}
	}
	return 0, false
}

func otherIndex(f detect.Form) int {
	for i, o := range f.Options {
		if o.Text {
			return i
		}
	}
	return -1
}

// group answers a radio or check-box group.
func (d formDriver) group(cur detect.Form, a AskAnswer) (detect.Form, error) {
	for _, l := range a.Labels {
		if _, ok := optionIndex(cur, l); !ok {
			return cur, &AskError{"The terminal does not offer " + l}
		}
	}
	other := otherIndex(cur)
	if a.Text != "" && other < 0 {
		return cur, &AskError{"This question takes no typed answer"}
	}
	if !cur.Multi && len(a.Labels)+boolInt(a.Text != "") != 1 {
		return cur, &AskError{"Pick one answer"}
	}
	if cur.Multi && a.Text == "" && other >= 0 && cur.Options[other].Detail != "" {
		// Typed at the terminal: it would count as an answer too.
		return cur, &AskError{"The terminal holds a typed answer to this question; finish there"}
	}
	var err error
	for i := range cur.Options {
		o := cur.Options[i]
		want := false
		for _, l := range a.Labels {
			if j, _ := optionIndex(cur, l); j == i {
				want = true
			}
		}
		if o.Text || want == o.Checked || !cur.Multi && !want {
			continue
		}
		if cur, err = d.moveTo(cur, i); err != nil {
			return cur, err
		}
		if err := d.keys(false, "Space"); err != nil {
			return cur, err
		}
		prompt := cur.Prompt
		next, ok := d.await(func(f detect.Form) bool {
			return f.Prompt == prompt && i < len(f.Options) && f.Options[i].Checked == want
		})
		if !ok {
			return cur, &AskError{"The choice did not stick; finish in the terminal"}
		}
		cur = next
	}
	if a.Text == "" {
		return d.leave(cur, "Tab")
	}
	if cur, err = d.moveTo(cur, other); err != nil {
		return cur, err
	}
	if cur, err = d.typeInto(cur, a.Text, 1); err != nil {
		return cur, err
	}
	// Enter takes the typed words as the answer; Tab would keep a radio's pick.
	return d.leave(cur, "Enter")
}

// dropdown steps a dropdown to the chosen option: Right to the end, then Left.
func (d formDriver) dropdown(cur detect.Form, a AskAnswer) (detect.Form, error) {
	if a.Text != "" || len(a.Labels) != 1 {
		return cur, &AskError{"Pick one answer"}
	}
	want := normalise(a.Labels[0])
	for _, key := range []string{"Right", "Left"} {
		for n := 0; n < maxFormMoves && !strings.EqualFold(normalise(cur.Value), want); n++ {
			if err := d.keys(false, key); err != nil {
				return cur, err
			}
			prev := cur.Value
			next, ok := d.await(func(f detect.Form) bool {
				return f.Stop == detect.FormSelect && f.Prompt == cur.Prompt && f.Value != prev
			})
			if !ok {
				break // the end of the list
			}
			cur = next
		}
	}
	if !strings.EqualFold(normalise(cur.Value), want) {
		return cur, &AskError{"The terminal does not offer " + a.Labels[0]}
	}
	return d.leave(cur, "Tab")
}

// text answers a text field or text area.
func (d formDriver) text(cur detect.Form, a AskAnswer) (detect.Form, error) {
	t := a.Text
	if t == "" && len(a.Labels) == 1 {
		t = a.Labels[0]
	}
	if t == "" || len(a.Labels) > 1 {
		return cur, &AskError{"This question takes typed words"}
	}
	if strings.IndexFunc(t, unicode.IsControl) >= 0 || len(t) > maxFormText {
		return cur, &AskError{"An answer like this can only be typed in the terminal"}
	}
	var err error
	if cur, err = d.typeInto(cur, t, cur.Rows); err != nil {
		return cur, err
	}
	return d.leave(cur, "Tab")
}

// typeInto types t into the focused input (an Other row or a text field)
// and waits for it to show. A field that holds other text is emptied
// first; a text area of several lines is not touched.
func (d formDriver) typeInto(cur detect.Form, t string, rows int) (detect.Form, error) {
	if shows(t, cur.Value) {
		return cur, nil
	}
	prompt, stop := cur.Prompt, cur.Stop
	here := func(f detect.Form) bool { return f.Stop == stop && f.Prompt == prompt }
	if cur.Value != "" {
		if rows > 1 {
			return cur, &AskError{"This field already holds text; finish in the terminal"}
		}
		if err := d.keys(false, "End", "C-u"); err != nil {
			return cur, err
		}
		old := cur.Value
		next, ok := d.await(func(f detect.Form) bool { return here(f) && f.Value != old })
		if !ok {
			return cur, &AskError{"The field could not be emptied; finish in the terminal"}
		}
		cur = next
	}
	if err := d.keys(true, t); err != nil {
		return cur, err
	}
	next, ok := d.await(func(f detect.Form) bool { return here(f) && shows(t, f.Value) })
	if !ok {
		// Never leave half an answer in the field.
		_ = d.keys(false, "End", "C-u")
		return cur, &AskError{"The typed answer did not show as sent; finish in the terminal"}
	}
	return next, nil
}

// shows reports that a field's visible text is t: all of it, or for text
// longer than the field the end of it (an input scrolls to its cursor; a
// text area wraps), spaces aside.
func shows(t, shown string) bool {
	full, vis := compact(t), compact(strings.TrimLeft(shown, "…"))
	if vis == "" {
		return full == ""
	}
	if len([]rune(full)) <= 60 {
		return full == vis
	}
	return strings.HasSuffix(full, vis) && len([]rune(vis)) >= 20
}

func compact(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
