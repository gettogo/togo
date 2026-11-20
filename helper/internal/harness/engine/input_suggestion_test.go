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
  restart: { relaunch: { argv: [fakeghost, --resume, '{{.id}}'] }, stability: documented }
`))
}

// The input line omp 19.0.1 drew after a turn (captured from tmux with
// colours): the prompt in the accent colour, then the prediction in the
// theme's muted colour, with the cursor right after "╰─ ".
const ghostLine = "\x1b[38;2;212;168;83m╰─ \x1b[38;2;106;103;96mread README.txt\x1b[39m"

func TestGhostPaneDrawsTheRecordedLine(t *testing.T) {
	p := &ghostPane{ghost: "read README.txt"}
	if got, _ := p.Styled(context.Background(), ""); !strings.Contains(got, "\x1b[38;2;212;168;83m╰─ \x1b[38;2;229;229;231m\x1b[38;2;106;103;96mread README.txt") {
		t.Fatalf("fake line %q drifted from the recorded %q", got, ghostLine)
	}
}

// ghostPane plays omp 19's input line. The prediction shows while nothing
// is typed and no key removes it; typed text that begins it leaves the
// rest drawn after the text, other typed text replaces it. The cursor is
// an index into the typed text: Home and End move it, C-u removes what
// is before it, typing goes in at it. Enter sends the typed text only and
// does nothing on an empty line. With cursorAtEnd the cursor is drawn
// after a prediction instead of before it.
type ghostPane struct {
	mu          sync.Mutex
	ghost       string // what the line suggests while empty
	text        string // what was typed
	cur         int    // cursor, in runes of text
	cursorAtEnd bool
	// ghostAfterEnter is shown again once a typed command is submitted, as
	// the prediction comes back when the line empties.
	ghostAfterEnter bool
	keys            []string
	submitted       []string
	relaunches      int
}

func (p *ghostPane) Type(_ context.Context, _, text string, submit, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := []rune(p.text)
	p.text = string(r[:p.cur]) + text + string(r[p.cur:])
	p.cur += len([]rune(text))
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
	p.text, p.cur = "", 0
	if !p.ghostAfterEnter {
		p.ghost = ""
	}
}

func (p *ghostPane) Keys(_ context.Context, _ string, keys ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range keys {
		p.keys = append(p.keys, k)
		r := []rune(p.text)
		switch k {
		case "Enter":
			p.enterLocked()
		case "Home":
			p.cur = 0
		case "End":
			p.cur = len(r)
		case "C-u":
			p.text, p.cur = string(r[p.cur:]), 0
		case "BSpace":
			if p.cur > 0 {
				p.text, p.cur = string(r[:p.cur-1])+string(r[p.cur:]), p.cur-1
			}
		}
	}
	return nil
}

func (p *ghostPane) line(styled bool) string {
	shown, ghost := p.text, ""
	switch {
	case p.text == "":
		ghost = p.ghost
	case strings.HasPrefix(p.ghost, p.text):
		ghost = strings.TrimPrefix(p.ghost, p.text)
	}
	if shown == "" && ghost == "" {
		return "╰─"
	}
	if styled {
		return "\x1b[38;2;212;168;83m╰─ \x1b[38;2;229;229;231m" + shown + "\x1b[38;2;106;103;96m" + ghost + "\x1b[39m"
	}
	return "╰─ " + shown + ghost
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
	x := 3 + p.cur
	if p.cursorAtEnd && p.text == "" && p.ghost != "" {
		x = 3 + len([]rune(p.ghost))
	}
	return x, 2, true, nil
}

func (p *ghostPane) set(f func(*ghostPane)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f(p)
}

func newGhostEngine(t *testing.T, id string, p *ghostPane) *Engine {
	t.Helper()
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakeghost", Managed: true, Cwd: t.TempDir(), Command: []string{"fakeghost"}}},
		Typist:   p, Marker: &fakeMarker{}, Screen: p.Screen, StyledScreen: p.Styled, Cursor: p.Cursor, Home: t.TempDir(),
		Relaunch: func(context.Context, string, []string) error {
			p.set(func(p *ghostPane) { p.relaunches++ })
			return nil
		},
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
	p := &ghostPane{ghost: "read README.txt", text: "half a thought"} // cursor at its start
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

func remembered(t *testing.T, e *Engine, id string) string {
	t.Helper()
	s, err := e.session(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.suggestion
}

// The review's case: the prediction is remembered, then the user types the
// same words at the desk, presses Home and leaves. The draft must be
// cleared, not read as the prediction, and the prompt sent alone.
func TestDeskDraftReadingLikeTheRememberedPredictionIsCleared(t *testing.T) {
	const id = "s_fakeghost5"
	p := &ghostPane{ghost: "read README.txt"}
	e := newGhostEngine(t, id, p)
	if err := say(e, id, "first"); err != nil {
		t.Fatal(err)
	}
	p.set(func(p *ghostPane) { p.ghost, p.text, p.cur, p.keys = "", "read README.txt", 0, nil })
	if err := say(e, id, "second"); err != nil {
		t.Fatal(err)
	}
	if got := p.submitted; len(got) != 2 || got[1] != "second" {
		t.Fatalf("submitted %q, want the prompt alone", got)
	}
	if !strings.HasPrefix(strings.Join(p.keys, " "), "End C-u") {
		t.Fatalf("keys %v, want the draft cleared first", p.keys)
	}
}

// omp predicts afresh after each turn: the new prediction is learned, the
// old one forgotten.
func TestPredictionChangesBetweenTurns(t *testing.T) {
	const id = "s_fakeghost6"
	p := &ghostPane{ghost: "read README.txt", ghostAfterEnter: true}
	e := newGhostEngine(t, id, p)
	if err := say(e, id, "one"); err != nil {
		t.Fatal(err)
	}
	if got := remembered(t, e, id); got != "read README.txt" {
		t.Fatalf("remembered %q", got)
	}
	p.set(func(p *ghostPane) { p.ghost = "run the tests" })
	if err := say(e, id, "two"); err != nil {
		t.Fatal(err)
	}
	if got := remembered(t, e, id); got != "run the tests" {
		t.Fatalf("remembered %q, want the new prediction", got)
	}
	if got := p.submitted; len(got) != 2 || got[1] != "two" {
		t.Fatalf("submitted %q", got)
	}
}

// A relaunch starts a new program: what the old one drew is forgotten.
func TestRelaunchForgetsThePrediction(t *testing.T) {
	const id = "s_fakeghost7"
	p := &ghostPane{ghost: "read README.txt"}
	e := newGhostEngine(t, id, p)
	if err := say(e, id, "one"); err != nil {
		t.Fatal(err)
	}
	if remembered(t, e, id) == "" {
		t.Fatal("prediction not learned")
	}
	arg, _ := json.Marshal("01a1")
	res, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "restart",
		Args: map[string]json.RawMessage{"id": arg}})
	if err != nil || res.Result != harness.ResultRelaunched {
		t.Fatalf("restart = %+v, %v", res, err)
	}
	if got := remembered(t, e, id); got != "" {
		t.Fatalf("remembered %q after a relaunch", got)
	}
}

// Seen live on omp 19.0.1: typed text that begins the prediction leaves
// the rest drawn after it, and Enter sends only what was typed. The prompt
// goes in, Enter follows, and only the prompt is sent.
func TestPromptThatBeginsThePredictionIsSentAsTyped(t *testing.T) {
	const id = "s_fakeghost8"
	p := &ghostPane{ghost: "now let me show you"}
	e := newGhostEngine(t, id, p)
	if err := say(e, id, "now"); err != nil {
		t.Fatal(err)
	}
	if got := p.submitted; len(got) != 1 || got[0] != "now" {
		t.Fatalf("submitted %q, want what was typed", got)
	}
}
