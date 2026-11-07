package claude

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// transcript maps Claude Code's transcript JSONL (~/.claude/projects/<cwd
// key>/<session>.jsonl and its subagents/agent-<id>.jsonl files) into
// conversation items. One instance serves both feeds of a session, so a
// subagent's records find the Agent call that started them.
type transcript struct {
	env harness.Env
	cwd string

	mu         sync.Mutex
	state      map[string]string
	turn       *turnState
	tools      map[string]*harness.Item // tool calls by id
	questions  map[string]*harness.Item // AskUserQuestion calls by id
	texts      map[string]string        // assistant text by message id
	agents     map[string]*harness.Item // agent items by agent id
	prompts    map[string]string        // Agent call id by prompt, until its agent id is known
	bgTasks    map[string]string        // background task id to its tool call id
	roles      map[string]string        // subagent type by Agent call id
	shellAbout map[string]string        // a Bash call's own description, by call id
	plan       []harness.Row
	changes    map[string]*harness.FileChange
	notices    int
	// workingAt is when the main chain last started or ended a turn,
	// the stamp of the working state (harness.MergeWorking).
	workingAt time.Time
}

type turnState struct {
	id      string
	started time.Time
	steps   int
	files   map[string]bool
	state   string
	ended   bool
}

func newTranscript(env harness.Env) *transcript {
	return &transcript{
		env: env, cwd: env.Cwd, state: map[string]string{},
		tools: map[string]*harness.Item{}, questions: map[string]*harness.Item{}, texts: map[string]string{}, agents: map[string]*harness.Item{},
		prompts: map[string]string{}, roles: map[string]string{}, shellAbout: map[string]string{}, bgTasks: map[string]string{}, changes: map[string]*harness.FileChange{},
	}
}

func (t *transcript) Name() string { return MapperTranscript }

func (t *transcript) State() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]string, len(t.state)+2)
	for k, v := range t.state {
		out[k] = v
	}
	if t.turn != nil {
		out["turn"] = t.turn.id
		out["working"] = fmt.Sprint(!t.turn.ended)
		out[harness.StateWorkingAt] = harness.StampWorking(t.workingAt)
	}
	return out
}

// entry is the part of a transcript line the mapper reads.
type entry struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	UUID             string          `json:"uuid"`
	Timestamp        string          `json:"timestamp"`
	SessionID        string          `json:"sessionId"`
	IsSidechain      bool            `json:"isSidechain"`
	AgentID          string          `json:"agentId"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	IsAPIError       bool            `json:"isApiErrorMessage"`
	PermissionMode   string          `json:"permissionMode"`
	Effort           any             `json:"effort"`
	Message          json.RawMessage `json:"message"`
	ToolUseResult    json.RawMessage `json:"toolUseResult"`
	Operation        string          `json:"operation"`
	Content          json.RawMessage `json:"content"`
	DurationMs       float64         `json:"durationMs"`
	CompactMetadata  json.RawMessage `json:"compactMetadata"`
	Origin           json.RawMessage `json:"origin"`
}

type message struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *usage          `json:"usage"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
	Source    *imageSource    `json:"source"`
}

// imageSource is an image block's picture, base64 in the transcript.
type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// imageRef reads an image block: inline when small, else described.
func imageRef(b block) (harness.ImageRef, bool) {
	if b.Type != "image" || b.Source == nil || b.Source.Type != "base64" {
		return harness.ImageRef{}, false
	}
	return harness.InlineImage(b.Source.MediaType, b.Source.Data)
}

// imagePlaceholder is how Claude Code writes a pasted picture into the prompt.
var imagePlaceholder = regexp.MustCompile(`\[Image #\d+\]\s*`)

// Map reads one transcript line. Lines it does not know are skipped.
func (t *transcript) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var e entry
	if json.Unmarshal(rec.Data, &e) != nil {
		return nil, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if e.SessionID != "" && !e.IsSidechain {
		t.state["harness_session"] = e.SessionID
	}
	switch e.Type {
	case "permission-mode":
		var pm struct {
			PermissionMode string `json:"permissionMode"`
		}
		if json.Unmarshal(rec.Data, &pm) == nil && pm.PermissionMode != "" {
			t.state["mode"] = pm.PermissionMode
		}
	case "user":
		return t.user(e)
	case "assistant":
		return t.assistant(e)
	case "system":
		return t.system(e)
	case "queue-operation":
		return t.queued(e)
	}
	return nil, nil
}

