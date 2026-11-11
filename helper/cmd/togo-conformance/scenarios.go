package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Results.
const (
	pass = "PASS"
	fail = "FAIL"
	skip = "SKIP"
)

type result struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// scenarios in report order.
var scenarios = []string{
	"create", "trust", "signin", "first_turn", "working", "model_switch", "mode", "effort",
	"approval", "question", "interrupt", "slash", "restore", "close",
}

const okPrompt = "Reply with only the word ok."

// flagErr: an agent that exits complaining about its arguments was
// launched wrong by Togo.
var flagErr = regexp.MustCompile(`(?i)(unknown (option|flag|argument)|unrecognized (option|flag|argument)|invalid (option|flag)|usage:)`)

func (r *runner) set(name, status, format string, a ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, done := r.res[name]; done {
		return
	}
	r.res[name] = result{Status: status, Reason: clip(oneLine(fmt.Sprintf(format, a...)), 240)}
}

func (r *runner) want(name string) bool {
	return len(r.opts.scenarios) == 0 || r.opts.scenarios[name]
}

// phase runs fn with its own timeout under the harness deadline.
func phase(ctx context.Context, d time.Duration, fn func(context.Context)) {
	c, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	fn(c)
}

// run drives every scenario on one session, in an order that lets later
// scenarios reuse what earlier ones set up.
func (r *runner) run(ctx context.Context) {
	defer func() {
		for _, s := range scenarios {
			r.set(s, skip, "not reached (an earlier step stopped the run)")
		}
	}()
	if !r.create(ctx) {
		phase(context.WithoutCancel(ctx), 30*time.Second, r.closeSession)
		return
	}
	r.set("trust", skip, "no folder-trust prompt")
	var m modelPlan
	if r.blocked == "" {
		phase(ctx, 150*time.Second, func(c context.Context) { m = r.modelSetup(c) })
	}
	t1 := r.firstTurn(ctx)
	r.judgeSignin(ctx)
	if r.blocked != "" {
		for _, s := range []string{"working", "model_switch", "mode", "effort", "approval", "question", "interrupt", "slash"} {
			r.set(s, skip, "blocked: %s", r.blocked)
		}
	} else {
		r.judgeWorking(t1)
		m.turn1 = truthFiles(r.id, r.marker, r.start)
		m.model1, m.strong1 = modelSince(m.turn1, nil)
		var mode modePlan
		var eff effortPlan
		if r.want("mode") {
			phase(ctx, 45*time.Second, func(c context.Context) { mode = r.modeChange(c) })
		}
		if r.want("effort") {
			phase(ctx, 90*time.Second, func(c context.Context) { eff = r.effortChange(c) })
		}
		if r.want("model_switch") {
			phase(ctx, 300*time.Second, func(c context.Context) { r.modelSwitch(c, &m) })
		}
		if !m.turned && (mode.target != "" || eff.target != "") {
			// Mode and effort must hold across a turn even without a model switch.
			phase(ctx, 120*time.Second, func(c context.Context) { r.turn(c, okPrompt, false, nil) })
		}
		if mode.target != "" {
			phase(ctx, 45*time.Second, func(c context.Context) { r.modePersist(c, mode) })
		}
		if eff.target != "" {
			phase(ctx, 60*time.Second, func(c context.Context) { r.effortPersist(c, eff) })
		}
		if r.want("approval") {
			phase(ctx, 150*time.Second, r.approvalScenario)
		}
		if r.want("question") {
			phase(ctx, 240*time.Second, r.questionScenario)
		}
		if r.want("interrupt") {
			phase(ctx, 100*time.Second, r.interruptScenario)
		}
		if r.want("slash") {
			phase(ctx, 40*time.Second, r.slashScenario)
		}
	}
	if r.want("restore") {
		phase(ctx, 180*time.Second, r.restoreScenario)
	}
	phase(context.WithoutCancel(ctx), 30*time.Second, r.closeSession)
}

