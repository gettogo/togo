package opencode

import (
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// textID names a streamed text or reasoning block of one step.
func textID(kind, msg string, ordinal int) string {
	if strings.HasPrefix(kind, "session.reasoning") {
		return msg + "-r" + strconv.Itoa(ordinal)
	}
	return msg + "-t" + strconv.Itoa(ordinal)
}

func (e *Events2) textStarted(kind string, d ev2, at time.Time) {
	if !e.follows(d.SessionID) || d.MessageID == "" {
		return
	}
	k := harness.KindAssistant
	if strings.HasPrefix(kind, "session.reasoning") {
		k = harness.KindThinking
	}
	e.texts[textID(kind, d.MessageID, d.Ordinal)] = &part{kind: k, turn: e.turnFor(d.SessionID), sess: d.SessionID, start: at}
}

func (e *Events2) textDelta(kind string, d ev2, at time.Time) ([]harness.Item, []harness.Op) {
	id := textID(kind, d.MessageID, d.Ordinal)
	st := e.texts[id]
	if st == nil {
		return nil, nil
	}
	st.text += d.Delta
	if (!st.last.IsZero() && at.Sub(st.last) < partialEvery) || strings.TrimSpace(st.text) == "" {
		return nil, nil
	}
	st.last = at
	return []harness.Item{textItem(id, st.turn, e.parent(st.sess), st.kind, st.start, st.text, true)}, nil
}

func (e *Events2) textEnded(kind string, d ev2, at time.Time) ([]harness.Item, []harness.Op) {
	id := textID(kind, d.MessageID, d.Ordinal)
	st := e.texts[id]
	delete(e.texts, id)
	if st == nil {
		if !e.follows(d.SessionID) {
			return nil, nil
		}
		e.textStarted(kind, d, at)
		st = e.texts[id]
		delete(e.texts, id)
	}
	text := d.Text
	if text == "" {
		text = st.text
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	return []harness.Item{textItem(id, st.turn, e.parent(st.sess), st.kind, st.start, text, false)}, nil
}

// tool follows one tool call from its first input to its result.
func (e *Events2) tool(kind string, d ev2, at time.Time) ([]harness.Item, []harness.Op) {
	if !e.follows(d.SessionID) || d.ID == "" {
		return nil, nil
	}
	prev, seen := e.tools[d.ID]
	name := d.Name
	st := toolState{Status: "running", Input: d.Input, Metadata: d.Metadata}
	if seen {
		if name == "" {
			name = prev.Tool.Name
		}
		if len(st.Input) == 0 {
			st.Input = e.inputs[d.ID]
		}
		st.Time.Start = prev.Tool.Started.UnixMilli()
	} else {
		st.Time.Start = at.UnixMilli()
	}
	switch kind {
	case "session.tool.success":
		st.Status = "completed"
		st.Output = contentText(d.Content)
		st.Time.End = at.UnixMilli()
	case "session.tool.failed":
		st.Status = "error"
		if d.Error != nil {
			st.Error = errText(d.Error)
			if d.Error.Type == "rejected" || strings.Contains(st.Error, "reject") {
				st.Error = "rejected permission: " + st.Error
			}
		}
		st.Time.End = at.UnixMilli()
	}
	turnID := e.turnFor(d.SessionID)
	x := toolItemIn(e.env.Cwd, d.ID, "", name, st, turnID, e.parent(d.SessionID), at)
	if tu := e.turns[turnID]; tu != nil {
		tu.steps[x.ID] = true
		for _, f := range x.Tool.Files {
			tu.files[f.Path] = true
		}
	}
	if x.Tool.State == harness.StateRunning {
		if seen && prev.Tool.State == harness.StateWaiting {
			x.Tool.State = harness.StateWaiting
		}
		e.tools[d.ID] = x
		if len(st.Input) > 0 {
			e.inputs[d.ID] = st.Input
		}
	} else {
		delete(e.tools, d.ID)
		delete(e.inputs, d.ID)
	}
	return []harness.Item{x}, nil
}

func contentText(cs []content2) string {
	var b strings.Builder
	for _, c := range cs {
		if c.Type == "text" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		}
	}
	return b.String()
}