// parentOf is the agent item that owns a sidechain record.
func (t *transcript) parentOf(e entry) string {
	if !e.IsSidechain || e.AgentID == "" {
		return ""
	}
	return agentItemID(e.AgentID)
}

func agentItemID(agentID string) string { return "agent-" + agentID }

func (t *transcript) user(e entry) ([]harness.Item, []harness.Op) {
	if e.PermissionMode != "" && !e.IsSidechain {
		t.state["mode"] = e.PermissionMode
	}
	if e.IsMeta || e.IsCompactSummary {
		return nil, nil
	}
	var m message
	if json.Unmarshal(e.Message, &m) != nil {
		return nil, nil
	}
	at := stamp(e.Timestamp)
	var text string
	if json.Unmarshal(m.Content, &text) == nil {
		return t.prompt(e, at, text, nil)
	}
	var blocks []block
	if json.Unmarshal(m.Content, &blocks) != nil {
		return nil, nil
	}
	var items []harness.Item
	var ops []harness.Op
	var texts []string
	var images []harness.ImageRef
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			it, o := t.toolResult(e, b, at)
			items, ops = append(items, it...), append(ops, o...)
		case "text":
			texts = append(texts, b.Text)
		case "image":
			if r, ok := imageRef(b); ok && len(images) < harness.MaxImagesPerItem {
				images = append(images, r)
			}
		}
	}
	if len(texts) > 0 || len(images) > 0 {
		text := strings.Join(texts, "\n")
		if len(images) > 0 {
			text = strings.TrimSpace(imagePlaceholder.ReplaceAllString(text, ""))
		}
		it, o := t.prompt(e, at, text, images)
		items, ops = append(items, it...), append(ops, o...)
	}
	return items, ops
}

var (
	cmdName   = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	cmdArgs   = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
	cmdStdout = regexp.MustCompile(`(?s)<local-command-stdout>(.*?)</local-command-stdout>`)
	anyTag    = regexp.MustCompile(`^\s*<([a-z-]+)>`)
)

// prompt maps a user message: a typed prompt starts a turn; command
// wrappers become the command as typed; local command output and
// interruptions become notices; other tagged text is skipped.
func (t *transcript) prompt(e entry, at time.Time, text string, images []harness.ImageRef) ([]harness.Item, []harness.Op) {
	parent := t.parentOf(e)
	if strings.HasPrefix(strings.TrimSpace(text), "[Request interrupted by user") {
		var items []harness.Item
		if parent == "" {
			// Ends the main chain's work even when the turn is already
			// over, such as one a task notification started.
			t.workingAt = at
		}
		if t.turn != nil && !t.turn.ended {
			t.turn.state = harness.StateInterrupted
			items = append(items, t.endTurn(at)...)
		}
		return append(items, t.notice(e, at, harness.NoticeInterrupted, "info", "Interrupted")...), nil
	}
	if m := cmdName.FindStringSubmatch(text); m != nil {
		cmd := strings.TrimSpace(m[1])
		if a := cmdArgs.FindStringSubmatch(text); a != nil {
			cmd = strings.TrimSpace(cmd + " " + strings.TrimSpace(a[1]))
		}
		text = cmd
	} else if m := cmdStdout.FindStringSubmatch(text); m != nil {
		out := strings.TrimSpace(plain(m[1]))
		if out == "" {
			return nil, nil
		}
		return t.notice(e, at, noticeCode(out), "info", firstLine(out)), nil
	} else if parent == "" && isTaskNotification(e, text) {
		return t.continued(e, at, text), nil
	} else if anyTag.MatchString(text) {
		return nil, nil
	}
	if strings.TrimSpace(text) == "" && len(images) == 0 {
		return nil, nil
	}
	if parent != "" {
		items, ops := t.linkAgent(e.AgentID, text, at)
		return append(items, harness.Item{ID: e.UUID, Parent: parent, Kind: harness.KindUser, At: at,
			User: &harness.UserBody{Text: text, Source: "unknown", Images: images}}), ops
	}
	var items []harness.Item
	if t.turn != nil && !t.turn.ended {
		items = append(items, t.endTurn(at)...)
	}
	t.turn = &turnState{id: e.UUID, started: at, files: map[string]bool{}, state: harness.StateRunning}
	t.workingAt = at
	items = append(items, harness.Item{ID: e.UUID, TurnID: e.UUID, Kind: harness.KindUser, At: at,
		User: &harness.UserBody{Text: text, Images: images}})
	return items, nil
}

