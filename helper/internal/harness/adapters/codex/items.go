package codex

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// threadItem is the app-server's ThreadItem, with the fields the mapper
// reads. Unknown types are skipped.
type threadItem struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"`
	Text    string          `json:"text"`
	Phase   string          `json:"phase"`
	Summary []string        `json:"summary"`

	Command          string          `json:"command"`
	Cwd              string          `json:"cwd"`
	Status           string          `json:"status"`
	CommandActions   []commandAction `json:"commandActions"`
	AggregatedOutput *string         `json:"aggregatedOutput"`
	ExitCode         *int            `json:"exitCode"`

	Changes []fileUpdate `json:"changes"`

	Server string          `json:"server"`
	Tool   string          `json:"tool"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`

	Query string `json:"query"`
	Path  string `json:"path"`

	ReceiverThreadIDs []string              `json:"receiverThreadIds"`
	AgentsStates      map[string]agentState `json:"agentsStates"`
	Prompt            *string               `json:"prompt"`
	Model             *string               `json:"model"`
}

type commandAction struct {
	Type    string  `json:"type"`
	Command string  `json:"command"`
	Name    string  `json:"name"`
	Path    *string `json:"path"`
	Query   *string `json:"query"`
}

type fileUpdate struct {
	Path string `json:"path"`
	Kind struct {
		Type     string  `json:"type"`
		MovePath *string `json:"move_path"`
	} `json:"kind"`
	Diff string `json:"diff"`
}

type agentState struct {
	Status  string  `json:"status"`
	Message *string `json:"message"`
}

// where says which turn and conversation an item belongs to and when it
// ran.
type where struct {
	turn    string
	parent  string
	started time.Time
	ended   *time.Time
	at      time.Time
}

func (w where) item(id, kind string) harness.Item {
	return harness.Item{ID: id, TurnID: w.turn, Parent: w.parent, Kind: kind, At: w.at}
}

// userText joins the text inputs of a userMessage.
func userText(content json.RawMessage) string {
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		Path string `json:"path"`
	}
	if json.Unmarshal(content, &parts) != nil {
		return ""
	}
	var out []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, p.Text)
		case "localImage", "image":
			if p.Path != "" {
				out = append(out, "["+filepath.Base(p.Path)+"]")
			}
		case "mention", "skill":
			if p.Path != "" {
				out = append(out, "@"+p.Path)
			}
		}
	}
	return strings.Join(out, "\n")
}

// runState maps the app-server's item statuses.
func runState(status string) string {
	switch status {
	case "inProgress", "pendingInit", "running":
		return harness.StateRunning
	case "completed":
		return harness.StateOK
	case "failed", "errored":
		return harness.StateFailed
	case "declined":
		return harness.StateDenied
	case "interrupted", "shutdown":
		return harness.StateInterrupted
	}
	return harness.StateRunning
}

// innerCommand drops the login-shell wrapper Codex adds
// (/bin/zsh -lc '...').
func innerCommand(cmd string) string {
	for _, sh := range []string{"zsh", "bash", "sh"} {
		for _, pre := range []string{"/bin/" + sh + " -lc ", "/usr/bin/" + sh + " -lc ", sh + " -lc ", "/bin/" + sh + " -c "} {
			if rest, ok := strings.CutPrefix(cmd, pre); ok {
				if len(rest) >= 2 && rest[0] == '\'' && rest[len(rest)-1] == '\'' {
					rest = strings.ReplaceAll(rest[1:len(rest)-1], `'\''`, `'`)
				}
				return rest
			}
		}
	}
	return cmd
}

// commandTool names a command execution by what it does.
func commandTool(it threadItem) (title, category string, subject *wire.Subject) {
	cmd := innerCommand(it.Command)
	if len(it.CommandActions) == 1 {
		a := it.CommandActions[0]
		switch a.Type {
		case "read":
			name := a.Name
			if name == "" && a.Path != nil {
				name = filepath.Base(*a.Path)
			}
			return "Read " + name, harness.ToolRead, &wire.Subject{Kind: wire.SubjectFile, Text: name}
		case "listFiles":
			p := "."
			if a.Path != nil && *a.Path != "" {
				p = *a.Path
			}
			return "List " + p, harness.ToolSearch, nil
		case "search":
			q := ""
			if a.Query != nil {
				q = *a.Query
			}
			return "Search " + q, harness.ToolSearch, nil
		}
	}
	return cmd, harness.ToolShell, &wire.Subject{Kind: wire.SubjectCommand, Text: truncate(cmd, wire.MaxSubjectText)}
}

