package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

const approvalFile = "conformance-approval.txt"

var autoModes = map[string]bool{"dontAsk": true, "bypassPermissions": true, "yolo": true, "auto": true, "full-auto": true}

// approvalScenario asks for a harmless shell command, expects the
// harness's permission prompt in Needs you for this session, answers it by
// its request id and checks the command ran.
func (r *runner) approvalScenario(ctx context.Context) {
	if !r.has(harness.CapNeedsApprovals) {
		r.set("approval", skip, "adapter declares no approvals")
		return
	}
	// A mode that runs commands without asking would skip the prompt:
	// switch this session to the harness's asking mode first.
	_ = r.describe(ctx)
	if autoModes[r.desc.Mode] {
		for _, c := range r.desc.Modes {
			if c.ID == "default" {
				res, err := r.act(ctx, harness.ActionSetMode, "", map[string]any{"id": "default"})
				r.note("approval: switched %s → default first (%s %v)", r.desc.Mode, res.Result, err)
				r.afterAct(ctx, res, "approval", "set_mode default")
				_ = r.describe(ctx)
			}
		}
	}
	target := filepath.Join(r.sb.work, approvalFile)
	_ = os.Remove(target)
	defer os.Remove(target)
	prompt := "Use your shell tool to run exactly this command: touch " + approvalFile + " — then reply with the word done."
	t := r.turn(ctx, prompt, true, nil)
	if t.approval == nil {
		_ = r.waitIdle(ctx, 20*time.Second)
		switch {
		case exists(target):
			r.set("approval", skip, "the command ran without asking (mode %q allows it)", r.desc.Mode)
		case blockRE.MatchString(t.errText):
			r.set("approval", skip, "blocked: %s", clip(t.errText, 100))
		default:
			r.set("approval", fail, "no approval reached Needs you and the command did not run; reply %q; err %q; pane: %s",
				clip(oneLine(t.reply), 50), t.errText, clip(oneLine(screenTail(r.capture(ctx), 5)), 140))
		}
		return
	}
	a := t.approval
	about := strings.ToLower(a.Title + " " + a.Snippet + " " + subjectText(a.Subject))
	if !strings.Contains(about, "touch") && !strings.Contains(about, approvalFile) && !strings.Contains(about, "bash") &&
		!strings.Contains(about, "shell") && !strings.Contains(about, "command") && !strings.Contains(about, "run") {
		// Never answer a prompt the suite did not cause.
		r.set("approval", fail, "a prompt that is not about the command reached Needs you instead: %q (left unanswered)", clip(oneLine(a.Title), 70))
		return
	}
	if a.SessionID == nil || *a.SessionID != r.sid {
		r.set("approval", fail, "approval %s listed under session %v, not %s", a.RequestID, a.SessionID, r.sid)
		return
	}
	// The chat item for the same request, when the adapter has a chat.
	itemOK := !r.has(harness.CapChat)
	for i := 0; i < 10 && !itemOK; i++ {
		r.mu.Lock()
		for _, it := range r.items {
			if it.Kind == harness.KindApproval && it.Approval != nil && it.Approval.RequestID == a.RequestID {
				itemOK = true
			}
		}
		r.mu.Unlock()
		_ = sleep(ctx, 500*time.Millisecond)
	}
	// An answer to an id nobody asked must not reach any prompt.
	if err := r.call(ctx, wire.MethodApprovalAnswer, map[string]string{"requestId": "conformance-unknown-request", "decision": "deny"}, nil); err == nil {
		r.set("approval", fail, "approval.answer accepted an unknown request id")
		return
	}
	if a.Answerable != nil && !*a.Answerable {
		r.set("approval", fail, "approval %q is not answerable from the phone (options %d)", clip(a.Title, 50), len(a.Options))
		return
	}
	if err := r.call(ctx, wire.MethodApprovalAnswer, map[string]string{"requestId": a.RequestID, "decision": "approve"}, nil); err != nil {
		r.set("approval", fail, "approval.answer %s: %v", a.RequestID, err)
		return
	}
	answered := map[string]bool{a.RequestID: true}
	gone, ran := false, false
	end := time.Now().Add(90 * time.Second)
	for time.Now().Before(end) && !(gone && ran) {
		if sleep(ctx, time.Second) != nil {
			break
		}
		gone = true
		for _, p := range r.pending(ctx) {
			if p.RequestID == a.RequestID {
				gone = false
				continue
			}
			// A follow-up prompt for the same command is answered too;
			// anything else is denied.
			if !answered[p.RequestID] {
				answered[p.RequestID] = true
				d := "deny"
				if strings.Contains(p.Title+p.Snippet+subjectText(p.Subject), approvalFile) {
					d = "approve"
				}
				_ = r.call(ctx, wire.MethodApprovalAnswer, map[string]string{"requestId": p.RequestID, "decision": d}, nil)
			}
		}
		ran = exists(target)
	}
	_ = r.waitIdle(ctx, 30*time.Second)
	switch {
	case !gone:
		r.set("approval", fail, "answered %s from the phone but it is still pending", short8(a.RequestID))
	case !ran:
		r.set("approval", fail, "answered %s (%q) but the command never ran; pane: %s", short8(a.RequestID), clip(a.Title, 40), clip(oneLine(screenTail(r.capture(ctx), 4)), 120))
	case !itemOK:
		r.set("approval", fail, "approval worked, but no chat item carries request %s", short8(a.RequestID))
	default:
		r.set("approval", pass, "%q (%d options) listed for this session, answered by id %s, command ran", clip(oneLine(a.Title), 40), len(a.Options), short8(a.RequestID))
	}
}

