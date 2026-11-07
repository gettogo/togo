package local

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// QuestionWaiter is the part of the approvals store that holds a Claude
// AskUserQuestion hook until the phone answers its question item.
type QuestionWaiter interface {
	WaitQuestion(ctx context.Context, sessionID string, prompts []string) (map[string]string, bool)
}

// question serves local.question: it blocks until the question item is
// answered from the phone, the timeout passes or the hook goes away.
// Without a device to answer it returns at once, so Claude's own dialog
// is the only one.
func (h *Handlers) question(ctx context.Context, raw json.RawMessage) (any, error) {
	var p wire.QuestionParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	id, err := managedID(p.SessionID)
	if err != nil {
		return nil, err
	}
	switch {
	case id == "":
		return nil, invalid("sessionId is required")
	case len(p.Questions) == 0 || len(p.Questions) > wire.MaxQuestions:
		return nil, invalid("1-16 questions")
	case p.TimeoutSec < 1 || p.TimeoutSec > 3600:
		return nil, invalid("timeoutSec must be 1-3600")
	}
	for _, q := range p.Questions {
		if strings.TrimSpace(q) == "" || len(q) > wire.MaxQuestionPrompt {
			return nil, invalid("a question is empty or too long")
		}
	}
	w, ok := h.Approvals.(QuestionWaiter)
	if !ok || (h.Reach != nil && !h.Reach(p.SessionID)) {
		return wire.QuestionResult{Reason: wire.AskTimeout}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutSec)*time.Second)
	defer cancel()
	answers, ok := w.WaitQuestion(ctx, id, p.Questions)
	if !ok {
		return wire.QuestionResult{Reason: wire.AskTimeout}, nil
	}
	return wire.QuestionResult{Answers: answers, Reason: wire.AskAnswered}, nil
}

// askTool is Claude Code's question tool. While it runs, Claude's own
// permission notification is that question's dialog, which the question
// item already shows; no approval opens for it.
const askTool = "AskUserQuestion"

// noteTool records the tool a session is about to run ("" when none).
func (h *Handlers) noteTool(id, tool string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.asking == nil {
		h.asking = map[string]bool{}
	}
	if tool == askTool {
		h.asking[id] = true
	} else {
		delete(h.asking, id)
	}
}

func (h *Handlers) askingQuestion(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.asking[id]
}
