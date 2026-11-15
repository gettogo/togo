package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// What was entered into a prompt may be repeated by the harness in what it
// shows next: the title of a following prompt ("Invalid key …"), a row, a
// key hint. Nothing read from the terminal is passed on with an entry in
// it. The entry itself is not kept: only its length and a salted digest,
// enough to find it again in a piece of text.

// enteredText is what a session keeps of its entries: one salt, and the
// digests of the entries by their length in runes.
type enteredText struct {
	salt  [16]byte
	sums  map[int]map[[32]byte]bool
	count int
}

// enteredKept bounds how many entries a session's terminal prompts take.
// Digests are never dropped while the session is known: an entry forgotten
// would show again the next time the harness repeats it. At the bound, a
// further entry is refused instead.
const (
	enteredKept = 128
	redacted    = "•••"
)

func (t *enteredText) digest(runes []rune) [32]byte {
	h := sha256.New()
	h.Write(t.salt[:])
	h.Write([]byte(string(runes)))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// noteEntry remembers how to recognise an entry, and the entry without the
// spaces around it, in text read later. It reports false when no more can
// be remembered: then the entry must not be typed.
func (e *Engine) noteEntry(session, value string) bool {
	e.menuMu.Lock()
	defer e.menuMu.Unlock()
	if e.entered == nil {
		e.entered = map[string]*enteredText{}
	}
	t := e.entered[session]
	if t == nil {
		t = &enteredText{sums: map[int]map[[32]byte]bool{}}
		if _, err := rand.Read(t.salt[:]); err != nil {
			return false
		}
		e.entered[session] = t
	}
	forms := []string{value}
	if trimmed := strings.TrimSpace(value); trimmed != value && trimmed != "" {
		forms = append(forms, trimmed)
	}
	if t.count+len(forms) > enteredKept {
		return false
	}
	for _, v := range forms {
		runes := []rune(v)
		if t.sums[len(runes)] == nil {
			t.sums[len(runes)] = map[[32]byte]bool{}
		}
		t.sums[len(runes)][t.digest(runes)] = true
		t.count++
	}
	return true
}

// redact returns text with every entry made in this session replaced.
func (e *Engine) redact(session, text string) string {
	if text == "" {
		return text
	}
	e.menuMu.Lock()
	defer e.menuMu.Unlock()
	t := e.entered[session]
	if t == nil {
		return text
	}
	for n, sums := range t.sums {
		runes := []rune(text)
		if len(runes) < n {
			continue
		}
		var out []rune
		for at := 0; at < len(runes); {
			if at+n <= len(runes) && sums[t.digest(runes[at:at+n])] {
				out = append(out, []rune(redacted)...)
				at += n
				continue
			}
			out = append(out, runes[at])
			at++
		}
		text = string(out)
	}
	return text
}

// redactScreen clears entries from everything a terminal screen says.
func (e *Engine) redactScreen(session string, sc *harness.Screen) *harness.Screen {
	if sc == nil {
		return sc
	}
	sc.Title = e.redact(session, sc.Title)
	for i := range sc.Actions {
		sc.Actions[i].Label = e.redact(session, sc.Actions[i].Label)
	}
	if p := sc.Picker; p != nil {
		for i := range p.Items {
			p.Items[i].Title = e.redact(session, p.Items[i].Title)
			p.Items[i].Subtitle = e.redact(session, p.Items[i].Subtitle)
			p.Items[i].Group = e.redact(session, p.Items[i].Group)
		}
		if p.Presentation != nil {
			p.Presentation.Message = e.redact(session, p.Presentation.Message)
		}
	}
	if st := sc.Settings; st != nil {
		for i := range st.Sections {
			st.Sections[i].Title = e.redact(session, st.Sections[i].Title)
			st.Sections[i].Footer = e.redact(session, st.Sections[i].Footer)
			for j := range st.Sections[i].Fields {
				f := &st.Sections[i].Fields[j]
				f.Label, f.Help, f.Placeholder = e.redact(session, f.Label), e.redact(session, f.Help), e.redact(session, f.Placeholder)
			}
		}
	}
	return sc
}
