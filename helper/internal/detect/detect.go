// Package detect recognises an agent waiting for the user on a captured
// tmux screen and extracts the question, its answer options and the keys
// that select each option, so a remote answer can be typed into the pane.
//
// Detection is deliberately conservative: a false positive pushes a "needs
// you" alert for a prompt that does not exist, so every shape requires a
// structural cue (a cursor marker on a numbered or arrow menu, or a y/n
// token on the last line) and must sit at the bottom of the screen.
package detect

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Prompt kinds.
const (
	KindPermission = "permission"
	KindQuestion   = "question"
	KindConfirm    = "confirm"
	// KindWaiting is a dialog the agent waits at that offers nothing the
	// phone can press: a text field, a review page, a form. It reaches the
	// phone as needs-you with the screen as its excerpt, answered in the
	// terminal (or by an adapter's question item that knows the dialog).
	KindWaiting = "waiting"
)

// windowLines is how much of the bottom of the screen is inspected. A
// prompt further up has scrolled away and is no longer the thing the agent
// is waiting on.
const windowLines = wire.MaxSnippetLines

// maxScreenBytes bounds the work per capture; a pane far wider or taller
// than any real terminal is cut to its tail.
const maxScreenBytes = 256 << 10

// Prompt is a recognised question on screen.
type Prompt struct {
	Kind   string
	Title  string
	Detail *string
	// Subject is the command, file or URL the prompt asks about, already
	// cleaned for display; nil when the prompt names none.
	Subject *wire.Subject
	Options []wire.Option
	// Keys maps an option number to the tmux key names that select it, as
	// of this capture. Arrow-menu keys are relative to the cursor row, so
	// callers about to type an answer use KeysFor on a fresh capture.
	Keys map[int][]string
	// Snippet is the inspected bottom of the screen, capped to the wire
	// snippet limits.
	Snippet string
	// Fingerprint identifies the prompt across captures: it covers the
	// title, detail and option labels with whitespace collapsed, so cursor
	// moves, redraws and unrelated output do not make the same prompt look
	// new, while the same question about a different command does.
	Fingerprint string
	// SignIn is set for KindSignIn: the page, the device code, and whether
	// a secret is awaited (answered as text, pasted).
	SignIn *wire.SignIn
	// ReadOnly marks a prompt one key cannot answer (Claude's tabbed or
	// multi-select question dialog); its question item answers it.
	ReadOnly bool
	// TextKeys, when not nil, says the prompt takes typed words: these
	// keys open the field (none when the words go straight in), then the
	// words are typed and Enter sent. Kimi's "Reject with feedback".
	TextKeys []string
}

// Detect inspects the last 40 non-empty lines of screen and reports the
// prompt the agent is waiting on, if any.
func Detect(screen string) (Prompt, bool) {
	raw := window(screen)
	if len(raw) == 0 {
		return Prompt{}, false
	}
	lines := make([]string, len(raw))
	boxed := make([]bool, len(raw))
	for i, l := range raw {
		lines[i] = unbox(l)
		boxed[i] = lines[i] != l
	}
	var p Prompt
	ok := false
	if a, isAsk := ParseAsk(screen); isAsk && !a.Review {
		p, ok = askPrompt(a), true
	}
	if !ok {
		p, ok = matchHarness(screen, raw, lines)
	}
	if !ok {
		p, ok = matchAgentChoice(screen)
	}
	if !ok {
		p, ok = matchConfirm(lines)
	}
	if !ok {
		p, ok = matchMenu(lines, boxed)
	}
	if !ok {
		p, ok = matchHintConfirm(lines)
	}
	if !ok {
		p, ok = matchWordButtons(lines)
	}
	if !ok {
		p, ok = matchCrushPermission(raw)
	}
	if !ok {
		p, ok = matchBulletBox(raw)
	}
	if !ok {
		p, ok = matchChoice(screen)
	}
	if !ok {
		p, ok = matchSignIn(raw, lines)
	}
	if !ok {
		p, ok = matchLoginStart(lines)
	}
	if !ok {
		p, ok = matchLineField(lines)
	}
	if !ok {
		p, ok = matchWaiting(raw)
	}
	if !ok {
		return Prompt{}, false
	}
	if p.TextKeys == nil {
		p.TextKeys = feedbackKeys(p)
	}
	if EnterOpensLink(screen) {
		// Enter would open a link outside the terminal: no option that
		// presses it is offered, and with none left there is no prompt.
		var kept []wire.Option
		for _, o := range p.Options {
			if !hasEnter(p.Keys[o.N]) {
				kept = append(kept, o)
			}
		}
		if len(p.Options) > 0 && len(kept) == 0 {
			return Prompt{}, false
		}
		p.Options = kept
	}
	p.Title = truncate(p.Title, wire.MaxTitle)
	if p.Detail != nil {
		d := truncate(*p.Detail, wire.MaxDetail)
		p.Detail = &d
	}
	for i := range p.Options {
		p.Options[i].Label = truncate(p.Options[i].Label, wire.MaxOptionLabel)
	}
	p.Snippet = snippet(raw)
	p.Fingerprint = fingerprint(p)
	return p, true
}

