package approval

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
)

// AnswerChoice answers a selection dialog (detect.ParseChoice: a cursor on
// radio or check-box rows, an Other row that takes words, questions in
// turn with a Review page) with keys, one question at a time, reading the
// screen back after every step: the cursor is moved to the chosen row and
// Enter pressed; a multi-select's rows are toggled with Space, then Enter
// moves on; a typed answer picks the Other row, types into the field and
// presses Enter; a Review page is submitted with Enter. It stops at the
// first mismatch and says so; nothing is sent once the screen shows
// something else. The pane lock is held throughout, so the prompt watcher
// and other answers wait. omp's ask tool (18.16) is driven this way; any
// agent whose dialog ParseChoice reads is too.
func (s *Store) AnswerChoice(ctx context.Context, sessionID string, answers []AskAnswer) error {
	for _, a := range answers {
		if strings.ContainsAny(a.Text, "\n\r") {
			return &AskError{"A typed answer with a line break can only be sent from the terminal"}
		}
		if a.Multi && a.Text != "" {
			return &AskError{"Type that answer in the terminal: a typed answer to a pick-any question can't be sent"}
		}
	}
	mu := s.lockFor(sessionID)
	mu.Lock()
	defer mu.Unlock()
	d := choiceDriver{s: s, ctx: ctx, target: target(sessionID)}
	for i, a := range answers {
		cur, ok := d.settled(func(c detect.Choice) bool { return !c.Review && samePrompt(c.Prompt, a.Prompt) })
		if !ok {
			if i == 0 {
				return &AskError{"The question is not waiting at the terminal"}
			}
			return &AskError{"The terminal shows a different question; stopped there"}
		}
		if err := d.answer(cur, a); err != nil {
			return err
		}
	}
	// A batch, or a multi-select, ends on a Review page: submit it. One
	// single-choice question closed on its Enter.
	deadline := time.Now().Add(askWait)
	for {
		c, found := d.read()
		switch {
		case found && c.Review:
			if err := d.keys(false, "Enter"); err != nil {
				return err
			}
			return d.gone()
		case !found && !d.waiting():
			return nil
		}
		if time.Now().After(deadline) || d.ctx.Err() != nil {
			return &AskError{"The dialog is still open; check the terminal"}
		}
		time.Sleep(askPoll)
	}
}

// samePrompt matches the question's prompt as the screen draws it, which
// may be its first line when the pane wrapped it.
func samePrompt(screen, prompt string) bool {
	a, b := strings.ToLower(strings.Join(strings.Fields(screen), " ")), strings.ToLower(strings.Join(strings.Fields(prompt), " "))
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasPrefix(b, a) || strings.HasPrefix(a, b)
}

type choiceDriver struct {
	s      *Store
	ctx    context.Context
	target string
}

func (d choiceDriver) read() (detect.Choice, bool) {
	screen, err := d.s.cfg.Tmux.CapturePane(d.ctx, d.target)
	if err != nil {
		return detect.Choice{}, false
	}
	return detect.ParseChoice(screen)
}

// waiting reports a dialog with nothing to pick: the text field.
func (d choiceDriver) waiting() bool {
	screen, err := d.s.cfg.Tmux.CapturePane(d.ctx, d.target)
	if err != nil {
		return false
	}
	p, ok := detect.Detect(screen)
	return ok && p.Kind == detect.KindWaiting
}

func (d choiceDriver) await(ok func(detect.Choice) bool) (detect.Choice, bool) {
	deadline := time.Now().Add(askWait)
	for {
		if c, found := d.read(); found && ok(c) {
			return c, true
		}
		if time.Now().After(deadline) || d.ctx.Err() != nil {
			return detect.Choice{}, false
		}
		time.Sleep(askPoll)
	}
}

// settled waits for the question, lets it stand for askSettle so the
// agent takes keys, and reads it again: still that question, as it is now.
func (d choiceDriver) settled(ok func(detect.Choice) bool) (detect.Choice, bool) {
	if _, found := d.await(ok); !found {
		return detect.Choice{}, false
	}
	if askSettle > 0 {
		select {
		case <-d.ctx.Done():
			return detect.Choice{}, false
		case <-time.After(askSettle):
		}
	}
	return d.await(ok)
}

func (d choiceDriver) keys(literal bool, keys ...string) error {
	if err := d.s.cfg.Tmux.SendKeys(d.ctx, d.target, literal, keys...); err != nil {
		return &AskError{"Sending keys to the terminal failed"}
	}
	return nil
}

