package approval

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/tmux"
)

// askSim draws Claude Code's question dialog the way 2.1.319 does (see
// detect/testdata/claude/ask-*.txt) and reacts to keys like it: a digit
// picks a single-select option and moves on, toggles a multi-select box,
// or focuses Type something; Tab moves on; on the review, Enter or 1
// submits.
type askSim struct {
	mu      sync.Mutex
	qs      []simQ
	tab     int
	typing  bool
	buf     string
	answers map[int]string
	checked map[int]map[string]bool
	closed  bool
	sent    [][]string
}

type simQ struct {
	prompt string
	multi  bool
	opts   []string
}

func newSim(qs ...simQ) *askSim {
	return &askSim{qs: qs, answers: map[int]string{}, checked: map[int]map[string]bool{}}
}

func (a *askSim) ListSessions(context.Context) ([]tmux.SessionInfo, error) { return nil, nil }

func (a *askSim) CapturePane(context.Context, string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return "⏺ User answered Claude's questions\n❯ \n", nil
	}
	var b strings.Builder
	b.WriteString(strings.Repeat("─", 40) + "\n")
	if len(a.qs) > 1 || a.qs[0].multi {
		b.WriteString("←  ")
		for i, q := range a.qs {
			mark := "☐"
			if _, ok := a.answers[i]; ok || len(a.checked[i]) > 0 {
				mark = "☒"
			}
			fmt.Fprintf(&b, "%s Q%d  ", mark, i+1)
			_ = q
		}
		b.WriteString("✔ Submit  →\n\n")
	}
	if a.tab >= len(a.qs) {
		b.WriteString("Review your answers\n\nReady to submit your answers?\n\n❯ 1. Submit answers\n  2. Cancel\n")
		return b.String(), nil
	}
	q := a.qs[a.tab]
	b.WriteString("\n" + q.prompt + "\n\n")
	for i, o := range q.opts {
		cur := "  "
		if i == 0 && !a.typing {
			cur = "❯ "
		}
		box := ""
		if q.multi {
			box = "[ ] "
			if a.checked[a.tab][o] {
				box = "[✔] "
			}
		}
		fmt.Fprintf(&b, "%s%d. %s%s\n     about %s\n", cur, i+1, box, o, o)
	}
	typed := "Type something."
	if a.typing {
		typed = a.buf
	}
	fmt.Fprintf(&b, "  %d. %s\n%s\n  %d. Chat about this\n\nEnter to select · Tab/Arrow keys to navigate · Esc to cancel\n",
		len(q.opts)+1, typed, strings.Repeat("─", 40), len(q.opts)+2)
	return b.String(), nil
}

func (a *askSim) PasteText(context.Context, string, string) error { return nil }

func (a *askSim) SendKeys(_ context.Context, _ string, literal bool, keys ...string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, append([]string{fmt.Sprint(literal)}, keys...))
	for _, k := range keys {
		if literal {
			a.buf += k
			continue
		}
		if a.tab >= len(a.qs) {
			if k == "Enter" || k == "1" {
				a.closed = true
			}
			continue
		}
		q := a.qs[a.tab]
		n, isDigit := strconv.Atoi(k)
		switch {
		case k == "Tab":
			a.tab++
		case k == "Enter" && a.typing:
			a.answers[a.tab], a.typing, a.buf = a.buf, false, ""
			a.next()
		case isDigit == nil && n == len(q.opts)+1:
			a.typing = true
		case isDigit == nil && n >= 1 && n <= len(q.opts) && q.multi:
			if a.checked[a.tab] == nil {
				a.checked[a.tab] = map[string]bool{}
			}
			a.checked[a.tab][q.opts[n-1]] = !a.checked[a.tab][q.opts[n-1]]
		case isDigit == nil && n >= 1 && n <= len(q.opts):
			a.answers[a.tab] = q.opts[n-1]
			a.next()
		}
	}
	return nil
}

// next moves on after a single-select answer; one plain question submits.
func (a *askSim) next() {
	if len(a.qs) == 1 && !a.qs[0].multi {
		a.closed = true
		return
	}
	a.tab++
}