// convert turns one ThreadItem into items. final is true for
// item/completed and history; partial text is then false.
func convert(it threadItem, w where, final bool) []harness.Item {
	switch it.Type {
	case "userMessage":
		text := userText(it.Content)
		if text == "" {
			return nil
		}
		x := w.item(it.ID, harness.KindUser)
		x.User = &harness.UserBody{Text: clip(text), Source: "unknown"}
		return []harness.Item{x}
	case "agentMessage", "plan":
		if it.Text == "" {
			return nil // the text arrives in deltas
		}
		x := w.item(it.ID, harness.KindAssistant)
		x.Assistant = &harness.TextBody{Text: clip(it.Text), Partial: !final}
		return []harness.Item{x}
	case "reasoning":
		text := strings.Join(it.Summary, "\n\n")
		if text == "" {
			return nil
		}
		x := w.item(it.ID, harness.KindThinking)
		x.Thinking = &harness.TextBody{Text: clip(text), Partial: !final}
		return []harness.Item{x}
	case "commandExecution":
		title, cat, subj := commandTool(it)
		body := toolBody(it.ID, "shell", title, cat, it.Status, w)
		body.Subject = subj
		if it.AggregatedOutput != nil {
			body.Output = tail(*it.AggregatedOutput, harness.MaxToolOutput)
		}
		body.ExitCode = it.ExitCode
		if body.State == harness.StateOK && it.ExitCode != nil && *it.ExitCode != 0 {
			body.State = harness.StateFailed
		}
		return []harness.Item{toolItem(it.ID, w, body)}
	case "fileChange":
		files := make([]harness.FileChange, 0, len(it.Changes))
		cat := harness.ToolWrite
		for _, c := range it.Changes {
			add, rem := countDiff(c.Diff)
			files = append(files, harness.FileChange{Path: c.Path, Added: add, Removed: rem})
			if c.Kind.Type != "add" {
				cat = harness.ToolEdit
			}
		}
		title := "Edit files"
		switch {
		case len(files) == 1 && cat == harness.ToolWrite:
			title = "Write " + filepath.Base(files[0].Path)
		case len(files) == 1:
			title = "Edit " + filepath.Base(files[0].Path)
		case len(files) > 1:
			title = "Edit " + itoa(len(files)) + " files"
		}
		body := toolBody(it.ID, "apply_patch", title, cat, it.Status, w)
		body.Files = files
		if len(files) == 1 {
			body.Subject = &wire.Subject{Kind: wire.SubjectFile, Text: truncate(files[0].Path, wire.MaxSubjectText)}
		}
		return []harness.Item{toolItem(it.ID, w, body)}
	case "mcpToolCall":
		body := toolBody(it.ID, it.Server+"."+it.Tool, it.Server+": "+it.Tool, harness.ToolMCP, it.Status, w)
		if it.Error != nil {
			body.Output = tail(it.Error.Message, harness.MaxToolOutput)
		} else if len(it.Result) > 0 && string(it.Result) != "null" {
			body.Output = tail(string(it.Result), harness.MaxToolOutput)
		}
		return []harness.Item{toolItem(it.ID, w, body)}
	case "dynamicToolCall":
		return []harness.Item{toolItem(it.ID, w, toolBody(it.ID, it.Tool, it.Tool, harness.ToolOther, it.Status, w))}
	case "webSearch":
		st := "completed"
		if !final {
			st = "inProgress"
		}
		return []harness.Item{toolItem(it.ID, w, toolBody(it.ID, "web_search", "Search the web: "+it.Query, harness.ToolWeb, st, w))}
	case "imageView":
		st := "completed"
		if !final {
			st = "inProgress"
		}
		return []harness.Item{toolItem(it.ID, w, toolBody(it.ID, "view_image", "View "+filepath.Base(it.Path), harness.ToolRead, st, w))}
	case "imageGeneration":
		return []harness.Item{toolItem(it.ID, w, toolBody(it.ID, "image_generation", "Generate an image", harness.ToolOther, it.Status, w))}
	case "contextCompaction":
		if !final {
			return nil
		}
		x := w.item(it.ID, harness.KindNotice)
		x.Notice = &harness.NoticeBody{Level: "info", Code: harness.NoticeCompacted, Text: "Context compacted"}
		return []harness.Item{x}
	case "enteredReviewMode", "exitedReviewMode":
		if !final {
			return nil
		}
		text := "Review started"
		if it.Type == "exitedReviewMode" {
			text = "Review finished"
		}
		x := w.item(it.ID, harness.KindNotice)
		x.Notice = &harness.NoticeBody{Level: "info", Code: harness.NoticeMode, Text: text}
		return []harness.Item{x}
	}
	return nil
}

func toolBody(callID, name, title, cat, status string, w where) harness.ToolBody {
	b := harness.ToolBody{CallID: callID, Name: name, Title: truncate(title, harness.MaxTitle), Category: cat,
		State: runState(status), Started: w.started}
	if b.State != harness.StateRunning {
		b.Ended = w.ended
	}
	return b
}

func toolItem(id string, w where, b harness.ToolBody) harness.Item {
	x := w.item(id, harness.KindTool)
	x.Tool = &b
	return x
}

// countDiff counts added and removed lines of a unified diff body.
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

// parseDiff splits a turn's unified diff into files with line counts.
func parseDiff(diff string) []harness.FileChange {
	var out []harness.FileChange
	var cur *harness.FileChange
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			fields := strings.Fields(line)
			path := ""
			if len(fields) >= 4 {
				path = strings.TrimPrefix(fields[3], "b/")
			}
			out = append(out, harness.FileChange{Path: path})
			cur = &out[len(out)-1]
		case strings.HasPrefix(line, "+++ "):
			p := strings.TrimPrefix(line, "+++ ")
			if p != "/dev/null" {
				p = strings.TrimPrefix(p, "b/")
				if cur == nil {
					out = append(out, harness.FileChange{Path: p})
					cur = &out[len(out)-1]
				} else if cur.Path == "" {
					cur.Path = p
				}
			}
		case strings.HasPrefix(line, "--- "):
			if cur == nil {
				out = append(out, harness.FileChange{})
				cur = &out[len(out)-1]
			}
		case cur != nil && strings.HasPrefix(line, "+"):
			cur.Added++
		case cur != nil && strings.HasPrefix(line, "-"):
			cur.Removed++
		}
	}
	files := out[:0]
	for _, f := range out {
		if f.Path != "" {
			files = append(files, f)
		}
	}
	return files
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
