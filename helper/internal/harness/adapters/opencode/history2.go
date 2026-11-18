package opencode

import (
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/harness"
)

// historyMessage is the record type of a message read from the
// conversation's past (the adapter's `history`): the messages route gives
// each message whole, where the event stream gave it piece by piece. The
// items are built with the ids the stream's events give them, so a message
// that is also seen live is one item, not two.
const historyMessage = "session.message"

// message2 is a message of GET /api/session/{id}/message (2.2.5): a user
// prompt, an assistant message with its parts, or a marker (idle and the
// rest), which carries nothing the chat shows.
type message2 struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
	Time struct {
		Created   int64 `json:"created"`
		Completed int64 `json:"completed"`
	} `json:"time"`
	Content []struct {
		Type  string `json:"type"`
		Text  string `json:"text"`
		ID    string `json:"id"`
		Name  string `json:"name"`
		State struct {
			Status   string          `json:"status"`
			Input    json.RawMessage `json:"input"`
			Content  []content2      `json:"content"`
			Error    *error2         `json:"error"`
			Metadata json.RawMessage `json:"metadata"`
		} `json:"state"`
		Time struct {
			Created   int64 `json:"created"`
			Completed int64 `json:"completed"`
		} `json:"time"`
	} `json:"content"`
}

// pastMessage maps one message of the conversation's past.
func (e *Events2) pastMessage(conv string, raw json.RawMessage) ([]harness.Item, []harness.Op) {
	var m message2
	if json.Unmarshal(raw, &m) != nil || m.ID == "" {
		return nil, nil
	}
	if e.main == "" && conv != "" {
		// After a helper restart nothing else has named the session yet.
		e.setMain(conv)
	}
	if conv != e.main {
		return nil, nil
	}
	at := ms(m.Time.Created).UTC()
	switch m.Type {
	case "user":
		e.pastTurn = "turn-" + m.ID
		if m.Text == "" {
			return nil, nil
		}
		return []harness.Item{textItem(m.ID, e.pastTurn, "", harness.KindUser, at, m.Text, false)}, nil
	case "assistant":
		var items []harness.Item
		texts, thoughts := 0, 0
		for _, p := range m.Content {
			switch p.Type {
			case "text":
				if p.Text != "" {
					items = append(items, textItem(textID("session.text", m.ID, texts), e.pastTurn, "", harness.KindAssistant, at, p.Text, false))
				}
				texts++
			case "reasoning":
				if p.Text != "" {
					items = append(items, textItem(textID("session.reasoning", m.ID, thoughts), e.pastTurn, "", harness.KindThinking, at, p.Text, false))
				}
				thoughts++
			case "tool":
				if p.ID == "" {
					continue
				}
				st := toolState{Status: p.State.Status, Metadata: p.State.Metadata, Output: contentText(p.State.Content)}
				// A call still streaming holds its input as text, not yet an object.
				if len(p.State.Input) > 0 && p.State.Input[0] == '{' {
					st.Input = p.State.Input
				}
				if st.Status == "streaming" {
					st.Status = "running"
				}
				if p.State.Error != nil {
					st.Error = errText(p.State.Error)
				}
				st.Time.Start = p.Time.Created
				st.Time.End = p.Time.Completed
				if st.Time.Start == 0 {
					st.Time.Start = m.Time.Created
				}
				if st.Time.End == 0 && st.Status != "running" {
					// A settled call with no end time ended with its message.
					st.Time.End = max(m.Time.Completed, st.Time.Start)
				}
				items = append(items, toolItemIn(e.env.Cwd, p.ID, "", p.Name, st, e.pastTurn, "", ms(st.Time.Start).UTC()))
			}
		}
		return items, nil
	}
	return nil, nil
}
