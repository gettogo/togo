package engine

import (
	"context"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/harness"
)

// A chat prompt is never typed into a question. While the session has an
// open approval, or the screen shows a line prompt or dialog the detector
// recognises as a question (Aider's "(Y)es/(N)o [Yes]:"), a send is
// queued behind it, or refused with the reason when it asked to go now.
// Typed into Aider's question the prompt was read as a bad answer ("Please
// answer with one of: yes, no, skip, all") and the chat showed Working.

// blockedBy returns why a prompt cannot be typed now, "" when nothing on
// the session holds it back. A screen that cannot be read holds nothing:
// the dialog guard looks again right before typing.
func (e *Engine) blockedBy(ctx context.Context, s *sess) string {
	if e.cfg.Approvals != nil {
		if p := e.cfg.Approvals.Pending(s.id); len(p) > 0 {
			return askingText(s, p[0].Title)
		}
	}
	if e.cfg.Screen == nil {
		return ""
	}
	screen, err := e.cfg.Screen(ctx, s.id)
	if err != nil {
		return ""
	}
	// Only questions hold a prompt here. Other dialogs (an update box, a
	// splash) stay with the dialog guard, which closes a splash or refuses
	// the send with the dialog's name.
	if p, ok := detect.Detect(screen); ok {
		return askingText(s, p.Title)
	}
	return ""
}

// askingText says the agent waits on a question and nothing was typed.
func askingText(s *sess, title string) string {
	if title == "" {
		return s.ad.Name + " is waiting for an answer. Nothing was typed: answer it first, then send again."
	}
	return s.ad.Name + " is asking " + quoteTitle(clipRunes(title, 120)) + ". Nothing was typed: answer it first, then send again."
}

// refuseBlocked is the error a send that had to go now gets while blocked.
func refuseBlocked(reason string) error {
	return &harness.FallbackError{Reason: "needs_you", Detail: reason}
}
