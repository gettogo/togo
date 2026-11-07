package opencode

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

type partPayload struct {
	Part struct {
		ID        string `json:"id"`
		MessageID string `json:"messageID"`
		SessionID string `json:"sessionID"`
		Type      string `json:"type"`
		Text      string `json:"text"`
		Synthetic bool   `json:"synthetic"`
		Ignored   bool   `json:"ignored"`
		Time      *struct {
			Start int64 `json:"start"`
			End   int64 `json:"end"`
		} `json:"time"`
		Tool   string    `json:"tool"`
		CallID string    `json:"callID"`
		State  toolState `json:"state"`
		Files  []string  `json:"files"`
	} `json:"part"`
}

type toolState struct {
	Status   string          `json:"status"`
	Input    json.RawMessage `json:"input"`
	Output   string          `json:"output"`
	Title    string          `json:"title"`
	Error    string          `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
	Time     struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	} `json:"time"`
}

func (e *Events) partUpdated(raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p partPayload
	if json.Unmarshal(raw, &p) != nil || p.Part.ID == "" || !e.follows(p.Part.SessionID) {
		return nil, nil
	}
	pt := p.Part
	msg, known := e.messages[pt.MessageID]
	turnID := msg.turn
	parent := e.parentOf(pt.SessionID)
	switch pt.Type {
	case "text", "reasoning":
		if pt.Synthetic || pt.Ignored {
			return nil, nil
		}
		kind := harness.KindAssistant
		switch {
		case pt.Type == "reasoning":
			kind = harness.KindThinking
		case known && msg.role == "user":
			kind = harness.KindUser
		}
		st := e.parts[pt.ID]
		if st == nil {
			st = &part{kind: kind, turn: turnID, sess: pt.SessionID, start: at}
			if pt.Time != nil && pt.Time.Start > 0 {
				st.start = ms(pt.Time.Start)
			}
			e.parts[pt.ID] = st
		}
		st.text = pt.Text
		final := kind == harness.KindUser || (pt.Time != nil && pt.Time.End > 0)
		if final {
			delete(e.parts, pt.ID)
		}
		if strings.TrimSpace(pt.Text) == "" {
			return nil, nil
		}
		return []harness.Item{textItem(pt.ID, turnID, parent, kind, st.start, pt.Text, !final)}, nil
	case "tool":
		x := e.toolItem(pt.CallID, pt.ID, pt.Tool, pt.State, turnID, parent, at)
		if tu := e.turns[turnID]; tu != nil {
			tu.steps[x.ID] = true
			for _, f := range x.Tool.Files {
				tu.files[f.Path] = true
			}
		}
		if x.Tool.State == harness.StateRunning {
			if old, ok := e.tools[x.ID]; ok && old.Tool.State == harness.StateWaiting {
				x.Tool.State = harness.StateWaiting
			}
			e.tools[x.ID] = x
		} else {
			delete(e.tools, x.ID)
		}
		return []harness.Item{x}, nil
	case "patch":
		if tu := e.turns[turnID]; tu != nil {
			for _, f := range pt.Files {
				tu.files[rel(e.env.Cwd, f)] = true
			}
		}
	case "compaction":
		return []harness.Item{e.notice("compaction-"+pt.ID, turnID, parent, at, "info", harness.NoticeCompacted, "Context compacted")}, nil
	}
	return nil, nil
}

func textItem(id, turn, parent, kind string, at time.Time, text string, partial bool) harness.Item {
	x := harness.Item{ID: id, TurnID: turn, Parent: parent, Kind: kind, At: at}
	switch kind {
	case harness.KindUser:
		x.User = &harness.UserBody{Text: clip(text), Source: "unknown"}
	case harness.KindThinking:
		x.Thinking = &harness.TextBody{Text: clip(text), Partial: partial}
	default:
		x.Assistant = &harness.TextBody{Text: clip(text), Partial: partial}
	}
	return x
}

// partDelta grows a streamed text part; the partial copy goes out at most
// every partialEvery.
func (e *Events) partDelta(raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		SessionID string `json:"sessionID"`
		PartID    string `json:"partID"`
		Field     string `json:"field"`
		Delta     string `json:"delta"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Field != "text" {
		return nil, nil
	}
	st := e.parts[p.PartID]
	if st == nil || st.kind == harness.KindUser {
		return nil, nil
	}
	st.text += p.Delta
	if !st.last.IsZero() && at.Sub(st.last) < partialEvery {
		return nil, nil
	}
	st.last = at
	if strings.TrimSpace(st.text) == "" {
		return nil, nil
	}
	return []harness.Item{textItem(p.PartID, st.turn, e.parentOf(st.sess), st.kind, st.start, st.text, true)}, nil
}

