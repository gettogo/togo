package approval

import (
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
)

// askRecordWait is how long a submitted form's answer may take to reach
// the agent's own record (the question item closes) before the answer is
// reported as not confirmed. It runs after askWait.
var askRecordWait = 5 * time.Second

func optionIndex(f detect.Form, label string) (int, bool) {
	for i, o := range f.Options {
		if !o.Text && normalise(o.Label) == normalise(label) {
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

// wanted reports the rows of cur's group the answer checks.
func wanted(cur detect.Form, a AskAnswer) []bool {
	want := make([]bool, len(cur.Options))
	for _, l := range a.Labels {
		if j, ok := optionIndex(cur, l); ok {
			want[j] = true
		}
	}
	if o := otherIndex(cur); o >= 0 {
		want[o] = a.Text != ""
	}
	return want
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
	want := wanted(cur, a)
	var err error
	for i := range cur.Options {
		o := cur.Options[i]
		if o.Text || want[i] == o.Checked || !cur.Multi && !want[i] {
			continue
		}
		if cur, err = d.moveTo(cur, i); err != nil {
			return cur, err
		}
		if err := d.press(at(cur), false, "Space"); err != nil {
			return cur, err
		}
		prompt := cur.Prompt
		next, ok := d.await(func(f detect.Form) bool {
			return f.Prompt == prompt && i < len(f.Options) && f.Options[i].Checked == want[i]
		})
		if !ok {
			return cur, &AskError{"The choice did not stick; finish in the terminal"}
		}
		cur = next
	}
	if other >= 0 && a.Text == "" && cur.Options[other].Checked {
		// Words typed into a check-box group's Other at the desk count as
		// an answer of their own: empty the row.
		if cur, err = d.moveTo(cur, other); err != nil {
			return cur, err
		}
		if cur, err = d.typeInto(cur, ""); err != nil {
			return cur, err
		}
	}
	if a.Text == "" {
		return d.leave(cur, "Tab")
	}
	if cur, err = d.moveTo(cur, other); err != nil {
		return cur, err
	}
	if cur, err = d.typeInto(cur, a.Text); err != nil {
		return cur, err
	}
	// Enter takes the typed words as the answer, dropping a radio's pick;
	// checkForm reads back that it did.
	return d.leave(cur, "Enter")
}

// fieldShows reports that the open field f shows exactly answer a.
func fieldShows(f detect.Form, a AskAnswer) bool {
	switch f.Stop {
	case detect.FormOption, detect.FormOther:
		want := wanted(f, a)
		for _, l := range a.Labels {
			if _, ok := optionIndex(f, l); !ok {
				return false
			}
		}
		for i, o := range f.Options {
			if o.Checked != want[i] {
				return false
			}
			if o.Text && want[i] {
				shown := o.Detail
				if i == f.Cursor {
					shown = shownText(f)
				}
				if shown != a.Text {
					return false
				}
			}
		}
		return true
	case detect.FormSelect:
		return len(a.Labels) == 1 && a.Text == "" && normalise(f.Value) == normalise(a.Labels[0])
	case detect.FormText:
		t, ok := textOf(a)
		if !ok || len(f.Values) == 0 || f.Values[0] != t {
			return false
		}
		for _, v := range f.Values[1:] {
			if strings.TrimSpace(v) != "" {
				return false
			}
		}
		return true
	}
	return false
}

// checkForm reads the whole form back before Submit: from its first
// field to Submit, every answered field must show exactly its answer, no
// field may be left out, and omp must show no field to fix. It ends with
// the focus on Submit.
func (d formDriver) checkForm(cur detect.Form, answers []AskAnswer) (detect.Form, error) {
	// To the first field: Shift+Tab until the focus stays.
	for n := 0; n < maxFormMoves; n++ {
		prev := cur
		if err := d.press(at(prev), false, "BTab"); err != nil {
			return cur, err
		}
		next, ok := d.await(func(f detect.Form) bool { return !same(f, prev) })
		if !ok {
			break
		}
		cur = next
	}
	seen := map[int]bool{}
	for n := 0; cur.Stop != detect.FormSubmit; n++ {
		if n >= maxFormMoves {
			return cur, &AskError{"The form could not be read back; finish in the terminal"}
		}
		for i, a := range answers {
			if a.Keep || !samePromptExact(cur.Prompt, a.Prompt) {
				continue
			}
			if !fieldShows(cur, a) {
				return cur, &AskError{"The terminal shows a different answer to " + a.Prompt + "; nothing was submitted. Finish in the terminal"}
			}
			seen[i] = true
		}
		var err error
		if cur, err = d.leave(cur, "Tab"); err != nil {
			return cur, err
		}
	}
	for i, a := range answers {
		if !a.Keep && !seen[i] {
			return cur, &AskError{"The form did not show " + a.Prompt + " on the way to Submit; nothing was submitted"}
		}
	}
	for _, l := range cur.Lines {
		if strings.HasPrefix(l, "! ") {
			return cur, &AskError{"omp marks " + strings.TrimSpace(l[2:]) + " to fix; nothing was submitted. Finish in the terminal"}
		}
	}
	if cur.Fix > 0 {
		return cur, &AskError{"omp marks a field to fix; nothing was submitted. Finish in the terminal"}
	}
	return cur, nil
}

// formGone waits for the submitted form to be taken: its frame absent on
// two reads in a row and, with open, the question closed by the agent's
// record of the answer. A frame that stays with a field to fix is omp
// refusing the answers, with its message.
func (d formDriver) formGone(open func() bool) error {
	deadline := time.Now().Add(askWait)
	recorded := deadline.Add(askRecordWait)
	absent, gone := 0, false
	for {
		screen, err := d.s.cfg.Tmux.CapturePane(d.ctx, d.target)
		switch {
		case err != nil:
			absent = 0
		case detect.FormOnScreen(screen):
			absent = 0
			if f, ok := detect.ParseForm(screen); ok && (f.Fix > 0 || f.Error != "") {
				msg := "omp did not take the answers"
				if f.Error != "" {
					msg += ": " + f.Prompt + ": " + f.Error
				}
				return &AskError{msg + ". Finish in the terminal"}
			}
		default:
			absent++
		}
		gone = gone || absent >= 2
		if gone && (open == nil || !open()) {
			return nil
		}
		now := time.Now()
		if !gone && now.After(deadline) {
			return &AskError{"The form is still open; check the terminal"}
		}
		if now.After(recorded) || d.ctx.Err() != nil {
			return &AskError{"The form closed, but omp has not recorded the answer yet; check the terminal"}
		}
		time.Sleep(askPoll)
	}
}