func subjectText(s *wire.Subject) string {
	if s == nil {
		return ""
	}
	return s.Text
}

func short8(s string) string { return clip(s, 8) }

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// waitIdle waits for the mission summary to say not working.
func (r *runner) waitIdle(ctx context.Context, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if w, err := r.workingNow(ctx); err == nil && !w {
			return true
		}
		if sleep(ctx, time.Second) != nil {
			return false
		}
	}
	return false
}

// interruptScenario starts a long answer and stops it with
// harness.interrupt; working must clear.
func (r *runner) interruptScenario(ctx context.Context) {
	if !r.has(harness.CapInterrupt) {
		r.set("interrupt", skip, "adapter declares no interrupt")
		return
	}
	var (
		mu     sync.Mutex
		res    harness.ActionResult
		ierr   error
		sentAt time.Time
	)
	done := make(chan struct{})
	fire := func() {
		go func() {
			defer close(done)
			_ = sleep(ctx, 2500*time.Millisecond)
			mu.Lock()
			sentAt = time.Now()
			mu.Unlock()
			var out harness.ActionResult
			err := r.call(ctx, harness.MethodInterrupt, map[string]string{"id": r.sid}, &out)
			mu.Lock()
			res, ierr = out, err
			mu.Unlock()
		}()
	}
	t := r.turn(ctx, "Count from 1 to 3000, one number per line, with no other text.", false, fire)
	if !t.workingSeen {
		r.set("interrupt", fail, "never saw the session working, so nothing to interrupt (%s)", firstNonEmpty(t.errText, "no error"))
		return
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
	}
	mu.Lock()
	defer mu.Unlock()
	if sentAt.IsZero() {
		r.set("interrupt", fail, "interrupt was never sent")
		return
	}
	if ierr != nil {
		r.set("interrupt", fail, "harness.interrupt: %v", ierr)
		return
	}
	stopped := t.sentAt.Add(t.dur).Sub(sentAt)
	if t.cleared && stopped < 0 {
		r.set("interrupt", skip, "turn finished before the interrupt was sent")
		return
	}
	if !t.cleared {
		r.set("interrupt", fail, "harness.interrupt → %s, but still working %.0fs later", res.Result, time.Since(sentAt).Seconds())
		return
	}
	if !actOK(res) {
		r.set("interrupt", fail, "working cleared but harness.interrupt answered %s %s", res.Result, res.Message)
		return
	}
	if stopped > 20*time.Second {
		r.set("interrupt", fail, "working cleared only %.0fs after the interrupt (%s)", stopped.Seconds(), res.Result)
		return
	}
	r.set("interrupt", pass, "%s; working cleared %.1fs later (turn state %q)", res.Result, stopped.Seconds(), t.turnState)
}

var harmlessCommands = []string{"cost", "context", "usage", "stats", "tokens", "status", "about", "version", "help"}