var ansiSeq = regexp.MustCompile(`\x1b(\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_])`)

// window returns the last windowLines non-empty lines with escape
// sequences, control bytes and trailing space removed.
func window(screen string) []string {
	if len(screen) > maxScreenBytes {
		screen = screen[len(screen)-maxScreenBytes:]
	}
	screen = ansiSeq.ReplaceAllString(screen, "")
	all := strings.Split(screen, "\n")
	var out []string
	for i := len(all) - 1; i >= 0 && len(out) < windowLines; i-- {
		l := strings.TrimRight(stripControls(all[i]), " \t")
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func stripControls(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

const boxEdges = "│┃|║"

// unbox removes one level of side borders that TUIs draw around dialogs,
// keeping the indentation inside the box.
func unbox(l string) string {
	t := strings.TrimLeft(l, " ")
	if r, size := utf8.DecodeRuneInString(t); size > 0 && strings.ContainsRune(boxEdges, r) {
		t = t[size:]
		t = strings.TrimRight(t, " ")
		if r, size := utf8.DecodeLastRuneInString(t); size > 0 && strings.ContainsRune(boxEdges, r) {
			t = t[:len(t)-size]
		}
		return strings.TrimRight(t, " ")
	}
	return l
}

// isFiller reports whether l carries no content: blank inside a box, or a
// box edge.
func isFiller(l string) bool {
	return strings.TrimSpace(l) == "" || isBorder(l)
}

// isBorder reports whether l is a horizontal box edge with no content.
func isBorder(l string) bool {
	t := strings.TrimSpace(l)
	if t == "" {
		return false
	}
	for _, r := range t {
		if !strings.ContainsRune("─━═╭╮╰╯┌┐└┘┏┓┗┛╔╗╚╝├┤+-│┃|║ ", r) {
			return false
		}
	}
	return true
}

func snippet(lines []string) string {
	s := strings.Join(lines, "\n")
	for len(s) > wire.MaxSnippetBytes {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return truncateBytes(s[len(s)-wire.MaxSnippetBytes:])
		}
		s = s[i+1:]
	}
	return s
}

// truncateBytes drops a leading partial rune left by a byte cut.
func truncateBytes(s string) string {
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max])
}

func fingerprint(p Prompt) string {
	h := sha256.New()
	h.Write([]byte(normalise(p.Title)))
	h.Write([]byte{0})
	if p.Detail != nil {
		h.Write([]byte(normalise(*p.Detail)))
	}
	for _, o := range p.Options {
		h.Write([]byte{0})
		h.Write([]byte(normalise(o.Label)))
	}
	if s := p.SignIn; s != nil {
		h.Write([]byte{0})
		h.Write([]byte(s.URL + "\x00" + s.Code))
		if s.Secret {
			h.Write([]byte{1})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// normalise collapses runs of whitespace, including line breaks from a
// redraw at a different width, to single spaces.
func normalise(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// KeysFor re-detects the prompt on a fresh capture and returns the keys
// that select the option labelled label. It reports false when the screen
// no longer shows the prompt with that fingerprint or the label is not
// offered, in which case nothing should be typed.
func KeysFor(screen, fingerprint, label string) ([]string, bool) {
	p, ok := Detect(screen)
	if !ok || p.Fingerprint != fingerprint {
		return nil, false
	}
	for _, o := range p.Options {
		if normalise(o.Label) == normalise(label) {
			return p.Keys[o.N], true
		}
	}
	return nil, false
}

// feedbackRow is an option that opens a field for the reason: Kimi Code's
// "Reject with feedback" and its plan review's "Revise".
var feedbackRow = regexp.MustCompile(`(?i)^(reject with feedback|revise)$`)

// feedbackKeys are the keys of a feedback option picked by one digit,
// which a typed answer presses before the words; nil without one.
func feedbackKeys(p Prompt) []string {
	for _, o := range p.Options {
		k := p.Keys[o.N]
		if feedbackRow.MatchString(o.Label) && len(k) == 1 && len(k[0]) == 1 && k[0] >= "1" && k[0] <= "9" {
			return []string{k[0]}
		}
	}
	return nil
}

// hasEnter reports a key list that presses Enter.
func hasEnter(keys []string) bool {
	for _, k := range keys {
		switch strings.ToLower(k) {
		case "enter", "c-m", "kpenter", "return", "\r", "\n":
			return true
		}
	}
	return false
}
