package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// questionPrompt asks the agent to use its own ask-the-user tool. Every
// harness names that tool differently (AskUserQuestion, ask, question,
// request_user_input), so the prompt describes it instead.
const questionPrompt = "Use your own ask-the-user tool, the one that shows me a multiple-choice question to pick from, " +
	"to ask me exactly one question: \"Which color should the button be?\" with the three options Red, Green and Blue. " +
	"Do not answer it yourself and use no other tool. After I answer, reply with only the word done."

// statusNeedsYou is the hub's session status while a prompt waits.
const statusNeedsYou = "needs_you"

// questionScenario: the agent's own question must reach the chat as a
// question item with its options, turn the roster to Needs you, be
// answerable from the phone through the answer action, and close once
// answered, with the agent moving on.
func (r *runner) questionScenario(ctx context.Context) {
	if !r.has(harness.CapChat) {
		r.set("question", skip, "adapter has no chat")
		return
	}
	r.ensureAsking(ctx, "question")
	r.mu.Lock()
	mark := r.seq
	r.mu.Unlock()
	sentAt := time.Now()
	if err := r.call(ctx, harness.MethodSend, map[string]any{"id": r.sid, "text": questionPrompt, "mode": "queue"}, nil); err != nil {
		r.set("question", fail, "send: %v", err)
		return
	}
	var q *harness.Item
	var screen *wire.Approval
	screenAt := time.Time{}
	reply, ended := "", false
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && q == nil {
		if sleep(ctx, 700*time.Millisecond) != nil {
			break
		}
		r.mu.Lock()
		for _, it := range r.newItems(mark) {
			switch {
			case it.Parent != "":
			case it.Kind == harness.KindQuestion && it.Question != nil && len(it.Question.Answered) == 0 && len(it.Question.Questions) > 0:
				it := it
				q = &it
			case it.Kind == harness.KindAssistant && it.Assistant != nil && !it.Assistant.Partial:
				reply = it.Assistant.Text
			case it.Kind == harness.KindTurn && it.Turn != nil:
				ended = it.Turn.State != harness.StateRunning && it.Turn.State != harness.StateWaiting
			}
		}
		r.mu.Unlock()
		if q != nil {
			break
		}
		if screen == nil {
			for _, p := range r.pending(ctx) {
				if !p.CreatedAt.Before(sentAt.Add(-2 * time.Second)) {
					p := p
					screen, screenAt = &p, time.Now()
				}
			}
		}
		if screen != nil && time.Since(screenAt) > 8*time.Second {
			break // the screen prompt stands; its item never came
		}
		if ended && screen == nil && time.Since(sentAt) > 5*time.Second {
			break
		}
	}
	if q == nil {
		switch {
		case screen != nil && !r.has(harness.CapNeedsQuestions):
			// The agent keeps no record of its ask (Cursor's AskQuestion
			// fires no hook): the screen prompt is the card, and must be
			// answerable from the phone and close.
			r.screenQuestion(ctx, screen)
		case screen != nil:
			r.set("question", fail, "the question reached Needs you only as a screen prompt %q (%d options), no question item: the adapter does not map its ask tool",
				clip(oneLine(screen.Title), 60), len(screen.Options))
			if len(screen.Options) > 0 {
				// Leave the agent unblocked: pick its first option.
				_ = r.call(ctx, wire.MethodApprovalAnswer, map[string]string{"requestId": screen.RequestID, "decision": "option:1"}, nil)
			}
		case ended && reply != "":
			r.set("question", skip, "the agent asked nothing with a tool (no question tool?); it replied %q", clip(oneLine(reply), 80))
		default:
			full := r.captureLate(ctx)
			pane := screenTail(full, 14)
			why := "no question item within 90s and nothing in Needs you"
			// Detect's reading, not a glyph: Cline's status line draws
			// "○ Plan ● Act" at every idle input box.
			if _, ok := detect.ParseChoice(full); ok {
				why = "the pane draws a choice dialog that never reached the chat or Needs you"
			} else if _, ok := detect.Detect(full); ok {
				why = "the pane draws a prompt that never reached the chat or Needs you"
			}
			// Was the prompt typed, or still held for an idle agent?
			typed := "the prompt was never listed"
			r.mu.Lock()
			for _, it := range r.newItems(mark) {
				if it.Kind == harness.KindUser && it.User != nil && strings.HasPrefix(it.User.Text, questionPrompt[:30]) {
					typed = map[bool]string{true: "the prompt is still queued (the agent never read as idle)", false: "the prompt was typed"}[it.User.Queued]
				}
			}
			r.mu.Unlock()
			bottom := []rune(oneLine(screenTail(pane, 6)))
			if len(bottom) > 90 {
				bottom = bottom[len(bottom)-90:]
			}
			r.set("question", fail, "%s; %s; mission working %v; reply %q; pane: …%s", why, typed, r.working, clip(oneLine(reply), 40), string(bottom))
		}
		return
	}
	first := q.Question.Questions[0]
	var labels []string
	for _, o := range first.Options {
		labels = append(labels, o.Label)
	}
	if len(first.Options) < 2 {
		r.set("question", fail, "question item %q has %d options (%s)", clip(first.Prompt, 50), len(first.Options), strings.Join(labels, "|"))
		return
	}
	// The roster must say Needs you, from the item alone.
	status := ""
	for i := 0; i < 20 && status != statusNeedsYou; i++ {
		if s, err := r.session(ctx); err == nil && s != nil {
			status = s.Status
		}
		if status != statusNeedsYou {
			_ = sleep(ctx, 500*time.Millisecond)
		}
	}
	if status != statusNeedsYou {
		r.set("question", fail, "question %q (%s) is in the chat but the roster status is %q, not needs_you", clip(first.Prompt, 50), strings.Join(labels, "|"), status)
	}
	if q.Question.Answerable != nil && !*q.Question.Answerable {
		r.set("question", fail, "question %q (%s) is not answerable from the phone (adapter has no answer action)", clip(first.Prompt, 50), strings.Join(labels, "|"))
		return
	}
	pick := first.Options[0]
	res, err := r.act(ctx, harness.ActionAnswer, q.ID, map[string]any{"answers": map[string][]string{first.ID: {pick.ID}}})
	if err != nil || res.Result != harness.ResultApplied {
		r.set("question", fail, "answer %q to %q: %s", pick.Label, clip(first.Prompt, 50), firstNonEmpty(errText(err), res.Result+" "+res.Message))
		return
	}
	// The item closes with the answer and the roster lets go.
	answered, released := false, false
	end := time.Now().Add(45 * time.Second)
	for time.Now().Before(end) && !(answered && released) {
		if sleep(ctx, 700*time.Millisecond) != nil {
			break
		}
		r.mu.Lock()
		if it, ok := r.items[q.ID]; ok && it.Question != nil && len(it.Question.Answered) > 0 {
			answered = true
		}
		r.mu.Unlock()
		if s, err := r.session(ctx); err == nil && s != nil {
			released = s.Status != statusNeedsYou && (s.Mission == nil || s.Mission.NeedsYou == 0)
		}
	}
	switch {
	case !answered:
		r.set("question", fail, "answered %q from the phone (%s) but the question item never recorded the answer", pick.Label, res.Result)
	case !released:
		var still []string
		for _, p := range r.pending(ctx) {
			still = append(still, fmt.Sprintf("%s %q", p.Source, clip(oneLine(p.Title+" "+subjectText(p.Subject)), 50)))
		}
		r.set("question", fail, "answered %q from the phone but the roster still says needs_you 45s later; pending: %s; pane: %s",
			pick.Label, strings.Join(still, ", "), clip(oneLine(screenTail(r.captureLate(ctx), 8)), 200))
	}
	idle := r.waitIdle(ctx, 60*time.Second)
	r.set("question", pass, "question %q (%s) reached the chat and Needs you; %q answered from the phone (%s), item closed in %.0fs%s",
		clip(first.Prompt, 40), strings.Join(labels, "|"), pick.Label, res.Result, time.Since(sentAt).Seconds(), map[bool]string{true: "", false: "; turn still running after 60s"}[idle])
}