// moveTo puts the cursor on row idx of the dialog that shows prompt, one
// arrow key at a time, and reads it back there.
func (d choiceDriver) moveTo(cur detect.Choice, idx int) (detect.Choice, error) {
	for cur.Cursor != idx {
		key := "Down"
		if idx < cur.Cursor {
			key = "Up"
		}
		if err := d.keys(false, key); err != nil {
			return cur, err
		}
		want := cur.Cursor + 1
		if key == "Up" {
			want = cur.Cursor - 1
		}
		next, ok := d.await(func(c detect.Choice) bool { return samePrompt(c.Prompt, cur.Prompt) && c.Cursor == want })
		if !ok {
			return cur, &AskError{"The cursor did not move; finish in the terminal"}
		}
		cur = next
	}
	return cur, nil
}

func (d choiceDriver) answer(cur detect.Choice, a AskAnswer) error {
	if a.Text != "" {
		if cur.TextRow < 0 {
			return &AskError{"This question takes no typed answer"}
		}
		switch {
		case cur.Digits:
			// Kimi: the row's digit opens its field in place.
			if !cur.Typing {
				if err := d.keys(false, digit(cur.TextRow)); err != nil {
					return err
				}
				if _, ok := d.await(func(c detect.Choice) bool { return c.Typing && samePrompt(c.Prompt, cur.Prompt) }); !ok {
					return &AskError{"The text field did not open; finish in the terminal"}
				}
			}
		default:
			next, err := d.moveTo(cur, cur.TextRow)
			if err != nil {
				return err
			}
			cur = next
			if !cur.InPlace {
				if err := d.keys(false, "Enter"); err != nil {
					return err
				}
				if !d.awaitWaiting() {
					return &AskError{"The text field did not open; finish in the terminal"}
				}
			}
		}
		if err := d.keys(true, a.Text); err != nil {
			return err
		}
		return d.moved(cur, "Enter")
	}
	if !a.Multi {
		if len(a.Labels) != 1 {
			return &AskError{"Pick one answer"}
		}
		idx, ok := cur.OptionIndex(a.Labels[0])
		if !ok {
			return &AskError{"The terminal does not offer " + a.Labels[0]}
		}
		switch {
		case cur.Digits:
			return d.moved(cur, digit(idx))
		case cur.Toggle:
			// Two buttons the capture shows no highlight on: from the
			// agent's default, Left flips to the other one.
			if idx != cur.Cursor {
				if err := d.keys(false, "Left"); err != nil {
					return err
				}
			}
			return d.moved(cur, "Enter")
		}
		cur, err := d.moveTo(cur, idx)
		if err != nil {
			return err
		}
		return d.moved(cur, "Enter")
	}
	for _, l := range a.Labels {
		if _, ok := cur.OptionIndex(l); !ok {
			return &AskError{"The terminal does not offer " + l}
		}
	}
	for i := range cur.Options {
		o := cur.Options[i]
		if o.Text {
			continue
		}
		if want := slices.ContainsFunc(a.Labels, func(l string) bool { j, ok := cur.OptionIndex(l); return ok && j == i }); want == o.Checked {
			continue
		}
		if cur.Digits {
			if err := d.keys(false, digit(i)); err != nil {
				return err
			}
		} else {
			next, err := d.moveTo(cur, i)
			if err != nil {
				return err
			}
			cur = next
			if err := d.keys(false, "Space"); err != nil {
				return err
			}
		}
		next, ok := d.await(func(c detect.Choice) bool {
			return samePrompt(c.Prompt, cur.Prompt) && len(c.Options) == len(cur.Options) && c.Options[i].Checked != o.Checked
		})
		if !ok {
			return &AskError{"The choice did not stick; finish in the terminal"}
		}
		cur = next
	}
	done := cur.Next
	if done == "" {
		done = "Enter"
	}
	return d.moved(cur, done)
}

// digit is the key that picks row i of a numbered dialog.
func digit(i int) string { return strconv.Itoa(i + 1) }

// awaitWaiting waits for the text field to open.
func (d choiceDriver) awaitWaiting() bool {
	deadline := time.Now().Add(askWait)
	for {
		if d.waiting() {
			return true
		}
		if time.Now().After(deadline) || d.ctx.Err() != nil {
			return false
		}
		time.Sleep(askPoll)
	}
}

// moved sends key and waits for the dialog to leave cur's question.
func (d choiceDriver) moved(cur detect.Choice, key string) error {
	if err := d.keys(false, key); err != nil {
		return err
	}
	deadline := time.Now().Add(askWait)
	for {
		c, found := d.read()
		if !found || c.Review || !samePrompt(c.Prompt, cur.Prompt) {
			return nil
		}
		if time.Now().After(deadline) || d.ctx.Err() != nil {
			return &AskError{"The terminal did not move on from " + cur.Prompt}
		}
		time.Sleep(askPoll)
	}
}

// gone waits for the dialog to close after the last step.
func (d choiceDriver) gone() error {
	deadline := time.Now().Add(askWait)
	for {
		if _, found := d.read(); !found && !d.waiting() {
			return nil
		}
		if time.Now().After(deadline) || d.ctx.Err() != nil {
			return &AskError{"The dialog is still open; check the terminal"}
		}
		time.Sleep(askPoll)
	}
}
