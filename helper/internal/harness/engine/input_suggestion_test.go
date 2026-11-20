package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// fakeghost reads its input line as omp 19.0.1 draws it, with the
// predicted next message after the cursor.
func init() {
	harness.RegisterAdapterFile("fakeghost", []byte(`
adapter: fakeghost
name: Fake ghost
revision: 1
match: { agents: [fakeghost] }
capabilities: [chat, commands]
input:
  line: '^╰─(?: (.*?))?(?:\s{2,}⇧⇥ to change thinking effort)?\s*$'
  clear: [End, C-u]
  dismiss: [Escape]
  suggestion: true
actions:
  effort: { type: { text: '/effort {{.id}}', submit: true }, stability: documented-command }
`))
}

// The input line omp 19.0.1 drew after a turn (captured from tmux with
// colours): the prompt in the accent colour, then the prediction in the
// theme's muted colour, with the cursor right after "╰─ ".
const ghostLine = "\x1b[38;2;212;168;83m╰─ \x1b[38;2;106;103;96mread README.txt\x1b[39m"

// ghostPane plays omp 19's input line. The prediction shows while nothing
// is typed and no key removes it; typed text replaces it; C-u removes text
// before the cursor; Enter sends typed text and does nothing on an empty
// line. With cursorAtEnd the cursor is drawn after the text instead.
type ghostPane struct {
	mu          sync.Mutex
	ghost       string // what the line suggests while empty
	text        string // what was typed
	cursorAtEnd bool
	// ghostAfterEnter is shown again once a typed command is submitted, as
	// the prediction comes back when the line empties.
	ghostAfterEnter bool
	keys            []string
	submitted       []string
}

func (p *ghostPane) Type(_ context.Context, _, text string, submit, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.text += text
	if submit {
		p.enterLocked()
	}
	return nil
}

func (p *ghostPane) enterLocked() {
	if p.text == "" {
		return
	}
	p.submitted = append(p.submitted, p.text)
	p.text = ""
	if !p.ghostAfterEnter {
		p.ghost = ""
	}
}

func (p *ghostPane) Keys(_ context.Context, _ string, keys ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range keys {
		p.keys = append(p.keys, k)
		switch k {
		case "Enter":
			p.enterLocked()
		case "C-u":
			p.text = ""
		case "BSpace":
			if r := []rune(p.text); len(r) > 0 {
				p.text = string(r[:len(r)-1])
			}
		}
	}
	return nil
}

func (p *ghostPane) line(styled bool) string {
	if p.text != "" {
		return "╰─ " + p.text
	}
	if p.ghost == "" {
		return "╰─"
	}
	if styled {
		return strings.Replace(ghostLine, "read README.txt", p.ghost, 1)
	}
	return "╰─ " + p.ghost
}

func (p *ghostPane) screen(styled bool) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return " done\n π > ◒ DeepSeek V4 Flash 0423 > proj\n" + p.line(styled) + "\n"
}

func (p *ghostPane) Screen(context.Context, string) (string, error) { return p.screen(false), nil }

func (p *ghostPane) Styled(context.Context, string) (string, error) { return p.screen(true), nil }

func (p *ghostPane) Cursor(context.Context, string) (int, int, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	x := 3 + len([]rune(p.text))
	if p.cursorAtEnd && p.text == "" && p.ghost != "" {
		x = 3 + len([]rune(p.ghost))
	}
	return x, 2, true, nil
}

func newGhostEngine(t *testing.T, id string, p *ghostPane) *Engine {
	t.Helper()
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakeghost", Managed: true, Cwd: t.TempDir(), Command: []string{"fakeghost"}}},
		Typist:   p, Marker: &fakeMarker{}, Screen: p.Screen, StyledScreen: p.Styled, Cursor: p.Cursor, Home: t.TempDir(),
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.05,
	})
	t.Cleanup(func() { e.Forget(id) })
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return e
}

func say(e *Engine, id, text string) error {
	_, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: text, Mode: harness.SendNow})
	return err
}

func effort(e *Engine, id, level string) (harness.ActionResult, error) {
	arg, _ := json.Marshal(level)
	return e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "effort",
		Args: map[string]json.RawMessage{"id": arg}})
}

// Seen live on omp 19.0.1: every prompt from the phone after a turn failed
// with "omp's input line already holds text (read README.txt)".
func TestPromptGoesInOverOmpsPrediction(t *testing.T) {
	const id = "s_fakeghost1"
	p := &ghostPane{ghost: "read README.txt"}
	e := newGhostEngine(t, id, p)
	if err := say(e, id, "Reply with the single word: pear"); err != nil {
		t.Fatal(err)
	}
	if len(p.submitted) != 1 || p.submitted[0] != "Reply with the single word: pear" {
		t.Fatalf("submitted %q, want the prompt alone", p.submitted)
	}
}

// A draft is removed by End and C-u wherever its cursor is, so it is never
// taken for a prediction.
func TestDraftIsClearedNotTakenForAPrediction(t *testing.T) {
	const id = "s_fakeghost2"
	p := &ghostPane{ghost: "read README.txt", text: "half a thought"}
	e := newGhostEngine(t, id, p)
	if err := say(e, id, "next"); err != nil {
		t.Fatal(err)
	}
	if len(p.submitted) != 1 || p.submitted[0] != "next" {
		t.Fatalf("submitted %q, want the draft cleared first", p.submitted)
	}
	if !strings.HasPrefix(strings.Join(p.keys, " "), "End C-u") {
		t.Fatalf("keys %v, want End then C-u first", p.keys)
	}
}

// Text that stays through every clear but stands before the cursor is not
// a suggestion: nothing is typed after it.
func TestTextBeforeTheCursorThatStaysIsRefused(t *testing.T) {
	const id = "s_fakeghost3"
	p := &ghostPane{ghost: "read README.txt", cursorAtEnd: true}
	e := newGhostEngine(t, id, p)
	err := say(e, id, "next")
	if err == nil || !strings.Contains(err.Error(), "already holds text") {
		t.Fatalf("err = %v, want the send refused", err)
	}
	if len(p.submitted) != 0 {
		t.Fatalf("submitted %q, want nothing", p.submitted)
	}
}

// After a command the prediction can show again. Once known, it reads as
// an empty line, so the command counts as sent and Enter is not pressed
// again.
func TestPredictionAfterACommandCountsAsEmpty(t *testing.T) {
	const id = "s_fakeghost4"
	p := &ghostPane{ghost: "read README.txt", ghostAfterEnter: true}
	e := newGhostEngine(t, id, p)
	if res, err := effort(e, id, "high"); err != nil || res.Result == harness.ResultFailed {
		t.Fatalf("first = %+v, %v", res, err)
	}
	p.mu.Lock()
	p.keys = nil
	p.mu.Unlock()
	if res, err := effort(e, id, "xhigh"); err != nil || res.Result == harness.ResultFailed {
		t.Fatalf("second = %+v, %v", res, err)
	}
	enters := 0
	for _, k := range p.keys {
		if k == "Enter" {
			enters++
		}
	}
	if enters != 1 || len(p.submitted) != 2 {
		t.Fatalf("keys %v, submitted %q; want each command once and one Enter", p.keys, p.submitted)
	}
}