// slashScenario sends a harmless typed slash command and checks it was
// submitted, not left in the input line.
func (r *runner) slashScenario(ctx context.Context) {
	if !r.has(harness.CapCommands) {
		r.set("slash", skip, "adapter declares no commands")
		return
	}
	var list harness.CompleteResult
	if err := r.call(ctx, harness.MethodComplete, map[string]any{"id": r.sid, "kind": "command", "query": "", "limit": 50}, &list); err != nil {
		r.set("slash", fail, "harness.complete: %v", err)
		return
	}
	cmd := ""
	for _, want := range harmlessCommands {
		for _, c := range list.Items {
			name := strings.TrimSpace(strings.TrimPrefix(c.Insert, "/"))
			if name == want && c.Screen == "" && c.Action == "" && c.Opens == "" && c.Kind != "terminal" && cmd == "" {
				cmd = "/" + want
			}
		}
	}
	if cmd == "" {
		cmd = "/help" // every harness has one; the list may hold only the adapter's own entries
	}
	if r.inputRE == nil {
		r.set("slash", skip, "adapter declares no input line to check %s against", cmd)
		return
	}
	_ = r.waitIdle(ctx, 20*time.Second)
	if err := r.call(ctx, harness.MethodSend, map[string]any{"id": r.sid, "text": cmd, "mode": "now"}, nil); err != nil {
		r.set("slash", fail, "harness.send %s: %v", cmd, err)
		return
	}
	_ = sleep(ctx, 4*time.Second)
	pane := r.capture(ctx)
	line, found := r.inputLine(pane)
	// Close whatever the command opened.
	_, _ = r.sb.tmux(ctx, "send-keys", "-t", "="+r.sid+":", "Escape")
	stuck := r.stuckWorking(ctx, cmd)
	switch {
	case stuck:
		r.set("slash", fail, "%s submitted, but mission.working then stayed true at an idle prompt", cmd)
	case found && strings.Contains(line, cmd):
		r.set("slash", fail, "%s typed but not submitted: input line still reads %q", cmd, clip(line, 60))
	case !strings.Contains(pane, cmd):
		r.set("slash", fail, "%s sent but it appears nowhere on screen", cmd)
	default:
		r.set("slash", pass, "%s submitted (input line empty, command on screen)", cmd)
	}
}

// inputLine returns the text in the harness's input line, from the last
// line matching the adapter's pattern.
func (r *runner) inputLine(pane string) (string, bool) {
	if r.inputRE == nil {
		return "", false
	}
	lines := strings.Split(screenTail(pane, 40), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimRight(strings.ReplaceAll(lines[i], "\u00a0", " "), " ")
		m := r.inputRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		text := ""
		if len(m) > 1 {
			text = strings.TrimSpace(m[1])
		}
		for _, e := range r.emptyRE {
			if e.MatchString(text) {
				text = ""
			}
		}
		return text, true
	}
	return "", false
}

// restoreScenario kills the sandbox's tmux server (what a host restart
// does), restores the session from the manifest and, where the agent can
// resume, checks the conversation continued.
func (r *runner) restoreScenario(ctx context.Context) {
	if strings.HasPrefix(r.blocked, "agent exits at launch") {
		r.set("restore", skip, "the agent does not stay up: %s", r.blocked)
		return
	}
	r.sb.killServer()
	var found *wire.RestorableSession
	for i := 0; i < 15 && found == nil; i++ {
		_ = sleep(ctx, time.Second)
		var res wire.RestorableResult
		if err := r.call(ctx, wire.MethodSessionRestorable, map[string]any{}, &res); err != nil {
			continue
		}
		for i := range res.Sessions {
			if res.Sessions[i].ID == r.sid {
				found = &res.Sessions[i]
			}
		}
	}
	if found == nil {
		r.set("restore", fail, "session not offered by session.restorable after its tmux server died")
		return
	}
	var s session
	if err := r.call(ctx, wire.MethodSessionRestore, map[string]any{"id": r.sid, "resume": found.Resumable}, &s); err != nil {
		r.set("restore", fail, "session.restore: %v", err)
		return
	}
	if !s.Muted {
		r.set("restore", fail, "restored session is not muted (pushes would reach the phone)")
	}
	blockedBefore := r.blocked
	r.blocked = ""
	if err := r.settle(ctx, 60*time.Second); err != nil {
		r.set("restore", fail, "restored (argv %v), then: %v; pane: %s", s.Command, err, clip(oneLine(screenTail(r.capture(ctx), 4)), 140))
		return
	}
	_ = r.describe(ctx)
	_ = r.open(ctx)
	if r.blocked == "" {
		r.blocked = blockedBefore
	}
	if s2, _ := r.session(ctx); s2 != nil && !s2.Muted {
		r.set("restore", fail, "restored session is not muted after start")
	}
	if !found.Resumable {
		r.set("restore", pass, "restored and running (agent declares no resume flag, started fresh)")
		return
	}
	if r.blocked != "" || !r.has(harness.CapChat) {
		r.set("restore", pass, "restored with resume and running (conversation not checked: %s)", firstNonEmpty(r.blocked, "no chat"))
		return
	}
	t := r.turn(ctx, "What single word did you reply with in your first answer in this conversation? Reply with only that word.", false, nil)
	if saysOK(t.reply) {
		r.set("restore", pass, "restored with resume; the conversation continued (recalled %q)", clip(oneLine(t.reply), 20))
		return
	}
	r.set("restore", fail, "restored with resume (argv %v) but the agent did not recall the conversation: reply %q %s",
		s.Command, clip(oneLine(t.reply), 40), t.errText)
}