var categories = map[string]string{
	"read": harness.ToolRead, "edit": harness.ToolEdit, "multiedit": harness.ToolEdit, "patch": harness.ToolEdit,
	"apply_patch": harness.ToolEdit, "write": harness.ToolWrite, "glob": harness.ToolSearch, "grep": harness.ToolSearch,
	"list": harness.ToolSearch, "ls": harness.ToolSearch, "codesearch": harness.ToolSearch, "bash": harness.ToolShell,
	"webfetch": harness.ToolWeb, "websearch": harness.ToolWeb, "task": harness.ToolAgent,
	"todowrite": harness.ToolOther, "todoread": harness.ToolOther, "question": harness.ToolOther, "skill": harness.ToolOther,
}

type toolInput struct {
	Command  string `json:"command"`
	FilePath string `json:"filePath"`
	Path     string `json:"path"`
	Pattern  string `json:"pattern"`
	URL      string `json:"url"`
	Query    string `json:"query"`
}

type toolMeta struct {
	Exit     *int `json:"exit"`
	FileDiff *struct {
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
	} `json:"filediff"`
	Diff string `json:"diff"`
}

func (e *Events) toolItem(callID, partID, name string, s toolState, turnID, parent string, at time.Time) harness.Item {
	id := callID
	if id == "" {
		id = partID
	}
	var in toolInput
	json.Unmarshal(s.Input, &in)
	var meta toolMeta
	json.Unmarshal(s.Metadata, &meta)
	cat, ok := categories[name]
	if !ok {
		cat = harness.ToolMCP // tools OpenCode does not ship come from MCP servers or plugins
	}
	file := in.FilePath
	if file == "" {
		file = in.Path
	}
	b := harness.ToolBody{CallID: id, Name: name, Category: cat, State: harness.StateRunning, Started: orAt(ms(s.Time.Start), at)}
	switch s.Status {
	case "completed":
		b.State = harness.StateOK
		b.Output = tail(s.Output, harness.MaxToolOutput)
	case "error":
		b.State = harness.StateFailed
		if strings.Contains(s.Error, "rejected permission") {
			b.State = harness.StateDenied
		}
		b.Output = tail(s.Error, harness.MaxToolOutput)
	}
	if b.State != harness.StateRunning {
		b.Ended = ptr(orAt(ms(s.Time.End), at))
	}
	if meta.Exit != nil {
		b.ExitCode = meta.Exit
		if *meta.Exit != 0 && b.State == harness.StateOK {
			b.State = harness.StateFailed
		}
	}
	b.Title = s.Title
	if cat == harness.ToolSearch && in.Pattern != "" {
		// OpenCode titles a search with the folder it searched.
		b.Title = "Search " + in.Pattern
	}
	switch {
	case in.Command != "":
		b.Subject = &wire.Subject{Kind: wire.SubjectCommand, Text: truncate(in.Command, wire.MaxSubjectText)}
		if b.Title == "" {
			b.Title = in.Command
		}
	case file != "":
		r := rel(e.env.Cwd, file)
		b.Subject = &wire.Subject{Kind: wire.SubjectFile, Text: truncate(r, wire.MaxSubjectText)}
		if cat == harness.ToolEdit || cat == harness.ToolWrite {
			fc := harness.FileChange{Path: r}
			if meta.FileDiff != nil {
				fc.Added, fc.Removed = meta.FileDiff.Additions, meta.FileDiff.Deletions
			} else if meta.Diff != "" {
				fc.Added, fc.Removed = countDiff(meta.Diff)
			}
			b.Files = []harness.FileChange{fc}
		}
		if b.Title == "" || b.Title == file {
			b.Title = verb(cat) + " " + filepath.Base(file)
		}
	}
	if b.Title == "" {
		switch {
		case in.Pattern != "":
			b.Title = "Search " + in.Pattern
		case in.URL != "":
			b.Title = "Fetch " + in.URL
		case in.Query != "":
			b.Title = "Search the web: " + in.Query
		default:
			b.Title = name
		}
	}
	b.Title = truncate(b.Title, harness.MaxTitle)
	return harness.Item{ID: id, TurnID: turnID, Parent: parent, Kind: harness.KindTool, At: b.Started, Tool: &b}
}