func (r *runner) create(ctx context.Context) bool {
	ok := false
	phase(ctx, 90*time.Second, func(c context.Context) {
		var s session
		params := map[string]any{"agent": r.id, "cwd": r.sb.work, "muted": true, "cols": 100, "rows": 40}
		if a := r.opts.args[r.id]; len(a) > 0 {
			params["args"] = a
			r.note("launched with %s", strings.Join(a, " "))
		}
		err := r.call(c, "session.create", params, &s)
		if err != nil {
			r.set("create", fail, "session.create: %v", err)
			return
		}
		r.sid = s.ID
		if !s.Muted {
			r.set("create", fail, "session.create with muted:true returned muted:false")
			_ = r.call(c, "session.setMute", map[string]any{"id": r.sid, "muted": true}, nil)
		}
		for i := 0; i < 10; i++ {
			if err := r.describe(c); err == nil && r.desc.Adapter != nil {
				break
			}
			_ = sleep(c, time.Second)
		}
		terminalOnly := ""
		if err := r.open(c); err != nil && r.desc.Adapter != nil {
			var re *rpcError
			if !errors.As(err, &re) || re.Code != -32008 {
				r.set("create", fail, "harness.open: %v", err)
				return
			}
			// The adapter refused this harness version (or its sources):
			// the session is terminal only, by design.
			terminalOnly = clip(string(re.Data), 80)
			r.desc.Capabilities = nil
		}
		if err := r.settle(c, 60*time.Second); err != nil {
			pane := screenTail(r.capture(c), 4)
			if !flagErr.MatchString(pane) {
				// The agent itself is not set up; Togo started it fine.
				r.blocked = "agent exits at launch: " + clip(oneLine(pane), 100)
				r.set("create", pass, "session created muted; %v (the agent's own problem, not its launch flags)", err)
				ok = true
				return
			}
			r.set("create", fail, "%v; pane: %s", err, clip(oneLine(pane), 120))
			return
		}
		if terminalOnly != "" {
			r.set("create", pass, "running muted; terminal only (harness.open refused: %s)", terminalOnly)
			ok = true
			return
		}
		_ = r.describe(c) // the version gate may have finished meanwhile
		if s2, _ := r.session(c); s2 != nil && !s2.Muted {
			r.set("create", fail, "session is not muted after start")
		}
		ok = true
		if r.desc.Adapter == nil {
			r.set("create", pass, "running muted; no adapter (terminal only)")
			return
		}
		deg := ""
		if len(r.desc.Degraded) > 0 {
			deg = "; degraded: " + strings.Join(r.desc.Degraded, ",")
		}
		r.set("create", pass, "running muted; adapter %s %s, %d capabilities%s",
			r.desc.Adapter.ID, r.desc.Adapter.HarnessVersion, len(r.desc.Capabilities), deg)
	})
	return ok
}

func (r *runner) firstTurn(ctx context.Context) turnOut {
	var t turnOut
	if r.blocked != "" {
		r.set("first_turn", skip, "blocked: %s", r.blocked)
		return t
	}
	if r.desc.Adapter == nil || len(r.desc.Capabilities) == 0 {
		// Nothing tells what is on screen: typing blind could land in a
		// settings or setup dialog.
		r.blocked = "terminal only (no adapter capabilities); turns are not driven"
		r.set("first_turn", skip, "%s", r.blocked)
		return t
	}
	if l := lineMatching(screenTail(r.capture(ctx), 20), setupDialog); l != "" {
		r.blocked = "setup dialog on screen (nothing typed): " + clip(oneLine(l), 90)
		r.set("first_turn", skip, "blocked: %s", r.blocked)
		return t
	}
	// Never type into a sign-in prompt: a prompt starting with "y" would
	// answer it.
	if l := lineMatching(screenTail(r.capture(ctx), 20), loginPrompt); l != "" {
		r.blocked = "sign-in prompt on screen (nothing typed): " + clip(oneLine(l), 90)
		r.set("first_turn", skip, "blocked: %s", r.blocked)
		return t
	}
	phase(ctx, 120*time.Second, func(c context.Context) {
		t = r.turn(c, okPrompt, false, nil)
		if saysOK(t.reply) {
			r.set("first_turn", pass, "replied %q in %.0fs", clip(oneLine(t.reply), 20), t.dur.Seconds())
			return
		}
		if t.timedOut && t.workingSeen && !t.cleared {
			bg, cancel := context.WithTimeout(context.WithoutCancel(c), 10*time.Second)
			defer cancel()
			a := r.capture(bg)
			_ = sleep(bg, 3*time.Second)
			if m := blockRE.FindString(a); m != "" {
				r.blocked = "provider unreachable: " + clip(oneLine(lineWith(a, m)), 100)
				r.set("first_turn", skip, "blocked: %s", r.blocked)
				return
			}
			if b := r.capture(bg); a != b {
				// Still producing output: a slow or busy model, not Togo.
				r.blocked = fmt.Sprintf("model too slow: no reply within %s, agent still busy", t.dur.Round(time.Second))
				r.set("first_turn", skip, "blocked: %s", r.blocked)
				return
			}
		}
		why := firstNonEmpty(t.errText, "no reply")
		if t.timedOut {
			why = "timed out after " + t.dur.Round(time.Second).String() + "; " + why
		}
		bg, cancel := context.WithTimeout(context.WithoutCancel(c), 10*time.Second)
		defer cancel()
		pane := screenTail(r.capture(bg), 12)
		for _, a := range r.pending(bg) {
			if blockRE.MatchString(a.Title+" "+a.Snippet) || a.SignIn != nil {
				a := a
				r.signin = &a
			}
		}
		evidence := why + " " + t.reply
		if m := blockRE.FindString(evidence); m != "" || r.signin != nil {
			r.blocked = "account/provider: " + clip(oneLine(evidence), 110)
		} else if m := blockRE.FindString(pane); m != "" && t.reply == "" {
			r.blocked = "account/provider (pane): " + clip(oneLine(lineWith(pane, m)), 110)
		}
		if r.blocked != "" {
			r.set("first_turn", skip, "blocked: %s", r.blocked)
			return
		}
		r.set("first_turn", fail, "%s; reply %q; pane: %s", why, clip(oneLine(t.reply), 40), clip(oneLine(screenTail(pane, 4)), 120))
	})
	return t
}

