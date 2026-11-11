package approval

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/tmux"
)

// choiceSim draws omp 18.16's ask dialog (see detect/testdata/omp/real-ask-*)
// and reacts to keys like it: Up and Down move the cursor, Space toggles a
// check box, Enter picks (one single-choice question closes at once; in a
// batch it moves on), Enter on Other opens the custom-answer field, which
// Enter submits; the Review page's Enter closes the dialog.
type choiceSim struct {
	mu      sync.Mutex
	qs      []simQ
	q       int
	cursor  int
	typing  bool
	buf     string
	picked  map[int][]string
	text    map[int]string
	checked map[int]map[string]bool
	closed  bool
	sent    [][]string
}

func newChoiceSim(qs ...simQ) *choiceSim {
	return &choiceSim{qs: qs, picked: map[int][]string{}, text: map[int]string{}, checked: map[int]map[string]bool{}}
}

func (c *choiceSim) ListSessions(context.Context) ([]tmux.SessionInfo, error) { return nil, nil }
func (c *choiceSim) PasteText(context.Context, string, string) error          { return nil }

const simWidth = 70

func boxLine(s string) string {
	pad := simWidth - len([]rune(s)) - 3
	if pad < 0 {
		pad = 0
	}
	return "│ " + s + strings.Repeat(" ", pad) + "│\n"
}

func (c *choiceSim) CapturePane(context.Context, string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	b.WriteString(" π > ◑ Model > 🗑 code/demo > ⑂ main\n")
	if c.closed {
		b.WriteString(" Thanks, noted.\n\n π > ◑ Model > 🗑 code/demo > ⑂ main > S0.15 ▶─2%──\n╰─                     ⇧⇥ to change thinking effort\n")
		return b.String(), nil
	}
	if c.typing {
		b.WriteString("╭─ Custom answer: " + c.qs[c.q].prompt + " " + strings.Repeat("─", 20) + "╮\n")
		b.WriteString(boxLine(""))
		b.WriteString(boxLine("> " + c.buf))
		b.WriteString(boxLine(""))
		b.WriteString(boxLine("⏎ or ⌃Q submit  ⎋ cancel  ⌃G external editor"))
		b.WriteString("╰" + strings.Repeat("─", simWidth-2) + "╯\n")
		return b.String(), nil
	}
	b.WriteString("╭─ Ask " + strings.Repeat("─", simWidth-8) + "╮\n")
	if c.q >= len(c.qs) {
		b.WriteString(boxLine("Review answers"))
		b.WriteString("├" + strings.Repeat("─", simWidth-2) + "┤\n")
		for i, q := range c.qs {
			b.WriteString(boxLine(fmt.Sprintf("%d. %s: %s", i+1, q.prompt, strings.Join(c.picked[i], ", "))))
		}
		b.WriteString(boxLine(""))
		b.WriteString(boxLine("❯ Submit"))
		b.WriteString("├" + strings.Repeat("─", simWidth-2) + "┤\n")
		b.WriteString(boxLine("⏎ submit · ↑/↓ scroll · ⎋ cancel · ⌥H hide"))
		b.WriteString("╰" + strings.Repeat("─", simWidth-2) + "╯\n")
		return b.String(), nil
	}
	q := c.qs[c.q]
	if len(c.qs) > 1 {
		var tabs []string
		for i := range c.qs {
			tabs = append(tabs, fmt.Sprintf("Q%d", i+1))
		}
		b.WriteString(boxLine(" " + strings.Join(append(tabs, "Submit"), "    ")))
	}
	b.WriteString(boxLine(q.prompt))
	b.WriteString("├" + strings.Repeat("─", simWidth-2) + "┤\n")
	rows := append(append([]string(nil), q.opts...), "Other (type your own)")
	for i, o := range rows {
		cur := "  "
		if i == c.cursor {
			cur = "❯ "
		}
		mark := "○"
		if q.multi {
			mark = "☐"
			if c.checked[c.q][o] {
				mark = "☑"
			}
		}
		b.WriteString(boxLine(fmt.Sprintf("%s%s %s", cur, mark, o)))
		if i < len(q.opts) {
			b.WriteString(boxLine("      About " + o + "."))
		}
	}
	b.WriteString("├" + strings.Repeat("─", simWidth-2) + "┤\n")
	if q.multi {
		b.WriteString(boxLine("␣ toggle · ⏎ next · ↑/↓ move · ⇥/←/→ question · ⎋ cancel · ⌥H hide"))
	} else {
		b.WriteString(boxLine("⏎ select · n note · ↑/↓ move · ⎋ cancel · ⌥H hide"))
	}
	b.WriteString("╰" + strings.Repeat("─", simWidth-2) + "╯\n")
	return b.String(), nil
}

func (c *choiceSim) SendKeys(_ context.Context, _ string, literal bool, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, append([]string{fmt.Sprint(literal)}, keys...))
	for _, k := range keys {
		if literal {
			c.buf += k
			continue
		}
		if c.closed {
			continue
		}
		if c.q >= len(c.qs) {
			if k == "Enter" {
				c.closed = true
			}
			continue
		}
		q := c.qs[c.q]
		other := len(q.opts)
		switch {
		case c.typing && k == "Enter":
			c.text[c.q], c.picked[c.q] = c.buf, []string{"“" + c.buf + "”"}
			c.typing, c.buf = false, ""
			c.next()
		case k == "Down" && c.cursor < other:
			c.cursor++
		case k == "Up" && c.cursor > 0:
			c.cursor--
		case k == "Space" && q.multi && c.cursor < other:
			if c.checked[c.q] == nil {
				c.checked[c.q] = map[string]bool{}
			}
			c.checked[c.q][q.opts[c.cursor]] = !c.checked[c.q][q.opts[c.cursor]]
		case k == "Enter" && c.cursor == other:
			c.typing = true
		case k == "Enter" && q.multi:
			for _, o := range q.opts {
				if c.checked[c.q][o] {
					c.picked[c.q] = append(c.picked[c.q], o)
				}
			}
			c.next()
		case k == "Enter":
			c.picked[c.q] = []string{q.opts[c.cursor]}
			c.next()
		}
	}
	return nil
}