func verb(cat string) string {
	switch cat {
	case harness.ToolRead:
		return "Read"
	case harness.ToolEdit:
		return "Edit"
	case harness.ToolWrite:
		return "Write"
	}
	return "Open"
}

func countDiff(diff string) (added, removed int) {
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

func (e *Events) diff(raw json.RawMessage) []harness.Op {
	var p struct {
		SessionID string `json:"sessionID"`
		Diff      []struct {
			File      string `json:"file"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
		} `json:"diff"`
	}
	if json.Unmarshal(raw, &p) != nil || p.SessionID != e.main {
		return nil
	}
	rows := make([]harness.Row, 0, len(p.Diff))
	for _, d := range p.Diff {
		path := rel(e.env.Cwd, d.File)
		rows = append(rows, harness.Row{ID: path, Title: path, Subtitle: fmt.Sprintf("+%d -%d", d.Additions, d.Deletions),
			State: harness.StateOK, Open: &harness.Open{Kind: harness.OpenDiff, Ref: path}})
	}
	return []harness.Op{{Op: harness.OpReplace, Section: harness.SectionChanges, Rows: rows}}
}

func (e *Events) todos(raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		SessionID string `json:"sessionID"`
		Todos     []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if json.Unmarshal(raw, &p) != nil || p.SessionID != e.main {
		return nil, nil
	}
	body := &harness.PlanBody{Total: len(p.Todos)}
	rows := make([]harness.Row, 0, len(p.Todos))
	for i, t := range p.Todos {
		st := harness.RowPending
		switch t.Status {
		case "completed":
			st = harness.RowDone
			body.Done++
		case "in_progress":
			st = harness.RowActive
			if body.Current == "" {
				body.Current = truncate(t.Content, harness.MaxTitle)
			}
		case "cancelled":
			st = harness.RowSkipped
		}
		id := t.ID
		if id == "" {
			id = "todo-" + strconv.Itoa(i+1)
		}
		rows = append(rows, harness.Row{ID: id, Title: truncate(t.Content, harness.MaxTitle), State: st})
	}
	turnID := e.state["turn"]
	id := "plan-" + turnID
	first, ok := e.planFirst[id]
	if !ok {
		first = at
		e.planFirst[id] = at
	}
	x := harness.Item{ID: id, TurnID: turnID, Kind: harness.KindPlan, At: first, Plan: body}
	return []harness.Item{x}, []harness.Op{{Op: harness.OpReplace, Section: harness.SectionPlan, Rows: rows}}
}

func (e *Events) sessionError(raw json.RawMessage, at time.Time) []harness.Item {
	var p struct {
		SessionID string `json:"sessionID"`
		Error     *struct {
			Name string `json:"name"`
			Data struct {
				Message string `json:"message"`
			} `json:"data"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Error == nil || (p.SessionID != "" && !e.follows(p.SessionID)) {
		return nil
	}
	if p.Error.Name == "MessageAbortedError" {
		return nil // the message update reports it
	}
	text := p.Error.Data.Message
	if text == "" {
		text = p.Error.Name
	}
	return []harness.Item{e.notice("session-error-"+strconv.FormatInt(at.UnixMilli(), 10), e.state["turn"], e.parentOf(p.SessionID), at, "error", harness.NoticeError, text)}
}
