package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// turnOut is what one prompt produced.
type turnOut struct {
	reply       string // final assistant text of the main conversation
	turnState   string // the turn item's last state, if the adapter has turns
	workingSeen bool   // the mission summary said working at some point
	cleared     bool   // and said not working at the end
	approval    *wire.Approval
	errText     string // an error notice, a refused send, or a failed turn
	timedOut    bool
	sendErr     error
	sentAt      time.Time
	dur         time.Duration
	seen        []string // the kinds of the items the turn produced, for a failure's report
}

var okRE = regexp.MustCompile(`(?im)^\W*ok\W*$`)

// turn sends prompt and waits for the turn to end (or for an approval,
// when stopOnApproval). onWorking, when set, runs once the first time the
// session is seen working (used to interrupt).
func (r *runner) turn(ctx context.Context, prompt string, stopOnApproval bool, onWorking func()) (out turnOut) {
	r.mu.Lock()
	mark := r.seq
	r.workingSeen = false
	r.mu.Unlock()
	out.sentAt = time.Now()
	defer func() { out.dur = time.Since(out.sentAt) }()

	if r.desc.Adapter != nil {
		out.sendErr = r.call(ctx, harness.MethodSend, map[string]any{"id": r.sid, "text": prompt, "mode": "queue"}, nil)
	} else {
		out.sendErr = r.call(ctx, wire.MethodSessionSendText, map[string]any{"id": r.sid, "text": prompt, "submit": true}, nil)
	}
	if out.sendErr != nil {
		out.errText = "send: " + out.sendErr.Error()
		var re *rpcError
		if errors.As(out.sendErr, &re) && re.Code == -32008 {
			if line, found := r.inputLine(r.capture(ctx)); found {
				out.errText += fmt.Sprintf(" while the input line on screen reads %q", line)
			}
		}
		return out
	}
	chat := r.has(harness.CapChat)
	var idleSince time.Time
	fired := false
	for {
		if err := sleep(ctx, 700*time.Millisecond); err != nil {
			out.timedOut = true
			break
		}
		working, err := r.workingNow(ctx)
		if err != nil {
			out.errText = err.Error()
			break
		}
		r.mu.Lock()
		out.workingSeen = r.workingSeen
		news := r.newItems(mark)
		r.mu.Unlock()
		if out.workingSeen && onWorking != nil && !fired {
			fired = true
			onWorking()
		}
		final, partial := "", false
		ended := false
		out.seen = out.seen[:0]
		for _, it := range news {
			out.seen = append(out.seen, itemTag(it))
			switch {
			case it.Parent != "":
			case it.Kind == harness.KindAssistant && it.Assistant != nil:
				if it.Assistant.Partial {
					partial = true
				} else {
					final = it.Assistant.Text
				}
			case it.Kind == harness.KindTurn && it.Turn != nil:
				out.turnState = it.Turn.State
				ended = it.Turn.State != harness.StateRunning && it.Turn.State != harness.StateWaiting
				if it.Turn.State == harness.StateFailed && out.errText == "" {
					out.errText = "turn failed"
				}
			case it.Kind == harness.KindNotice && it.Notice != nil && (it.Notice.Level == "error" || it.Notice.Code == harness.NoticeRateLimit):
				// A rate limit is a warning notice; "out of credits" is one too.
				out.errText = it.Notice.Text
			}
		}
		if final != "" {
			out.reply = final
		}
		if stopOnApproval {
			// Only a prompt raised after this send can be this turn's.
			for _, p := range r.pending(ctx) {
				if !p.CreatedAt.Before(out.sentAt.Add(-2 * time.Second)) {
					p := p
					out.approval = &p
					return out
				}
			}
		}
		if working {
			idleSince = time.Time{}
			continue
		}
		if idleSince.IsZero() {
			idleSince = time.Now()
		}
		idle := time.Since(idleSince)
		switch {
		case chat && (ended || (out.reply != "" && out.workingSeen && !partial)) && idle > 1500*time.Millisecond:
			out.cleared = true
			return out
		case chat && out.errText != "" && idle > 3*time.Second:
			out.cleared = true
			return out
		case !chat && out.workingSeen && idle > 3*time.Second:
			out.cleared = true
			out.reply = paneReply(r.capture(ctx), prompt)
			return out
		case time.Since(out.sentAt) > 40*time.Second && !out.workingSeen && out.reply == "":
			// Nothing ever started: read the pane for why.
			out.reply = paneReply(r.capture(ctx), prompt)
			if out.reply == "" {
				out.errText = firstNonEmpty(out.errText, "nothing happened within 40s; pane: "+clip(oneLine(screenTail(r.capture(ctx), 6)), 160))
			}
			out.cleared = true
			return out
		}
	}
	return out
}

// itemTag names an item for a report: its kind, and a turn's state.
func itemTag(it harness.Item) string {
	tag := it.Kind
	if it.Turn != nil {
		tag += "(" + it.Turn.State + ")"
	}
	if it.Parent != "" {
		tag += "^"
	}
	return tag
}

// newItems returns the main conversation's items stored after mark, in
// seq order. Callers hold r.mu.
func (r *runner) newItems(mark int64) []harness.Item {
	var out []harness.Item
	for _, it := range r.items {
		if it.Seq > mark {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// paneReply finds the reply on screen for harnesses without a chat feed:
// the text after the last line that holds the prompt.
func paneReply(pane, prompt string) string {
	key := prompt
	i := strings.LastIndex(pane, key)
	if i < 0 {
		return ""
	}
	rest := pane[i+len(key):]
	if okRE.MatchString(rest) {
		return "ok"
	}
	return ""
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func saysOK(s string) bool {
	return regexp.MustCompile(`(?i)\bok\b`).MatchString(s)
}
