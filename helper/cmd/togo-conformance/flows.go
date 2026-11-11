package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

const approvalFile = "conformance-approval.txt"

// askingModes are the modes that ask before running a command, in the
// order tried (Claude's default, Goose's approve, Codex's on-request).
var askingModes = []string{"default", "approve", "on-request"}

var autoModes = map[string]bool{"dontAsk": true, "bypassPermissions": true, "yolo": true, "auto": true, "full-auto": true}

// ensureAsking switches a session in a mode that runs without asking (which
// would skip a permission prompt, and in Claude denies AskUserQuestion) to
// the harness's asking mode first.
func (r *runner) ensureAsking(ctx context.Context, scenario string) {
	_ = r.describe(ctx)
	if !autoModes[r.desc.Mode] {
		return
	}
	ids := map[string]bool{}
	for _, c := range r.desc.Modes {
		ids[c.ID] = true
	}
	for _, ask := range askingModes {
		if !ids[ask] {
			continue
		}
		res, err := r.act(ctx, harness.ActionSetMode, "", map[string]any{"id": ask})
		r.note("%s: switched %s → %s first (%s %v)", scenario, r.desc.Mode, ask, res.Result, err)
		r.afterAct(ctx, res, scenario, "set_mode "+ask)
		_ = r.describe(ctx)
		return
	}
}

// approvalScenario asks for a harmless shell command, expects the
// harness's permission prompt in Needs you for this session, answers it by
// its request id and checks the command ran.
func (r *runner) approvalScenario(ctx context.Context) {
	if !r.has(harness.CapNeedsApprovals) {
		r.set("approval", skip, "adapter declares no approvals")
		return
	}
	r.ensureAsking(ctx, "approval")
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
				clip(oneLine(t.reply), 50), t.errText, clip(oneLine(screenTail(r.captureLate(ctx), 5)), 140))
		}
		return
	}
	a := t.approval
	// A hook can raise the request before the agent draws its prompt; it
	// is answerable once the watcher reads the prompt's options.
	for i := 0; i < 20 && a.Answerable != nil && !*a.Answerable; i++ {
		if sleep(ctx, 500*time.Millisecond) != nil {
			break
		}
		for _, p := range r.pending(ctx) {
			if p.RequestID == a.RequestID {
				p := p
				a = &p
			}
		}
	}
	about := strings.ToLower(a.Title + " " + a.Snippet + " " + subjectText(a.Subject))
	if !strings.Contains(about, "touch") && !strings.Contains(about, approvalFile) && !strings.Contains(about, "bash") &&
		!strings.Contains(about, "shell") && !strings.Contains(about, "command") && !strings.Contains(about, "run") {
		// Never answer a prompt the suite did not cause.
		r.set("approval", fail, "a prompt that is not about the command reached Needs you instead: %q (%s, %d options; left unanswered); pane: %s",
			clip(oneLine(a.Title), 70), a.Source, len(a.Options), clip(oneLine(screenTail(r.captureLate(ctx), 8)), 200))
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
		r.set("approval", fail, "approval %q is not answerable from the phone (%s, options %d); pane: %s", clip(a.Title, 50), a.Source, len(a.Options), clip(oneLine(screenTail(r.captureLate(ctx), 8)), 200))
		return
	}
	if err := r.call(ctx, wire.MethodApprovalAnswer, map[string]string{"requestId": a.RequestID, "decision": "approve"}, nil); err != nil {
		// The engine offers an ask for the agent's own permission record
		// when the watcher has not read the dialog yet; the dialog's
		// prompt takes over once read, closing the offer. Answer the one
		// that took over, as the phone's card (the question's) would.
		next := r.takeOver(ctx, a, t.sentAt)
		if next == nil {
			r.set("approval", fail, "approval.answer %s (%s %q): %v", a.RequestID, a.Source, clip(oneLine(a.Title), 40), err)
			return
		}
		r.note("approval: the %s request %s was taken over by the %s prompt %s (%v)", a.Source, short8(a.RequestID), next.Source, short8(next.RequestID), err)
		a = next
		if err := r.call(ctx, wire.MethodApprovalAnswer, map[string]string{"requestId": a.RequestID, "decision": "approve"}, nil); err != nil {
			r.set("approval", fail, "approval.answer %s (%s %q): %v", a.RequestID, a.Source, clip(oneLine(a.Title), 40), err)
			return
		}
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
			r.followUp(ctx, p, answered)
		}
		ran = exists(target)
	}
	// The agent may go on to check its work (Cline runs `ls` on the file),
	// asking again: those prompts are answered until it is idle, so none
	// is left waiting for the next scenario.
	quiet := 0
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end) && quiet < 3; {
		if sleep(ctx, time.Second) != nil {
			break
		}
		open := false
		for _, p := range r.pending(ctx) {
			if p.SessionID != nil && *p.SessionID == r.sid {
				open = true
				r.followUp(ctx, p, answered)
			}
		}
		if w, err := r.workingNow(ctx); err == nil && !w && !open {
			quiet++
		} else {
			quiet = 0
		}
	}
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

