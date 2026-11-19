package enginetest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/tmux"
)

// Terminal is the agent's pane as the engine and the approvals store see
// it: a screen taken from a real capture, an input line that shows what
// was typed into it, and a record of every paste and key. It stands in
// for the engine's Typist, Screen, StyledScreen and Cursor, and for the
// tmux client the approvals store reads and types through.
//
// It models only what an agent's terminal does with input: typed text
// lands in the input line (the row the adapter's input pattern finds in
// the capture), C-u and BSpace edit it, Enter submits it. What the agent
// then does with a submitted line (write a record, fire a hook, start a
// turn) is the test's script, in OnSubmit. A dialog shown over the screen
// is another real capture; a key that answers it closes it, unless the
// test's OnKey says otherwise.
type Terminal struct {
	mu     sync.Mutex
	agent  string
	in     *spec.Input
	re     *regexp.Regexp
	base   Screen
	dialog *Dialog
	input  string
	edited bool
	log    []Input
	hist   []string // lines scrolled off the screen's top (Output)
	subs   []string
	gone   bool // the pane's program ended: nothing reads input
	// OnSubmit is the agent taking a submitted line, called outside the
	// terminal's lock. Nil does nothing with it.
	OnSubmit func(text string)
	// OnKey is called for each key or literal text sent while a dialog
	// is up, before the default (an answer closes the dialog). It
	// returns true when it handled the key itself.
	OnKey func(d *Dialog, key string, literal bool) bool
	// OnInterrupt is the agent taking an interrupt key (Escape or C-c)
	// with no dialog up, outside the lock.
	OnInterrupt func(key string)
}

// Input is one thing sent to the pane.
type Input struct {
	At      time.Time
	Paste   string   // pasted text (Type, PasteText)
	Keys    []string // named keys (Keys, SendKeys)
	Literal string   // text sent as keystrokes (TypeKeys, literal SendKeys)
	Submit  bool     // the paste ended with Enter
}

// Screen is a captured screen: its rows as captured (with colour
// sequences when the capture has them) and where the input line is.
type Screen struct {
	Name string
	Rows []string
	// Input is the input line's row, -1 when the screen has none;
	// Prefix is what the line shows before typed text; Text what was in
	// it when captured.
	Input  int
	Prefix string
	Text   string
}

// Dialog is a prompt, menu or form drawn over the screen.
type Dialog struct {
	Screen Screen
	// Next maps a key to the screen the program draws after it (a
	// cursor move to the next option, captured as "-moved").
	Next map[string]Screen
	// Keys are the keys it took, in order.
	Keys []string
}

// ErrPaneGone is what typing into a pane whose program has ended gives.
var ErrPaneGone = errors.New("enginetest: the pane's program has ended")

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// Plain is s without its colour and control sequences.
func Plain(s string) string { return ansi.ReplaceAllString(s, "") }

// sourceDir is helper/internal, found from this file.
func sourceDir() string {
	dir, _ := os.Getwd()
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) {
		if filepath.Base(d) == "internal" && filepath.Base(filepath.Dir(d)) == "helper" {
			return d
		}
	}
	return filepath.Join(dir, "..", "..", "..")
}

// newTerminal reads the agent's input line from its adapter file.
func newTerminal(agent string) *Terminal {
	t := &Terminal{agent: agent, base: Screen{Input: -1}}
	paths, _ := filepath.Glob(filepath.Join(sourceDir(), "harness", "adapters", agent, "*.yaml"))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if a, err := spec.Parse(b); err == nil && a.Input != nil {
			t.in = a.Input
			t.re, _ = regexp.Compile(a.Input.Line)
		}
	}
	return t
}

