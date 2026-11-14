package opencode

import (
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// OpenCode's /undo does not delete anything at first: it stages a revert,
// a boundary message from which on the conversation does not apply, and
// puts the files back (session.revert.staged {sessionID, revert:
// {messageID, snapshot, files}}; another /undo stages an earlier boundary).
// /redo clears it (session.revert.cleared), and the terminal commits it
// before the next prompt (session.revert.committed {to}), which removes
// the messages for good. Seen on 2.2.5.
//
// The chat keeps what was said and says what state it is in: one notice
// per undo, after the messages it took back, rewritten as the undo moves,
// is redone or is committed.

// revert2 is a staged revert as the server sends it.
type revert2 struct {
	MessageID string `json:"messageID"`
	Files     []struct {
		File      string  `json:"file"`
		Additions float64 `json:"additions"`
		Deletions float64 `json:"deletions"`
	} `json:"files"`
}

// prompt2 is a prompt of the main session, kept to count and name what an
// undo took back.
type prompt2 struct{ id, text string }

// undo2 is the staged revert the mapper holds a notice for.
type undo2 struct {
	notice   string // item id of its notice
	boundary string // message id the conversation is cut at
	n        int    // prompts from the boundary on; 0 when not known
	from     string // the first of them
	files    string
}

const (
	maxPrompts2    = 1000 // prompts remembered per conversation
	maxUndoFiles2  = 3    // files a notice names
	undoExcerpt2   = 80   // runes of the prompt a notice quotes
	undoFileRunes2 = 60
)

// forgetUndo drops what belonged to the last conversation.
func (e *Events2) forgetUndo() {
	e.prompts, e.undo, e.redone = nil, nil, ""
}

// prompted remembers a prompt of the main session. A new prompt while an
// undo is staged means it was committed (the terminal does that first):
// the notice says so even when the stream dropped the event.
func (e *Events2) prompted(d ev2, at time.Time) []harness.Item {
	if d.SessionID != e.main {
		return nil
	}
	for _, p := range e.prompts {
		if p.id == d.InboxID {
			return nil
		}
	}
	items := e.undoCommitted(at)
	text := strings.Join(strings.Fields(d.Item.Payload.Text), " ")
	e.prompts = append(e.prompts, prompt2{id: d.InboxID, text: truncate(text, undoExcerpt2)})
	if len(e.prompts) > maxPrompts2 {
		e.prompts = e.prompts[len(e.prompts)-maxPrompts2:]
	}
	return items
}

// reverted maps the three revert events of the main session.
func (e *Events2) reverted(kind, evID string, d ev2, at time.Time) ([]harness.Item, []harness.Op) {
	if d.SessionID == "" || d.SessionID != e.main {
		return nil, nil
	}
	switch kind {
	case "session.revert.staged":
		if d.Revert == nil || d.Revert.MessageID == "" {
			return nil, nil
		}
		u := &undo2{notice: "revert-" + evID, boundary: d.Revert.MessageID, files: undoFiles(d.Revert)}
		if e.undo != nil {
			u.notice = e.undo.notice // the same undo, moved
		}
		// Message ids ascend, so everything from the boundary on is undone.
		for _, p := range e.prompts {
			if p.id >= u.boundary {
				if u.n == 0 {
					u.from = p.text
				}
				u.n++
			}
		}
		e.undo = u
		return []harness.Item{e.undoNotice(u, at, "; /redo in the terminal restores "+them(u.n)+".")}, nil
	case "session.revert.cleared":
		if evID != "" && evID == e.redone {
			return nil, nil // the same event again
		}
		e.redone = evID
		id, n := "revert-"+evID, 0
		if e.undo != nil {
			id, n = e.undo.notice, e.undo.n
		}
		e.undo = nil
		text := "Redone in the terminal: the messages apply again."
		if n == 1 {
			text = "Redone in the terminal: 1 message applies again."
		} else if n > 1 {
			text = "Redone in the terminal: " + strconv.Itoa(n) + " messages apply again."
		}
		return []harness.Item{e.notice(id, "", d.SessionID, at, "info", harness.NoticeResumed, text)}, nil
	case "session.revert.committed":
		return e.undoCommitted(at), nil
	}
	return nil, nil
}

// undoCommitted ends the staged undo for good: its messages are gone from
// the conversation, so they are no longer counted and /redo is no longer
// offered.
func (e *Events2) undoCommitted(at time.Time) []harness.Item {
	u := e.undo
	if u == nil {
		return nil
	}
	e.undo = nil
	kept := e.prompts[:0]
	for _, p := range e.prompts {
		if p.id < u.boundary {
			kept = append(kept, p)
		}
	}
	e.prompts = kept
	tail := ". They were removed from the conversation."
	if u.n == 1 {
		tail = ". It was removed from the conversation."
	}
	return []harness.Item{e.undoNotice(u, at, tail)}
}

// undoNotice words a staged or committed undo. It stands on its own after
// the turns it took back (no turn id: a folded turn would hide it).
func (e *Events2) undoNotice(u *undo2, at time.Time, tail string) harness.Item {
	var b strings.Builder
	switch u.n {
	case 0:
		b.WriteString("Messages undone in the terminal")
	case 1:
		b.WriteString("1 message undone in the terminal")
	default:
		b.WriteString(strconv.Itoa(u.n) + " messages undone in the terminal")
	}
	if u.from != "" {
		b.WriteString(", from “" + u.from + "”")
	}
	if strings.HasPrefix(tail, ";") {
		if u.n == 1 {
			b.WriteString(". It no longer applies")
		} else {
			b.WriteString(". They no longer apply")
		}
	}
	b.WriteString(tail)
	if u.files != "" {
		b.WriteString(" Files put back: " + u.files + ".")
	}
	return e.notice(u.notice, "", e.main, at, "info", harness.NoticeResumed, b.String())
}

func them(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// undoFiles lists the files a revert put back, a few by name.
func undoFiles(r *revert2) string {
	var out []string
	for i, f := range r.Files {
		if f.File == "" {
			continue
		}
		if i >= maxUndoFiles2 {
			out = append(out, "and "+strconv.Itoa(len(r.Files)-i)+" more")
			break
		}
		name := strings.Join(strings.Fields(f.File), " ")
		out = append(out, truncate(name, undoFileRunes2)+" +"+strconv.Itoa(int(f.Additions))+" -"+strconv.Itoa(int(f.Deletions)))
	}
	return strings.Join(out, ", ")
}