// screenQuestion answers a screen-only question by its request id with the
// first option and checks that it leaves Needs you and the agent goes on.
func (r *runner) screenQuestion(ctx context.Context, a *wire.Approval) {
	if len(a.Options) < 2 {
		r.set("question", fail, "the screen prompt %q has %d options", clip(oneLine(a.Title), 60), len(a.Options))
		return
	}
	status := ""
	if s, err := r.session(ctx); err == nil && s != nil {
		status = s.Status
	}
	if err := r.call(ctx, wire.MethodApprovalAnswer, map[string]string{"requestId": a.RequestID, "decision": fmt.Sprintf("option:%d", a.Options[0].N)}, nil); err != nil {
		r.set("question", fail, "approval.answer %s: %v", short8(a.RequestID), err)
		return
	}
	end := time.Now().Add(45 * time.Second)
	for time.Now().Before(end) {
		if sleep(ctx, time.Second) != nil {
			break
		}
		open := false
		for _, p := range r.pending(ctx) {
			if p.RequestID == a.RequestID {
				open = true
			}
		}
		if !open {
			_ = r.waitIdle(ctx, 30*time.Second)
			r.set("question", pass, "screen prompt %q (%d options, roster %s) answered %q from the phone and closed; the adapter records no question item", clip(oneLine(a.Title), 50), len(a.Options), status, a.Options[0].Label)
			return
		}
	}
	r.set("question", fail, "answered the screen prompt %q from the phone but it is still pending", clip(oneLine(a.Title), 50))
}