// Capture loads a real captured screen from rel, a path under
// helper/internal (harness/engine/testdata/input/omp-idle.txt). The
// input line is the last row the agent's input pattern matches.
func (t *Terminal) Capture(rel string) (Screen, error) {
	b, err := os.ReadFile(filepath.Join(sourceDir(), rel))
	if err != nil {
		return Screen{}, err
	}
	s := Screen{Name: rel, Rows: strings.Split(strings.TrimRight(string(b), "\n"), "\n"), Input: -1}
	if t.re == nil {
		return s, nil
	}
	for i := len(s.Rows) - 1; i >= 0; i-- {
		line := strings.TrimRight(spaces(Plain(s.Rows[i])), " \t\r")
		m := t.re.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		s.Input = i
		if len(m) >= 4 && m[2] >= 0 {
			s.Prefix, s.Text = line[:m[2]], strings.TrimSpace(line[m[2]:m[3]])
			if dimmed(s.Rows[i]) {
				s.Text = "" // a suggestion drawn dim, not typed text
			}
			return s, nil
		}
		s.Prefix = t.prefix(line)
		return s, nil
	}
	return s, nil
}

// prefix finds the shortest start of an empty input line after which
// typed text reads back as typed.
func (t *Terminal) prefix(line string) string {
	for i := range line {
		if i == 0 {
			continue
		}
		p := strings.TrimRight(line[:i], " ") + " "
		if m := t.re.FindStringSubmatch(p + "probe"); len(m) > 1 && m[1] == "probe" {
			return p
		}
	}
	return ""
}

func dimmed(row string) bool { return strings.Contains(row, "\x1b[2m") }

func spaces(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' {
			return ' '
		}
		return r
	}, s)
}

// MustCapture is Capture that panics on a missing file.
func (t *Terminal) MustCapture(rel string) Screen {
	s, err := t.Capture(rel)
	if err != nil {
		panic(err)
	}
	return s
}

// Show makes s the pane's screen, with its input line as captured.
func (t *Terminal) Show(s Screen) {
	t.mu.Lock()
	t.base, t.dialog, t.input, t.edited = s, nil, s.Text, false
	t.mu.Unlock()
}

// Open draws a dialog over the screen.
func (t *Terminal) Open(d *Dialog) {
	t.mu.Lock()
	t.dialog = d
	t.mu.Unlock()
}

// CloseDialog takes the dialog away, as the agent does when it is
// answered at the desk or withdrawn.
func (t *Terminal) CloseDialog() {
	t.mu.Lock()
	t.dialog = nil
	t.mu.Unlock()
}

// Dialog is the dialog up now, nil when none.
func (t *Terminal) Dialog() *Dialog {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dialog
}

// SetGone marks the pane's program ended (true) or running (false).
func (t *Terminal) SetGone(gone bool) {
	t.mu.Lock()
	t.gone = gone
	t.mu.Unlock()
}

// Line is the text in the input line now.
func (t *Terminal) Line() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.input
}

// Log is everything sent to the pane so far.
func (t *Terminal) Log() []Input {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Input(nil), t.log...)
}

// Submitted lists the lines Enter took out of the input line, in order:
// what the agent was given.
func (t *Terminal) Submitted() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.subs...)
}

// Pasted lists the pasted texts, in order.
func (t *Terminal) Pasted() []string {
	var out []string
	for _, in := range t.Log() {
		if in.Paste != "" {
			out = append(out, in.Paste)
		}
	}
	return out
}

// KeysSent lists every named key sent, in order.
func (t *Terminal) KeysSent() []string {
	var out []string
	for _, in := range t.Log() {
		out = append(out, in.Keys...)
	}
	return out
}

// Styled is the screen with its colour sequences.
func (t *Terminal) Styled() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dialog != nil {
		return strings.Join(t.dialog.Screen.Rows, "\n")
	}
	rows := append([]string(nil), t.base.Rows...)
	if i := t.base.Input; i >= 0 && i < len(rows) && t.edited {
		rows[i] = t.base.Prefix + t.input
	}
	return strings.Join(rows, "\n")
}

// Text is the screen as plain text, as capture-pane without -e gives it.
func (t *Terminal) Text() string { return Plain(t.Styled()) }

// The engine's Typist.

