package pisession

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/internal/errtext"
	x "github.com/gettogo/togo/helper/internal/harness/adapters/internal/itemtext"
)

type message struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	Provider     string          `json:"provider"`
	Model        string          `json:"model"`
	StopReason   string          `json:"stopReason"`
	ErrorMessage string          `json:"errorMessage"`
	ErrorStatus  int             `json:"errorStatus"`
	ToolCallID   string          `json:"toolCallId"`
	ToolName     string          `json:"toolName"`
	IsError      bool            `json:"isError"`
	Command      string          `json:"command"`
	Output       string          `json:"output"`
	ExitCode     *int            `json:"exitCode"`
	Cancelled    bool            `json:"cancelled"`
	Summary      string          `json:"summary"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// blocks reads content written as a string or as typed blocks.
func blocks(raw json.RawMessage) []block {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	json.Unmarshal(raw, &bs)
	return bs
}

func text(bs []block) string {
	var out []string
	for _, b := range bs {
		if b.Type == "text" && b.Text != "" {
			out = append(out, b.Text)
		}
	}
	return strings.Join(out, "\n")
}

func (s *Session) message(id string, raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var m message
	if json.Unmarshal(raw, &m) != nil || id == "" {
		return nil, nil
	}
	switch m.Role {
	case "user":
		items := s.startTurn(id, at)
		return append(items, harness.Item{ID: id, TurnID: s.turnID(), Kind: harness.KindUser, At: at,
			User: &harness.UserBody{Text: x.Clip(text(blocks(m.Content))), Source: "unknown"}}), nil
	case "assistant":
		return s.assistant(id, m, at)
	case "toolResult":
		return s.toolResult(m, at), nil
	case "bashExecution":
		return s.bash(id, m, at), nil
	case "compactionSummary", "branchSummary":
		return []harness.Item{{ID: id, TurnID: s.turnID(), Kind: harness.KindNotice, At: at,
			Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeCompacted, Text: x.Title(m.Summary)}}}, nil
	}
	return nil, nil
}

func (s *Session) assistant(id string, m message, at time.Time) ([]harness.Item, []harness.Op) {
	var items []harness.Item
	if s.turn == nil {
		items = s.startTurn(id, at)
	}
	s.turn.steps++
	mi, ops := s.setModel(m.Provider, m.Model, "", at)
	items = append(items, mi...)
	for i, b := range blocks(m.Content) {
		bid := id + ":" + strconv.Itoa(i)
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				items = append(items, harness.Item{ID: bid, TurnID: s.turnID(), Kind: harness.KindAssistant, At: at,
					Assistant: &harness.TextBody{Text: x.Clip(b.Text)}})
			}
		case "thinking":
			if strings.TrimSpace(b.Thinking) != "" {
				items = append(items, harness.Item{ID: bid, TurnID: s.turnID(), Kind: harness.KindThinking, At: at,
					Thinking: &harness.TextBody{Text: x.Clip(b.Thinking)}})
			}
		case "toolCall":
			items = append(items, s.toolCall(b, at))
		}
	}
	// An aborted reply carries errorMessage "Interrupted by user" too; only
	// a failed one gets an error notice.
	switch {
	case m.StopReason == "aborted":
		items = append(items, harness.Item{ID: id + ":interrupted", TurnID: s.turnID(), Kind: harness.KindNotice, At: at,
			Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeInterrupted, Text: "Interrupted"}})
	case m.StopReason == "error" || m.ErrorMessage != "":
		items = append(items, harness.Item{ID: id + ":error", TurnID: s.turnID(), Kind: harness.KindNotice, At: at,
			Notice: errtext.Notice(m.ErrorMessage, m.ErrorStatus, "The model request failed")})
	}
	switch m.StopReason {
	case "stop", "length":
		end, _ := s.endTurn(harness.StateOK, at)
		items = append(items, end...)
	case "error":
		end, _ := s.endTurn(harness.StateFailed, at)
		items = append(items, end...)
	case "aborted":
		end, _ := s.endTurn(harness.StateInterrupted, at)
		items = append(items, end...)
	}
	return items, ops
}

func (s *Session) toolCall(b block, at time.Time) harness.Item {
	var args struct {
		Path    string `json:"path"`
		Command string `json:"command"`
		Pattern string `json:"pattern"`
	}
	json.Unmarshal(b.Arguments, &args)
	title := b.Name
	for _, v := range []string{args.Command, x.Rel(s.env.Cwd, args.Path), args.Pattern} {
		if v != "" {
			title = b.Name + " " + v
			break
		}
	}
	it := harness.Item{ID: b.ID, TurnID: s.turnID(), Kind: harness.KindTool, At: at,
		Tool: &harness.ToolBody{CallID: b.ID, Name: b.Name, Title: x.Title(title), Category: x.Category(b.Name), State: harness.StateRunning, Started: at}}
	if args.Path != "" && (it.Tool.Category == harness.ToolEdit || it.Tool.Category == harness.ToolWrite) {
		it.Tool.Files = []harness.FileChange{{Path: x.Rel(s.env.Cwd, args.Path)}}
		s.turn.files[args.Path] = true
	}
	s.tools[b.ID] = it
	return it
}

func (s *Session) toolResult(m message, at time.Time) []harness.Item {
	it, ok := s.tools[m.ToolCallID]
	if !ok {
		return nil
	}
	delete(s.tools, m.ToolCallID)
	tb := *it.Tool
	tb.State, tb.Ended, tb.Output = harness.StateOK, x.Ptr(at), x.Tail(text(blocks(m.Content)), harness.MaxToolOutput)
	if m.IsError {
		tb.State = harness.StateFailed
	}
	it.Tool = &tb
	return []harness.Item{it}
}

// bash is a command the user ran with "!" in the TUI.
func (s *Session) bash(id string, m message, at time.Time) []harness.Item {
	state := harness.StateOK
	if m.Cancelled {
		state = harness.StateCancelled
	} else if m.ExitCode != nil && *m.ExitCode != 0 {
		state = harness.StateFailed
	}
	return []harness.Item{{ID: id, TurnID: s.turnID(), Kind: harness.KindTool, At: at,
		Tool: &harness.ToolBody{CallID: id, Name: "bash", Title: x.Title(m.Command), Category: harness.ToolShell, State: state,
			Started: at, Ended: x.Ptr(at), Output: x.Tail(m.Output, harness.MaxToolOutput), ExitCode: m.ExitCode}}}
}