// next moves on; one single-choice question closes at once, anything
// else ends on the Review page.
func (c *choiceSim) next() {
	c.cursor = 0
	if len(c.qs) == 1 && !c.qs[0].multi {
		c.closed = true
		return
	}
	c.q++
}

func TestAnswerChoiceSingleQuestionMovesAndSelects(t *testing.T) {
	fastAsk(t)
	sim := newChoiceSim(simQ{"What should I work on next?", false, []string{"Bug fixes", "New feature", "Refactoring"}})
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	if err := st.AnswerChoice(context.Background(), sess, []AskAnswer{{Prompt: "What should I work on next?", Labels: []string{"Refactoring"}}}); err != nil {
		t.Fatal(err)
	}
	if !sim.closed || !reflect.DeepEqual(sim.picked[0], []string{"Refactoring"}) {
		t.Fatalf("sim %+v", sim)
	}
	want := [][]string{{"false", "Down"}, {"false", "Down"}, {"false", "Enter"}}
	if !reflect.DeepEqual(sim.sent, want) {
		t.Fatalf("keys %v\nwant %v", sim.sent, want)
	}
}

func TestAnswerChoiceBatchWithMultiSelectAndTypedOther(t *testing.T) {
	fastAsk(t)
	sim := newChoiceSim(
		simQ{"Which areas should the new feature touch?", true, []string{"Backend", "Frontend", "Database"}},
		simQ{"How should the feature ship?", false, []string{"Feature flag", "Direct release"}},
	)
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{
		{Prompt: "Which areas should the new feature touch?", Multi: true, Labels: []string{"Backend", "Database"}},
		{Prompt: "How should the feature ship?", Text: "Canary to 5 percent first"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sim.closed || !reflect.DeepEqual(sim.checked[0], map[string]bool{"Backend": true, "Database": true}) || sim.text[1] != "Canary to 5 percent first" {
		t.Fatalf("sim picked=%v checked=%v text=%v closed=%v", sim.picked, sim.checked, sim.text, sim.closed)
	}
	want := [][]string{
		{"false", "Space"}, {"false", "Down"}, {"false", "Down"}, {"false", "Space"}, {"false", "Enter"}, // Backend, then Database, then next
		{"false", "Down"}, {"false", "Down"}, {"false", "Enter"}, {"true", "Canary to 5 percent first"}, {"false", "Enter"}, // Other, type, submit
		{"false", "Enter"}, // Review: Submit
	}
	if !reflect.DeepEqual(sim.sent, want) {
		t.Fatalf("keys %v\nwant %v", sim.sent, want)
	}
}

func TestAnswerChoiceRefusesWhatTheScreenDoesNotOffer(t *testing.T) {
	fastAsk(t)
	sim := newChoiceSim(simQ{"Which color?", false, []string{"Red", "Blue"}})
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{{Prompt: "Which color?", Labels: []string{"Green"}}})
	var ae *AskError
	if !asAskError(err, &ae) || !strings.Contains(ae.Msg, "does not offer") || len(sim.sent) != 0 {
		t.Fatalf("err = %v, sent %v", err, sim.sent)
	}
	err = st.AnswerChoice(context.Background(), sess, []AskAnswer{{Prompt: "Which size?", Labels: []string{"L"}}})
	if !asAskError(err, &ae) || !strings.Contains(ae.Msg, "not waiting") || len(sim.sent) != 0 {
		t.Fatalf("err = %v, sent %v", err, sim.sent)
	}
}

// deafDialog shows Vibe 3.1's real ask dialog (detect/testdata/vibe/
// real-ask-single.txt) and, like Vibe, drops the keys sent within deaf of
// drawing it; its digit 2 picks SQLite and closes it.
type deafDialog struct {
	mu     sync.Mutex
	dialog string
	drawn  time.Time
	deaf   time.Duration
	closed bool
	sent   []string
}

func (c *deafDialog) ListSessions(context.Context) ([]tmux.SessionInfo, error) { return nil, nil }
func (c *deafDialog) PasteText(context.Context, string, string) error          { return nil }

func (c *deafDialog) CapturePane(context.Context, string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return "✓ Answered \"Which database should we use?\" → SQLite\n> \n", nil
	}
	return c.dialog, nil
}

func (c *deafDialog) SendKeys(_ context.Context, _ string, _ bool, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, keys...)
	if time.Since(c.drawn) >= c.deaf && len(keys) == 1 && keys[0] == "2" {
		c.closed = true
	}
	return nil
}

func TestAnswerChoiceWaitsForAFreshDialogToTakeKeys(t *testing.T) {
	fastAsk(t)
	askSettle = 80 * time.Millisecond
	raw, err := os.ReadFile("../detect/testdata/vibe/real-ask-single.txt")
	if err != nil {
		t.Fatal(err)
	}
	sim := &deafDialog{dialog: string(raw), drawn: time.Now(), deaf: 40 * time.Millisecond}
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	if err := st.AnswerChoice(context.Background(), sess, []AskAnswer{{Prompt: "Which database should we use?", Labels: []string{"SQLite"}}}); err != nil {
		t.Fatalf("%v (keys %v)", err, sim.sent)
	}
	if !sim.closed || !reflect.DeepEqual(sim.sent, []string{"2"}) {
		t.Fatalf("closed %v, keys %v", sim.closed, sim.sent)
	}
}
