package claude

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// The transcript a session follows is named by Claude Code's SessionStart
// hook, which comes once per conversation. A helper restart (togo update)
// keeps the agent running and sends no new hook, and the folder guess
// (transcriptFor) gives up when the folder holds more than one recent
// transcript, as it does after any /clear or /resume. So the path is
// remembered under the helper's run folder,
// run/follow-<session>.claude.json (0600), and read back when the mapper
// is built again.
//
// Set: on each SessionStart this session takes (startup, /clear,
// /resume, a start after the agent exited), and on a relaunch with
// --resume <id>. Cleared: on a relaunch that starts a new conversation,
// and with the session (Engine.Forget, source.RemoveFollows). Kept: when
// the agent exits (the chat still shows that conversation, and the next
// start replaces it), on turn ends, interrupts and reconnects, and across
// a helper restart.
//
// That the session's agent ended is remembered beside it
// (run/follow-<session>.claude-ended.json): after a restart the next
// Claude Code started in the pane is the session's own, not a nested run.
const (
	followName  = "claude"
	followEnded = "claude-ended"
)

// projectsDir is where Claude Code keeps its transcripts.
func projectsDir(env harness.Env) string {
	return filepath.Join(source.Expand("~", env.Vars[source.VarHome]), ".claude", "projects")
}

// remembered returns the transcript followed before a helper restart, ""
// when there is none or it can no longer be followed: it must still be a
// regular file (not a link) ending in .jsonl under the projects folder.
func remembered(env harness.Env) string {
	p := source.LoadFollowed(env, followName)
	if !validTranscript(env, p) {
		return ""
	}
	return p
}

func validTranscript(env harness.Env, p string) bool {
	if p == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p || filepath.Ext(p) != ".jsonl" {
		return false
	}
	root, err := filepath.EvalSymlinks(projectsDir(env))
	if err != nil {
		return false
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil || !strings.HasPrefix(dir, root+string(filepath.Separator)) {
		return false
	}
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().IsRegular()
}

// restore loads what the mapper followed before a helper restart.
func (h *hooks) restore() {
	if p := remembered(h.env); p != "" {
		h.setTranscript(p)
		h.state["harness_session"] = conversationOf(p)
		h.ended = source.LoadFollowed(h.env, followEnded) != ""
	}
}

// remember keeps the followed transcript for a helper restart.
func (h *hooks) remember(p string) {
	source.SaveFollowed(h.env, followName, p)
	h.markEnded(false)
}

func (h *hooks) markEnded(ended bool) {
	h.ended = ended
	v := ""
	if ended {
		v = "1"
	}
	source.SaveFollowed(h.env, followEnded, v)
}

// Relaunched follows the agent Togo started again: --resume <id> opens
// that conversation, in the folder of the one followed now; anything else
// opens a new one, whose SessionStart names it.
func (h *hooks) Relaunched(argv []string, _ time.Time) []harness.Op {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := ""
	for i, a := range argv {
		if a == "--resume" && i+1 < len(argv) {
			id = argv[i+1]
		}
	}
	cur := h.state["transcript_path"]
	if id == "" || strings.HasPrefix(id, "-") || id != filepath.Base(id) || cur == "" {
		source.SaveFollowed(h.env, followName, "")
		return nil
	}
	if next := filepath.Join(filepath.Dir(cur), id+".jsonl"); next != cur {
		source.SaveFollowed(h.env, followName, next)
	}
	return nil
}
