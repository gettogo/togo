package claude

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

// nested reports whether a hook payload comes from another Claude Code
// session than the one the pane runs, which the chat must not follow: a
// `claude -p` that the agent's shell tool or a user script ran, which
// inherits TOGO_SESSION and so reports its hooks here. Its SessionStart
// says "startup" while the pane's session has not ended.
//
// The pane's own agent moves to another session only by /clear or
// /resume (SessionStart source "clear" or "resume", a resume from the
// phone included) or by starting again after its session ended, or
// after it died with no SessionEnd (soleAgent). Until a
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
		case src == "clear", src == "resume", h.ended, src == "startup" && h.soleAgent():
			h.markEnded(false)
			return false
		}
	}
	return true
}

// agentCounter counts the session's own processes of a program
// (procs.Session.Within).
type agentCounter interface {
	Within(ctx context.Context, commands []string) (int, error)
}

// soleAgent reports whether the pane runs exactly one Claude Code, the
// one that sent a "startup": the session followed before has gone
// without a SessionEnd (killed, or it exited while the helper was down),
// so the new one is the pane's own and not a nested run, which would run
// below a living Claude Code. Unknown is not sole. It lists processes
// (bounded) and is reached only on such a startup.
func (h *hooks) soleAgent() bool {
	c, ok := h.env.Procs.(agentCounter)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	n, err := c.Within(ctx, []string{"claude"})
	return err == nil && n == 1
}
