package engine

import (
	"regexp"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// phoneWindow is how long a prompt typed from the phone waits to be matched
// with the agent's record of it.
const phoneWindow = 2 * time.Minute

// phoneText is a prompt typed from the phone, waiting for the agent's own
// record of it.
//
// A prompt is noted pending before it is sent, because an agent that keeps
// a file can write its record while the helper still waits for the submit
// to show. A record that arrives then is the proof the prompt was sent: it
// takes the phone item's id and matched is set. settlePhoneLocked ends the
// pending note when sending returns; a conversation start-over drops all
// notes (conversation.go).
type phoneText struct {
	id   string
	text string
	// raw is the prompt as sent, before clipping and redaction: what the
	// screen echoes, so awaitText takes it. Kept in memory only; never
	// published or logged.
	raw     string
	images  []harness.ImageRef
	at      time.Time
	pending bool
	matched bool
	// ends is s.turnEnds when the prompt was noted: a turn that ended
	// since did not answer it.
	ends uint64
}

// notePhoneLocked remembers a prompt the helper typed.
func (s *sess) notePhoneLocked(id, text string, images []harness.ImageRef) {
	s.addPhoneLocked(phoneText{id: id, text: text, images: images})
}

// pendPhoneLocked remembers a prompt about to be sent as id. The caller
// settles it with settlePhoneLocked once sending returned.
func (s *sess) pendPhoneLocked(id, text string, images []harness.ImageRef) {
	s.addPhoneLocked(phoneText{id: id, text: text, images: images, pending: true})
}

func (s *sess) addPhoneLocked(p phoneText) {
	// Kept in the form the agent's record is compared in (prepareLocked
	// clips and redacts it first): a prompt over MaxItemText, or one with
	// a key in it, still matches its record.
	p.raw = p.text
	p.ends = s.turnEnds
	p.text = strings.TrimSpace(clipItemText(s.e.red.String(p.text)))
	now := s.e.cfg.Now()
	keep := s.phone[:0]
	for _, q := range s.phone {
		// A pending note waits for its sender, however long typing takes.
		if q.pending || now.Sub(q.at) < phoneWindow {
			keep = append(keep, q)
		}
	}
	p.at = now
	s.phone = append(keep, p)
}

// settlePhoneLocked ends the pending note id once sending returned; sent
// says whether it went. It reports whether the agent's record of the
// prompt already arrived and was published as id, in which case the prompt
// reached the agent whatever sending said. A prompt that was sent and not
// yet recorded stays noted for the record to come; one that failed is
// forgotten, so the same words typed at the desk are not taken for it.
func (s *sess) settlePhoneLocked(id string, sent bool) (recorded bool) {
	for i, p := range s.phone {
		if p.id != id {
			continue
		}
		if p.matched || !sent {
			s.phone = append(s.phone[:i], s.phone[i+1:]...)
			return p.matched
		}
		s.phone[i].pending = false
		s.phone[i].at = s.e.cfg.Now()
		return false
	}
	return false
}

// phoneEndsLocked is the turn ends seen when the prompt id was noted, or
// the count now when no note is left.
func (s *sess) phoneEndsLocked(id string) uint64 {
	for _, p := range s.phone {
		if p.id == id {
			return p.ends
		}
	}
	return s.turnEnds
}

// imagePlaceholder is how an agent writes a pasted picture into the prompt
// text: "[Image #1]" (Claude Code) or "[Image 1]" (OpenCode 2).
var imagePlaceholder = regexp.MustCompile(`\[Image #?\d+\]`)

// matchPhoneLocked finds the phone prompt the agent's record of text is.
// A prompt sent with pictures matches once the pasted paths and the
// agent's [Image #n] placeholders are taken out. Notes are taken oldest
// first, each once, so the same words sent twice are two prompts.
func (s *sess) matchPhoneLocked(self, text string) (phoneText, bool) {
	text = strings.TrimSpace(text)
	for i, p := range s.phone {
		if p.id == self {
			return phoneText{}, false
		}
		if p.matched {
			continue
		}
		if p.text == text || (len(p.images) > 0 && strings.Join(strings.Fields(p.text), " ") == withoutImages(text, p.images)) {
			if p.pending {
				// Recorded while its sender still waits: the sender
				// settles the note. The record is the proof it was sent.
				s.phone[i].matched = true
				s.awaitLocked(p.raw, p.ends)
				return p, true
			}
			s.phone = append(s.phone[:i], s.phone[i+1:]...)
			return p, true
		}
	}
	return phoneText{}, false
}

func withoutImages(text string, images []harness.ImageRef) string {
	for _, r := range images {
		if r.Path != "" {
			text = strings.ReplaceAll(text, harness.QuotePath(r.Path), "")
			text = strings.ReplaceAll(text, r.Path, "")
		}
	}
	text = imagePlaceholder.ReplaceAllString(text, "")
	return strings.Join(strings.Fields(text), " ")
}
