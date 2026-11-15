package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// What was entered into a prompt may be repeated by the harness in what it
// shows next: the title of a following prompt ("Invalid key …"), a row, a
// key hint. Nothing read from the terminal is passed on with an entry in
// it. The entry itself is not kept: only its length and a salted digest,
// enough to find it again in a piece of text.

type enteredText struct {
	n    int // length in runes
	salt [16]byte
	sum  [32]byte
	at   time.Time
}

const (
	enteredLife = 30 * time.Minute
	enteredKept = 6
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
// spaces around it, in text read later.
func (e *Engine) noteEntry(session, value string) {
	e.menuMu.Lock()
	defer e.menuMu.Unlock()
	if e.entered == nil {
		e.entered = map[string][]enteredText{}
	}
	for _, v := range []string{value, strings.TrimSpace(value)} {
		runes := []rune(v)
		if len(runes) == 0 {
			continue
		}
		t := enteredText{n: len(runes), at: time.Now()}
		_, _ = rand.Read(t.salt[:])
		t.sum = t.digest(runes)
		list := append(e.entered[session], t)
		if len(list) > enteredKept {
			list = list[len(list)-enteredKept:]
		}
		e.entered[session] = list
		if v == value && strings.TrimSpace(value) == value {
			break
		}
	}
}

// redact returns text with every entry made in this session replaced.
func (e *Engine) redact(session, text string) string {
	if text == "" {
		return text
	}
	e.menuMu.Lock()
	list := append([]enteredText(nil), e.entered[session]...)
	e.menuMu.Unlock()
	for i := range list {
		t := &list[i]
		if time.Since(t.at) > enteredLife {
			continue
		}
		runes := []rune(text)
		if len(runes) < t.n {
			continue
		}
		var out []rune
		for at := 0; at < len(runes); {
			if at+t.n <= len(runes) && t.digest(runes[at:at+t.n]) == t.sum {
				out = append(out, []rune(redacted)...)
				at += t.n
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
