package engine

import (
	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/harness"
)

// QuestionDialog reports whether a prompt the pane watcher found is a step
// of Claude Code's question dialog (AskUserQuestion: one of its questions
// or its review) while the session shows that question as an open item.
// The item, and the one request the engine offers for it, stand for the
// whole dialog. Opened as screen approvals as well, each step closed the
// one before it as answered at the desk although nobody had answered, and
// a two-question dialog left four approval rows beside the question, the
// pending one not answerable from the phone.
//
// Only an adapter whose answer action is claude_ask is paired: other
// agents' dialogs are answered as the screen prompt the watcher found.
func (e *Engine) QuestionDialog(session string, p detect.Prompt) bool {
	if !p.Ask {
		return false
	}
	e.mu.Lock()
	s := e.sessions[session]
	e.mu.Unlock()
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ad == nil || s.ad.Actions[harness.ActionAnswer].Builtin != "claude_ask" {
		return false
	}
	for _, q := range s.needs.open {
		if takesText(&q) {
			return true
		}
	}
	return false
}