func fastAsk(t *testing.T) {
	poll, wait := askPoll, askWait
	askPoll, askWait = time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { askPoll, askWait = poll, wait })
}

func TestAnswerAskTwoQuestionsWithMultiSelectAndText(t *testing.T) {
	fastAsk(t)
	sim := newSim(simQ{"Which color?", false, []string{"Red", "Blue"}}, simQ{"Which fruits (pick any)", true, []string{"Apple", "Pear", "Plum"}},
		simQ{"Anything else?", false, []string{"No"}})
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerAsk(context.Background(), sess, []AskAnswer{
		{Prompt: "Which color?", Labels: []string{"Blue"}},
		{Prompt: "Which fruits (pick any)", Multi: true, Labels: []string{"Apple", "Plum"}},
		{Prompt: "Anything else?", Text: "Ship it"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sim.closed || sim.answers[0] != "Blue" || sim.answers[2] != "Ship it" ||
		!reflect.DeepEqual(sim.checked[1], map[string]bool{"Apple": true, "Plum": true}) {
		t.Fatalf("sim = %+v", sim)
	}
	want := [][]string{{"false", "2"}, {"false", "1"}, {"false", "3"}, {"false", "Tab"}, {"false", "2"}, {"true", "Ship it"}, {"false", "Enter"}, {"false", "Enter"}}
	if !reflect.DeepEqual(sim.sent, want) {
		t.Fatalf("keys %v\nwant %v", sim.sent, want)
	}
}

func TestAnswerAskSingleQuestionSubmitsOnTheDigit(t *testing.T) {
	fastAsk(t)
	sim := newSim(simQ{"Which color?", false, []string{"Red", "Blue"}})
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	if err := st.AnswerAsk(context.Background(), sess, []AskAnswer{{Prompt: "Which color?", Labels: []string{"Red"}}}); err != nil {
		t.Fatal(err)
	}
	if !sim.closed || len(sim.sent) != 1 {
		t.Fatalf("sim %+v", sim)
	}
}

func TestAnswerAskStopsOnAMismatch(t *testing.T) {
	fastAsk(t)
	sim := newSim(simQ{"Which color?", false, []string{"Red", "Blue"}}, simQ{"Something new?", false, []string{"Yes"}})
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerAsk(context.Background(), sess, []AskAnswer{
		{Prompt: "Which color?", Labels: []string{"Red"}},
		{Prompt: "Which size?", Labels: []string{"L"}},
	})
	var ae *AskError
	if !asAskError(err, &ae) || !strings.Contains(ae.Msg, "different question") {
		t.Fatalf("err = %v", err)
	}
	if len(sim.sent) != 1 || sim.closed {
		t.Fatalf("kept typing after the mismatch: %v", sim.sent)
	}
	// Nothing at all is sent when the first question is not on screen.
	sim2 := newSim(simQ{"Other?", false, []string{"A"}})
	err = New(Config{Tmux: sim2, Pub: &fakePub{}}).AnswerAsk(context.Background(), sess, []AskAnswer{{Prompt: "Which color?", Labels: []string{"Red"}}})
	if !asAskError(err, &ae) || len(sim2.sent) != 0 {
		t.Fatalf("err %v sent %v", err, sim2.sent)
	}
}

func TestAnswerAskRefusesNewlinesAndTypedPickAny(t *testing.T) {
	sim := newSim(simQ{"Q?", false, []string{"A"}})
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	for _, a := range []AskAnswer{{Prompt: "Q?", Text: "two\nlines"}, {Prompt: "Q?", Multi: true, Text: "x"}} {
		var ae *AskError
		if err := st.AnswerAsk(context.Background(), sess, []AskAnswer{a}); !asAskError(err, &ae) || ae.Msg == "" {
			t.Errorf("%+v: err %v", a, err)
		}
	}
	if len(sim.sent) != 0 || slices.Contains([]bool{sim.closed}, true) {
		t.Fatalf("keys were sent: %v", sim.sent)
	}
}

func asAskError(err error, target **AskError) bool {
	e, ok := err.(*AskError)
	if ok {
		*target = e
	}
	return ok
}