func noticeCode(out string) string {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "set model to"):
		return harness.NoticeModel
	case strings.Contains(low, "compacted"):
		return harness.NoticeCompacted
	}
	return harness.NoticeMode
}

func (t *transcript) notice(e entry, at time.Time, code, level, text string) []harness.Item {
	id := e.UUID
	if id == "" {
		t.notices++
		id = fmt.Sprintf("notice-%d-%d", at.UnixMilli(), t.notices)
	}
	turn := ""
	if t.turn != nil {
		turn = t.turn.id
	}
	return []harness.Item{{ID: id, TurnID: turn, Parent: t.parentOf(e), Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: level, Code: code, Text: text}}}
}

func (t *transcript) assistant(e entry) ([]harness.Item, []harness.Op) {
	var m message
	if json.Unmarshal(e.Message, &m) != nil {
		return nil, nil
	}
	at := stamp(e.Timestamp)
	if m.Model != "" && m.Model != "<synthetic>" && !e.IsSidechain {
		t.state["model"] = Alias(m.Model)
	}
	if lvl, ok := e.Effort.(string); ok && lvl != "" && !e.IsSidechain {
		t.state["effort"] = lvl
	}
	var blocks []block
	if json.Unmarshal(m.Content, &blocks) != nil {
		return nil, nil
	}
	parent := t.parentOf(e)
	turn := ""
	if parent == "" && t.turn != nil {
		turn = t.turn.id
	}
	var items []harness.Item
	var ops []harness.Op
	if !e.IsSidechain && m.Model != "<synthetic>" {
		ops = append(ops, contextOps(m)...)
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			if e.IsAPIError || m.Model == "<synthetic>" {
				items = append(items, t.notice(e, at, harness.NoticeError, "error", firstLine(b.Text))...)
				continue
			}
			id := firstNonEmpty(m.ID, e.UUID)
			txt := b.Text
			if prev, ok := t.texts[id]; ok {
				txt = prev + "\n\n" + b.Text
			}
			t.texts[id] = txt
			items = append(items, harness.Item{ID: id, TurnID: turn, Parent: parent, Kind: harness.KindAssistant, At: at,
				Assistant: &harness.TextBody{Text: txt}})
		case "thinking":
			if strings.TrimSpace(b.Thinking) == "" {
				continue
			}
			id := firstNonEmpty(m.ID, e.UUID) + ".thinking"
			items = append(items, harness.Item{ID: id, TurnID: turn, Parent: parent, Kind: harness.KindThinking, At: at,
				Thinking: &harness.TextBody{Text: b.Thinking}})
		case "tool_use":
			it, o := t.toolUse(e, b, at, turn, parent)
			items, ops = append(items, it...), append(ops, o...)
		}
	}
	return items, ops
}

func (t *transcript) system(e entry) ([]harness.Item, []harness.Op) {
	at := stamp(e.Timestamp)
	switch e.Subtype {
	case "turn_duration":
		if !e.IsSidechain {
			t.workingAt = at
		}
		if t.turn != nil && !t.turn.ended && !e.IsSidechain {
			if t.turn.state == harness.StateRunning {
				t.turn.state = harness.StateOK
			}
			return t.endTurn(at), nil
		}
	case "compact_boundary":
		var meta struct {
			Trigger string `json:"trigger"`
		}
		_ = json.Unmarshal(e.CompactMetadata, &meta)
		text := "Conversation compacted"
		if meta.Trigger == "auto" {
			text = "Conversation compacted automatically"
		}
		return t.notice(e, at, harness.NoticeCompacted, "info", text), nil
	case "api_error":
		return t.notice(e, at, harness.NoticeError, "warning", "The API returned an error; retrying"), nil
	}
	return nil, nil
}

// endTurn closes the current turn and emits its fold item.
func (t *transcript) endTurn(at time.Time) []harness.Item {
	tr := t.turn
	if tr == nil || tr.ended {
		return nil
	}
	tr.ended = true
	if tr.state == harness.StateRunning {
		tr.state = harness.StateOK
	}
	end := at
	return []harness.Item{{ID: tr.id + ".turn", TurnID: tr.id, Kind: harness.KindTurn, At: tr.started,
		Turn: &harness.TurnBody{State: tr.state, Started: tr.started, Ended: &end, Steps: tr.steps, FilesChanged: len(tr.files)}}}
}