// followUp answers a prompt that came after the approval: one about the
// same file is approved, anything else denied. Each is answered once.
func (r *runner) followUp(ctx context.Context, p wire.Approval, answered map[string]bool) {
	if answered[p.RequestID] {
		return
	}
	answered[p.RequestID] = true
	d := "deny"
	if strings.Contains(p.Title+p.Snippet+subjectText(p.Subject), approvalFile) {
		d = "approve"
	}
	r.note("approval: follow-up prompt %q answered %s", clip(oneLine(p.Title+" "+subjectText(p.Subject)), 60), d)
	_ = r.call(ctx, wire.MethodApprovalAnswer, map[string]string{"requestId": p.RequestID, "decision": d}, nil)
}

// takeOver finds the prompt that replaced approval a: another request of
// this session raised since the turn was sent, pending now.
func (r *runner) takeOver(ctx context.Context, a *wire.Approval, since time.Time) *wire.Approval {
	for i := 0; i < 10; i++ {
		for _, p := range r.pending(ctx) {
			if p.RequestID != a.RequestID && p.SessionID != nil && *p.SessionID == r.sid && !p.CreatedAt.Before(since.Add(-2*time.Second)) {
				p := p
				return &p
			}
		}
		if sleep(ctx, 500*time.Millisecond) != nil {
			break
		}
	}
	return nil
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
		r.mu.Lock()
		w := r.working
		r.mu.Unlock()
		if w {
			r.set("interrupt", fail, "harness.interrupt → %s, but still working %.0fs later; pane: %s", res.Result, time.Since(sentAt).Seconds(), clip(oneLine(screenTail(r.captureLate(ctx), 4)), 120))
		} else {
			r.set("interrupt", fail, "harness.interrupt → %s; working cleared, but the turn never ended in the chat (turn state %q)", res.Result, t.turnState)
		}
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
	prior := r.capture(ctx)
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
	case !strings.Contains(pane, cmd) && opened(prior, pane, cmd):
		r.set("slash", pass, "%s submitted (input line empty; it opened its own view)", cmd)
	case !strings.Contains(pane, cmd):
		r.set("slash", fail, "%s sent but it appears nowhere on screen", cmd)
	default:
		r.set("slash", pass, "%s submitted (input line empty, command on screen)", cmd)
	}
}

// dumpItems writes the session's items, oldest first, next to its home in
// the scratch root (kept with --keep), for a failure the report cannot hold.
func (r *runner) dumpItems(scenario string) {
	r.mu.Lock()
	list := make([]harness.Item, 0, len(r.items))
	for _, it := range r.items {
		list = append(list, it)
	}
	r.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].Seq < list[j].Seq })
	var b strings.Builder
	for _, it := range list {
		line, _ := json.Marshal(it)
		b.Write(line)
		b.WriteByte('\n')
	}
	_ = os.WriteFile(filepath.Join(filepath.Dir(r.sb.home), r.id+"-"+scenario+"-items.jsonl"), []byte(b.String()), 0o600)
}

// opened reports whether a command that is not echoed (omp's /context,
// Claude's /help) drew a view of its own: a line new since prior is headed
// by its name, as a box title ("╭─ Context · Claude Haiku 4.5 ─") or a tab
// row ("Help  General  Commands"). A line that merely starts a sentence
// with it ("Context left until auto-compact: 5%") is not.
func opened(prior, pane, cmd string) bool {
	name := regexp.QuoteMeta(strings.TrimPrefix(cmd, "/"))
	header := regexp.MustCompile(`(?i)^[\s╭┌╔│┃─━═]*/?` + name + `(\s*$|\s{2,}|\s*[·─━═│┃:(])`)
	was := map[string]bool{}
	for _, l := range strings.Split(prior, "\n") {
		was[strings.TrimSpace(l)] = true
	}
	for _, l := range strings.Split(pane, "\n") {
		if t := strings.TrimSpace(l); t != "" && !was[t] && header.MatchString(l) {
			return true
		}
	}
	return false
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
	r.dumpItems("restore")
	r.set("restore", fail, "restored with resume (argv %v) but the agent did not recall the conversation: reply %q %s; items %v; pane: %s",
		s.Command, clip(oneLine(t.reply), 40), t.errText, t.seen, clip(oneLine(screenTail(r.captureLate(ctx), 6)), 120))
}
