// Package harness turns a coding agent's own data into one normalized form
// the app can draw natively: a conversation of items, a mission control
// snapshot, screens built from seven primitives, and actions that go back
// through the agent's own CLI, config, API or documented typed commands.
//
// The terminal stays the source of truth. Every item carries an Anchor that
// says where in the pane's output it happened, and every screen falls back
// to the terminal when its source fails.
//
// This file holds the conversation item model. Field names follow
// protocol/v1/schema.json ($defs HarnessItem and friends).
package harness

import (
	"encoding/json"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Item kinds. The body of an item sits under the key named by its kind.
const (
	KindUser       = "user"
	KindAssistant  = "assistant"
	KindThinking   = "thinking"
	KindTool       = "tool"
	KindTurn       = "turn"
	KindApproval   = "approval"
	KindQuestion   = "question"
	KindPlan       = "plan"
	KindAgent      = "agent"
	KindBackground = "background"
	KindNotice     = "notice"
	KindShell      = "shell"
)

// Run states shared by tools, turns, agents, background work and shell
// blocks.
const (
	StateRunning     = "running"
	StateWaiting     = "waiting"
	StateOK          = "ok"
	StateFailed      = "failed"
	StateDenied      = "denied"
	StateCancelled   = "cancelled"
	StateInterrupted = "interrupted"
	StateIdle        = "idle"
)

// Tool categories, so the app can pick a glyph and a verb without knowing
// each harness's tool names.
const (
	ToolRead   = "read"
	ToolEdit   = "edit"
	ToolWrite  = "write"
	ToolSearch = "search"
	ToolShell  = "shell"
	ToolWeb    = "web"
	ToolAgent  = "agent"
	ToolMCP    = "mcp"
	ToolOther  = "other"
)

// Limits the helper enforces before an item leaves it.
const (
	MaxItemText   = 64 << 10 // assistant, thinking and user text, in bytes
	MaxToolOutput = 4 << 10  // tail of a tool's output, in bytes
	MaxNeedle     = 120      // anchor needle, in runes
	MaxTitle      = 200      // titles and labels, in runes
)

// Item is one entry in a session's conversation. ID is stable: when a tool
// finishes or streamed text grows, the helper sends the item again with the
// same ID and a new Seq, and the app replaces what it had.
type Item struct {
	ID string `json:"id"`
	// Seq orders the session's stream. It is strictly increasing per session
	// and per Epoch; an update takes a new Seq.
	Seq int64 `json:"seq"`
	// Turn groups the items of one user prompt and the agent's answer.
	TurnID string `json:"turnId,omitempty"`
	// Parent is the ID of the agent item whose own conversation holds this
	// item. Empty for the main conversation.
	Parent string    `json:"parent,omitempty"`
	Kind   string    `json:"kind"`
	At     time.Time `json:"at"`
	Anchor *Anchor   `json:"anchor,omitempty"`

	User       *UserBody       `json:"user,omitempty"`
	Assistant  *TextBody       `json:"assistant,omitempty"`
	Thinking   *TextBody       `json:"thinking,omitempty"`
	Tool       *ToolBody       `json:"tool,omitempty"`
	Turn       *TurnBody       `json:"turn,omitempty"`
	Approval   *ApprovalBody   `json:"approval,omitempty"`
	Question   *QuestionBody   `json:"question,omitempty"`
	Plan       *PlanBody       `json:"plan,omitempty"`
	Agent      *AgentBody      `json:"agent,omitempty"`
	Background *BackgroundBody `json:"background,omitempty"`
	Notice     *NoticeBody     `json:"notice,omitempty"`
	Shell      *ShellBody      `json:"shell,omitempty"`
}

// UserBody is a prompt the user sent, from the phone or at the desk.
type UserBody struct {
	Text string `json:"text"`
	// Queued is true while the prompt waits for the agent to finish its
	// turn. The item is sent again with Queued false once it is typed.
	Queued bool `json:"queued"`
	// Withdrawn is true once the user took a queued prompt back before it
	// was typed. The item stays (same id, new seq) so every client drops it.
	Withdrawn bool `json:"withdrawn,omitempty"`
	// Source is phone, desk or unknown.
	Source string `json:"source"`
	// Images are pictures the prompt carried, such as a pasted screenshot.
	Images []ImageRef `json:"images,omitempty"`
}

// MaxInlineImage is the largest image carried inside an item, decoded. A
// larger picture is described (type and size) and, when it has a path,
// fetched on demand with fs.image, so items stay small.
const MaxInlineImage = 48 << 10

// MaxImagesPerItem bounds the pictures one item lists.
const MaxImagesPerItem = 8

// ImageRef is a picture in the conversation: inline when small, else by
// path, or only described when the source holds neither.
type ImageRef struct {
	// Path is where the file is on this machine, absolute.
	Path string `json:"path,omitempty"`
	// MediaType is image/png, image/jpeg, image/gif or image/webp.
	MediaType string `json:"mediaType,omitempty"`
	// Bytes is the decoded size when known.
	Bytes int `json:"bytes,omitempty"`
	// Data is the picture itself, base64url without padding, at most
	// MaxInlineImage bytes decoded.
	Data string `json:"data,omitempty"`
}

// TextBody is assistant or thinking text, as markdown.
type TextBody struct {
	Text string `json:"text"`
	// Partial is true while the text is still streaming.
	Partial bool `json:"partial"`
	// AppendFrom, on a notification only, says Text is the new tail of a
	// text the client holds, which is this many UTF-8 bytes long.
	AppendFrom *int `json:"appendFrom,omitempty"`
}

// ToolBody is one tool call and its outcome.
type ToolBody struct {
	CallID   string        `json:"callId"`
	Name     string        `json:"name"`
	Title    string        `json:"title"`
	Category string        `json:"category"`
	Subject  *wire.Subject `json:"subject,omitempty"`
	State    string        `json:"state"`
	Started  time.Time     `json:"startedAt"`
	Ended    *time.Time    `json:"endedAt"`
	// Output is the tail of what the tool printed, at most MaxToolOutput
	// bytes, redacted.
	Output   string       `json:"output,omitempty"`
	ExitCode *int         `json:"exitCode,omitempty"`
	Files    []FileChange `json:"files,omitempty"`
	// Images are pictures the tool read or produced.
	Images []ImageRef `json:"images,omitempty"`
}

// FileChange is a file a tool or turn changed.
type FileChange struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

// TurnBody summarises a turn for the folded "Worked 1m 12s, 6 steps, 2
// files changed" line. The item's TurnID equals its ID.
type TurnBody struct {
	State        string     `json:"state"`
	Started      time.Time  `json:"startedAt"`
	Ended        *time.Time `json:"endedAt"`
	Steps        int        `json:"steps"`
	FilesChanged int        `json:"filesChanged"`
}

// ApprovalBody points at a pending approval served by approvals.v1, so the
// conversation shows it inline without a second answer path.
type ApprovalBody struct {
	RequestID string `json:"requestId"`
	// Resolved is set once the approval closed: phone, desk, expired or gone.
	Resolved string `json:"resolved,omitempty"`
	// Owner is the question item that is this same prompt; the question
	// answers it and clients do not draw the approval on its own.
	Owner string `json:"owner,omitempty"`
}

// QuestionBody is one or more questions the agent asked, each with
// labelled options. It is answered with the "answer" action.
type QuestionBody struct {
	Questions []Question `json:"questions"`
	// Answered holds the chosen option ids or text per question id once the
	// questions were answered, from the phone or the desk.
	Answered map[string][]string `json:"answered,omitempty"`
	// Answerable is false when the adapter offers no way to answer from
	// the phone. Nil means true.
	Answerable *bool `json:"answerable,omitempty"`
	// RequestID is the pending approval that is this question at the
	// terminal, once linked.
	RequestID string `json:"requestId,omitempty"`
}

// Question is one question of a Confirm, in an item or a screen.
type Question struct {
	ID        string   `json:"id"`
	Header    string   `json:"header,omitempty"`
	Prompt    string   `json:"prompt"`
	Options   []Choice `json:"options"`
	Multi     bool     `json:"multi"`
	AllowText bool     `json:"allowText"`
}

// PlanBody is a light marker that the plan changed; the full plan lives in
// mission control.
type PlanBody struct {
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Current string `json:"current,omitempty"`
}

// AgentBody is a subagent the agent started. Items whose Parent is this
// item's ID form its own conversation.
type AgentBody struct {
	AgentID string     `json:"agentId"`
	Name    string     `json:"name"`
	Role    string     `json:"role,omitempty"`
	State   string     `json:"state"`
	Started time.Time  `json:"startedAt"`
	Ended   *time.Time `json:"endedAt"`
	Summary string     `json:"summary,omitempty"`
}

// Background kinds.
const (
	BackgroundShell  = "shell"
	BackgroundServer = "server"
	BackgroundExec   = "exec"
	BackgroundJob    = "job"
)

// BackgroundBody is long-running work outside the turn: a background shell,
// a dev server, a Codex exec or a togo job.
type BackgroundBody struct {
	JobID   string     `json:"jobId"`
	Kind    string     `json:"kind"`
	Title   string     `json:"title"`
	Command string     `json:"command,omitempty"`
	State   string     `json:"state"`
	Started time.Time  `json:"startedAt"`
	Ended   *time.Time `json:"endedAt"`
}

// Notice codes.
const (
	NoticeCompacted   = "compacted"
	NoticeModel       = "model"
	NoticeMode        = "mode"
	NoticeRateLimit   = "rate_limit"
	NoticeError       = "error"
	NoticeResumed     = "resumed"
	NoticeInterrupted = "interrupted"
	NoticeFallback    = "fallback"
	// NoticeContinued opens a turn the agent started on its own, such as
	// after a background task or subagent it launched finished. Its text
	// names the trigger.
	NoticeContinued = "continued"
)

// NoticeBody is a quiet system line.
type NoticeBody struct {
	Level string `json:"level"` // info, warning or error
	Code  string `json:"code"`
	// Reason says what kind of error an error notice is (see notice.go):
	// auth, provider_unreachable, rate_limit, model_not_found,
	// context_overflow, crashed, refused or other. Empty otherwise.
	Reason string `json:"reason,omitempty"`
	Text   string `json:"text"`
}

// ShellBody is one command block of a plain shell session, from OSC 133
// prompt marks.
type ShellBody struct {
	Command  string     `json:"command"`
	Cwd      string     `json:"cwd,omitempty"`
	State    string     `json:"state"`
	ExitCode *int       `json:"exitCode"`
	Started  time.Time  `json:"startedAt"`
	Ended    *time.Time `json:"endedAt"`
	// Output is the tail of what the command printed, at most
	// MaxToolOutput bytes of plain text, once the block finished.
	Output string `json:"output,omitempty"`
}

// Choice is a labelled option, used by questions, pickers and settings.
type Choice struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// Anchor says where in the pane's output an item happened, so "Show in
// terminal" can scroll to it and highlight it.
type Anchor struct {
	// Epoch names the pane's output stream. It changes when the pane is
	// respawned or the tap restarts; an anchor from an older epoch can only
	// be found by Needle.
	Epoch string `json:"epoch"`
	// Bytes is how many output bytes the tap had seen in this epoch when
	// the item arrived.
	Bytes int64 `json:"bytes"`
	// Line is the absolute grid line of the cursor when the item arrived:
	// lines scrolled into history plus the cursor row.
	Line int64 `json:"line"`
	// Needle is a short piece of the text on screen near Line, to confirm
	// the match and to search for when Line has scrolled out of history.
	Needle string `json:"needle,omitempty"`
}

// Record is raw data a source produced, before an adapter's mapper turns it
// into items and mission ops. Data never leaves the helper.
type Record struct {
	Source string
	At     time.Time
	Data   json.RawMessage
}
