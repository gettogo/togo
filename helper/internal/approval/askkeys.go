package approval

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
)

// AskAnswer is the answer to one question of Claude Code's question
// dialog: the labels chosen (one for a single-select), or typed text.
type AskAnswer struct {
	Prompt string
	Multi  bool
	Labels []string
	Text   string
}

// AskError says why the key driver stopped; Msg is shown to the user.
type AskError struct{ Msg string }

func (e *AskError) Error() string { return e.Msg }

// Waits of the key driver: how often the pane is read back and how long a
// step may take to show on screen. Tests shrink them.
var (
	askPoll = 100 * time.Millisecond
	askWait = 3 * time.Second
)

// AnswerAsk answers Claude Code's question dialog with keys, one question
// at a time, reading the screen back after every step: the option's digit
// (a single-select moves on by itself), digits that toggle a multi-select's
// boxes and then Tab, or Type something, the text and Enter; then Submit
// answers on the review. It stops at the first mismatch and says so;
// nothing is sent once the screen shows something else. The pane lock is
// held throughout, so the prompt watcher and other answers wait.
func (s *Store) AnswerAsk(ctx context.Context, sessionID string, answers []AskAnswer) error {
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
	d := askDriver{s: s, ctx: ctx, target: target(sessionID)}
	var last detect.Ask
	for i, a := range answers {
		cur, ok := d.await(func(x detect.Ask) bool { return !x.Review && x.Prompt == a.Prompt })
		if !ok {
			if i == 0 {
				return &AskError{"The question is not waiting at the terminal"}
			}
			return &AskError{"The terminal shows a different question; stopped there"}
		}
		if err := d.answer(cur, a); err != nil {
			return err
		}
		last = cur
	}
	if len(last.Tabs) == 0 && !last.Multi {
		// One single-select question: its digit submitted it.
		return d.gone()
	}
	review, ok := d.await(func(x detect.Ask) bool { return x.Review })
	if !ok {
		return &AskError{"The answers did not reach the Submit step; finish in the terminal"}
	}
	key := strconv.Itoa(review.SubmitN)
	if review.Cursor == review.SubmitN {
		key = "Enter"
	}
	if err := d.keys(false, key); err != nil {
		return err
	}
	return d.gone()
}

type askDriver struct {
	s      *Store
	ctx    context.Context
	target string
}

func (d askDriver) read() (detect.Ask, bool) {
	screen, err := d.s.cfg.Tmux.CapturePane(d.ctx, d.target)
	if err != nil {
		return detect.Ask{}, false
	}
	return detect.ParseAsk(screen)
}

// await reads the pane until ok holds for the dialog on it.
func (d askDriver) await(ok func(detect.Ask) bool) (detect.Ask, bool) {
	deadline := time.Now().Add(askWait)
	for {
		if a, found := d.read(); found && ok(a) {
			return a, true
		}
		if time.Now().After(deadline) || d.ctx.Err() != nil {
			return detect.Ask{}, false
		}
		time.Sleep(askPoll)
	}
}

func (d askDriver) keys(literal bool, keys ...string) error {
	if err := d.s.cfg.Tmux.SendKeys(d.ctx, d.target, literal, keys...); err != nil {
		return &AskError{"Sending keys to the terminal failed"}
	}
	return nil
}

func (d askDriver) answer(cur detect.Ask, a AskAnswer) error {
	if a.Text != "" {
		if cur.TypeN == 0 {
			return &AskError{"This question takes no typed answer"}
		}
		if err := d.keys(false, strconv.Itoa(cur.TypeN)); err != nil {
			return err
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
		n, ok := cur.OptionN(a.Labels[0])
		if !ok {
			return &AskError{"The terminal does not offer " + a.Labels[0]}
		}
		return d.moved(cur, strconv.Itoa(n))
	}
	for _, o := range cur.Options {
		if want := slices.Contains(a.Labels, o.Label); want != o.Checked {
			if err := d.keys(false, strconv.Itoa(o.N)); err != nil {
				return err
			}
		}
	}
	for _, l := range a.Labels {
		if _, ok := cur.OptionN(l); !ok {
			return &AskError{"The terminal does not offer " + l}
		}
	}
	if _, ok := d.await(func(x detect.Ask) bool { return x.Prompt == cur.Prompt && checks(x, a.Labels) }); !ok {
		return &AskError{"The choices did not stick; finish in the terminal"}
	}
	return d.moved(cur, "Tab")
}

// moved sends key and waits for the dialog to leave cur's question.
func (d askDriver) moved(cur detect.Ask, key string) error {
	if err := d.keys(false, key); err != nil {
		return err
	}
	deadline := time.Now().Add(askWait)
	for {
		a, found := d.read()
		if !found || a.Review || a.Prompt != cur.Prompt {
			return nil
		}
		if time.Now().After(deadline) || d.ctx.Err() != nil {
			return &AskError{"The terminal did not move on from " + cur.Prompt}
		}
		time.Sleep(askPoll)
	}
}

// gone waits for the dialog to close after the last step.
func (d askDriver) gone() error {
	deadline := time.Now().Add(askWait)
	for {
		if _, found := d.read(); !found {
			return nil
		}
		if time.Now().After(deadline) || d.ctx.Err() != nil {
			return &AskError{"The dialog is still open; check the terminal"}
		}
		time.Sleep(askPoll)
	}
}

func checks(a detect.Ask, labels []string) bool {
	for _, o := range a.Options {
		if o.Checked != slices.Contains(labels, o.Label) {
			return false
		}
	}
	return true
}
