package claude

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// hooks maps Claude Code hook payloads (code.claude.com/docs/en/hooks):
// the session's ids and transcript path, the permission mode, whether a
// turn runs, task hooks into plan rows, and failures into notices. The
// transcript carries the conversation itself.
type hooks struct {
	env harness.Env

	mu        sync.Mutex
	state     map[string]string
	guessedAt time.Time
	notices   int
}

func newHooks(env harness.Env) *hooks {
	return &hooks{env: env, state: map[string]string{}}
}

func (h *hooks) Name() string { return MapperHooks }

// State reports what the hooks said. Before any SessionStart, the newest
// transcript of the session folder stands in for the hook's path.
func (h *hooks) State() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state["transcript_path"] == "" && time.Since(h.guessedAt) > 5*time.Second {
		h.guessedAt = time.Now()
		if p := transcriptFor(h.env); p != "" {
			h.setTranscript(p)
		}
	}
	out := make(map[string]string, len(h.state))
	for k, v := range h.state {
		out[k] = v
	}
	return out
}

func (h *hooks) setTranscript(p string) {
	h.state["transcript_path"] = p
	h.state["transcript_dir"] = strings.TrimSuffix(p, ".jsonl")
}

// Map reads one hook payload.
func (h *hooks) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	p := decodeObj(rec.Data)
	if p == nil {
		return nil, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if v := str(p, "session_id"); v != "" {
		h.state["harness_session"] = v
	}
	if v := str(p, "transcript_path"); v != "" {
		h.setTranscript(v)
	}
	if v := str(p, "permission_mode"); v != "" {
		h.state["mode"] = v
	}
	at := rec.At.UTC()
	switch str(p, "hook_event_name") {
	case "SessionStart":
		if m := str(p, "model"); m != "" {
			h.state["model"] = Alias(m)
		}
		h.setWorking(false, at)
		if str(p, "source") == "resume" {
			return h.notice(at, harness.NoticeResumed, "info", "Resumed an earlier conversation"), nil
		}
	case "UserPromptSubmit", "PreToolUse", "PostToolUse", "PermissionRequest", "SubagentStart", "PreCompact":
		h.setWorking(true, at)
	case "Stop", "SessionEnd":
		h.setWorking(false, at)
	case "StopFailure":
		h.setWorking(false, at)
		if h.state["transcript_path"] != "" {
			// The transcript holds the same failure as a synthetic API
			// error message in Claude's words, and that notice ends the
			// turn: one notice, not two.
			return nil, nil
		}
		tag := str(p, "error")
		text := strings.TrimSpace(firstLine(str(p, "error_details")))
		if text == "" {
			text = stopFailureText(tag)
		}
		items := h.notice(at, "", "", "")
		items[0].Notice = harness.ErrorNotice(errorReason(tag, text), text)
		return items, nil
	case "TaskCreated", "TaskCompleted":
		return nil, taskOps(p)
	}
	return nil, nil
}

// setWorking records whether a turn runs as of the hook's time. No Stop
// hook follows an interrupted turn, so the stamp lets the transcript's
// later interrupt end the turn (harness.MergeWorking).
func (h *hooks) setWorking(w bool, at time.Time) {
	h.state["working"] = fmt.Sprint(w)
	h.state[harness.StateWorkingAt] = harness.StampWorking(at)
}

func (h *hooks) notice(at time.Time, code, level, text string) []harness.Item {
	h.notices++
	return []harness.Item{{
		ID: fmt.Sprintf("hook-notice-%d-%d", at.UnixMilli(), h.notices), Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: level, Code: code, Text: text},
	}}
}

// taskOps turns a task hook into a plan row.
func taskOps(p map[string]any) []harness.Op {
	id := str(p, "task_id")
	if id == "" {
		return nil
	}
	title := firstNonEmpty(str(p, "task_subject"), str(p, "task_description"), "Task "+id)
	state := harness.RowPending
	if str(p, "hook_event_name") == "TaskCompleted" {
		state = harness.RowDone
	}
	return []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionPlan, Row: &harness.Row{ID: id, Title: title, State: state}}}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// Mode lines Claude Code draws under its prompt box.
var modeLines = []struct{ text, mode string }{
	{"manual mode on", "default"}, // 2.1.319 draws the default mode too
	{"plan mode on", "plan"},
	{"accept edits on", "acceptEdits"},
	{"bypass permissions on", "bypassPermissions"},
	{"auto mode on", "auto"},
	{"don't ask on", "dontAsk"},
}

// ReadScreen reads the permission mode from the pane's visible text. The
// default mode draws no mode line, so it is reported only when the prompt
// box is on screen with none.
func (h *hooks) ReadScreen(text string) map[string]string {
	return ReadMode(text)
}

// ReadMode is ReadScreen without a mapper.
func ReadMode(text string) map[string]string {
	lines := strings.Split(strings.TrimRight(text, "\n "), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	box, dialog := false, false
	for _, l := range lines {
		low := strings.ToLower(l)
		if strings.Contains(low, "enter to confirm") || strings.Contains(low, "esc to cancel") {
			dialog = true // a dialog (folder trust, permission) covers the prompt box
		}
		for _, m := range modeLines {
			if strings.Contains(low, m.text) {
				return map[string]string{"mode": m.mode}
			}
		}
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "│ >") || strings.HasPrefix(t, "> ") || t == ">" || promptGlyph(t) ||
			strings.Contains(low, "? for shortcuts") || strings.Contains(low, "shift+tab to cycle") {
			box = true
		}
	}
	if box && !dialog {
		return map[string]string{"mode": "default"}
	}
	return nil
}

// promptGlyph matches the ❯ prompt, not a ❯ that marks a numbered dialog
// choice.
func promptGlyph(t string) bool {
	rest, ok := strings.CutPrefix(t, "❯")
	if !ok {
		rest, ok = strings.CutPrefix(t, "│ ❯")
	}
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	return rest == "" || rest[0] < '0' || rest[0] > '9'
}