func lineWith(text, sub string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return sub
}

// judgeSignin: a harness that is not signed in must put its sign-in
// prompt in Needs you; one whose pane asks for a login with nothing in
// Needs you fails.
func (r *runner) judgeSignin(ctx context.Context) {
	switch {
	case r.signin != nil:
		kind := "plain prompt"
		if r.signin.SignIn != nil {
			kind = "sign-in step"
			if r.signin.SignIn.URL != "" {
				kind += " with URL"
			}
		}
		r.set("signin", pass, "reached Needs you as %s: %q", kind, clip(oneLine(r.signin.Title), 60))
	case r.blocked == "":
		r.set("signin", skip, "signed in (no sign-in prompt)")
	case strings.HasPrefix(r.blocked, "sign-in prompt on screen"):
		// The prompt asks a question: it must reach Needs you. Detection
		// can lag a redraw, so give it 20 s.
		for i := 0; i < 20; i++ {
			for _, a := range r.pending(ctx) {
				a := a
				r.signin = &a
				r.set("signin", pass, "reached Needs you after a delay: %q", clip(oneLine(a.Title), 60))
				return
			}
			if sleep(ctx, time.Second) != nil {
				break
			}
		}
		r.set("signin", fail, "sign-in prompt on screen for 20s+ never reached Needs you: %s", strings.TrimPrefix(r.blocked, "sign-in prompt on screen (nothing typed): "))
	default:
		r.set("signin", skip, "not signed in or provider unusable (no prompt to answer): %s", r.blocked)
	}
}

func (r *runner) judgeWorking(t turnOut) {
	if r.desc.Adapter == nil {
		r.set("working", skip, "no adapter")
		return
	}
	switch {
	case t.sendErr != nil:
		r.set("working", skip, "first turn not sent")
	case !t.workingSeen:
		r.set("working", fail, "mission summary never said working during a %.0fs turn", t.dur.Seconds())
	case !t.cleared:
		r.set("working", fail, "still working when the turn ended (turn state %q, reply %q)", t.turnState, clip(t.reply, 30))
	default:
		r.set("working", pass, "set during the turn, cleared at its end (turn state %q)", t.turnState)
	}
}

func (r *runner) closeSession(ctx context.Context) {
	if r.sid == "" {
		r.set("close", skip, "no session")
		return
	}
	if err := r.call(ctx, "session.kill", map[string]string{"id": r.sid}, nil); err != nil {
		r.set("close", fail, "session.kill: %v", err)
		return
	}
	for i := 0; i < 20; i++ {
		s, err := r.session(ctx)
		if err == nil && s == nil {
			if _, err := r.sb.tmux(ctx, "has-session", "-t", "="+r.sid); err != nil {
				r.set("close", pass, "killed; gone from session.list and tmux")
				return
			}
		}
		_ = sleep(ctx, 500*time.Millisecond)
	}
	r.set("close", fail, "session still listed or its tmux session alive 10s after session.kill")
}