// Type pastes text into the input line; submit presses Enter.
func (t *Terminal) Type(_ context.Context, _ string, text string, submit, _ bool) error {
	t.mu.Lock()
	if t.gone {
		t.mu.Unlock()
		return ErrPaneGone
	}
	t.log = append(t.log, Input{At: time.Now(), Paste: text, Submit: submit})
	t.mu.Unlock()
	t.typeText(text)
	if submit {
		t.press("Enter", false)
	}
	return nil
}

// TypeKeys types text as keystrokes.
func (t *Terminal) TypeKeys(_ context.Context, _ string, text string) error {
	t.mu.Lock()
	if t.gone {
		t.mu.Unlock()
		return ErrPaneGone
	}
	t.log = append(t.log, Input{At: time.Now(), Literal: text})
	t.mu.Unlock()
	t.press(text, true)
	return nil
}

// Keys sends named keys.
func (t *Terminal) Keys(_ context.Context, _ string, keys ...string) error {
	t.mu.Lock()
	if t.gone {
		t.mu.Unlock()
		return ErrPaneGone
	}
	t.log = append(t.log, Input{At: time.Now(), Keys: append([]string(nil), keys...)})
	t.mu.Unlock()
	for _, k := range keys {
		t.press(k, false)
	}
	return nil
}

func (t *Terminal) typeText(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dialog != nil {
		return // a paste into a dialog's field; the dialog keeps its screen
	}
	t.input += strings.ReplaceAll(text, "\n", " ")
	t.edited = true
}

// press applies one key (or literal text) the way an agent's input does.
func (t *Terminal) press(key string, literal bool) {
	t.mu.Lock()
	if d := t.dialog; d != nil {
		on := t.OnKey
		t.mu.Unlock()
		if on != nil && on(d, key, literal) {
			return
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.dialog != d {
			return
		}
		d.Keys = append(d.Keys, key)
		if next, ok := d.Next[key]; ok {
			d.Screen = next
			return
		}
		if literal || !moveKey(key) {
			t.dialog = nil // answered: the agent takes it off the screen
		}
		return
	}
	if literal {
		t.input += key
		t.edited = true
		t.mu.Unlock()
		return
	}
	switch key {
	case "C-u":
		t.input, t.edited = "", true
	case "BSpace":
		if t.input != "" {
			_, n := utf8.DecodeLastRuneInString(t.input)
			t.input, t.edited = t.input[:len(t.input)-n], true
		}
	case "Enter":
		text := t.input
		t.input, t.edited = "", true
		if strings.TrimSpace(text) != "" {
			t.subs = append(t.subs, text)
		}
		submit := t.OnSubmit
		t.mu.Unlock()
		if submit != nil && strings.TrimSpace(text) != "" {
			submit(text)
		}
		return
	case "Escape", "C-c":
		on := t.OnInterrupt
		t.mu.Unlock()
		if on != nil {
			on(key)
		}
		return
	}
	t.mu.Unlock()
}

func moveKey(k string) bool {
	switch k {
	case "Up", "Down", "Left", "Right", "Tab", "BTab":
		return true
	}
	return false
}

// The tmux client the approvals store uses (approval.Tmux).

type paneTmux struct{ r *Rig }

func (p paneTmux) ListSessions(context.Context) ([]tmux.SessionInfo, error) {
	return []tmux.SessionInfo{{Name: p.r.ID, Agent: p.r.Opt.Agent, Created: p.r.Opt.Created}}, nil
}

func (p paneTmux) CapturePane(_ context.Context, target string) (string, error) {
	if target != "="+p.r.ID+":" {
		return "", errors.New("enginetest: no such pane " + target)
	}
	return p.r.Term.Text(), nil
}

func (p paneTmux) SendKeys(ctx context.Context, _ string, literal bool, keys ...string) error {
	if literal {
		return p.r.Term.TypeKeys(ctx, p.r.ID, strings.Join(keys, ""))
	}
	return p.r.Term.Keys(ctx, p.r.ID, keys...)
}

func (p paneTmux) PasteText(ctx context.Context, _ string, text string) error {
	return p.r.Term.Type(ctx, p.r.ID, text, false, false)
}
