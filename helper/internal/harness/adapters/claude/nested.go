package claude

import (
	"path/filepath"
	"strings"
)

// nested reports whether a hook payload comes from another Claude Code
// session than the one the pane runs, which the chat must not follow: a
// `claude -p` that the agent's shell tool or a user script ran, which
// inherits TOGO_SESSION and so reports its hooks here. Its SessionStart
// says "startup" while the pane's session has not ended.
//
// The pane's own agent moves to another session only by /clear or
// /resume (SessionStart source "clear" or "resume", a resume from the
// phone included) or by starting again after its session ended. Until a
// session is known, the first one seen is taken. Every other payload of
// another session is dropped, so a nested run's prompts, tools and stop
// do not change this session's state.
func (h *hooks) nested(p map[string]any) bool {
	id := str(p, "session_id")
	cur := h.state["harness_session"]
	if cur == "" && h.state["transcript_path"] != "" {
		// Known from the transcript guess only: its file is named by the
		// session id.
		cur = strings.TrimSuffix(filepath.Base(h.state["transcript_path"]), ".jsonl")
	}
	if id == "" || cur == "" || id == cur {
		return false
	}
	if str(p, "hook_event_name") == "SessionStart" {
		switch src := str(p, "source"); {
		case src == "clear", src == "resume", h.ended:
			h.markEnded(false)
			return false
		}
	}
	return true
}
