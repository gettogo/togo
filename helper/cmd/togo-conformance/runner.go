package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
	"gopkg.in/yaml.v3"
)

// runner drives one harness through the scenarios on one real session.
type runner struct {
	id     string
	sb     *sandbox
	opts   *options
	c      *client
	sid    string
	desc   harness.Descriptor
	marker string // the session folder's name, which the harness's records carry
	start  time.Time

	inputRE *regexp.Regexp
	emptyRE []*regexp.Regexp

	mu          sync.Mutex
	items       map[string]harness.Item
	seq         int64
	working     bool
	workingSeen bool
	status      *harness.StatusBlock

	blocked string         // why turns cannot run (login, provider, update prompt)
	signin  *wire.Approval // a sign-in prompt that reached Needs you
	notes   []string       // context lines for the report
	res     map[string]result
	version string
}

type session struct {
	ID      string               `json:"id"`
	Agent   string               `json:"agent"`
	Status  string               `json:"status"`
	Muted   bool                 `json:"muted"`
	Mission *wire.MissionSummary `json:"mission"`
	Exit    *wire.SessionExit    `json:"exit"`
	Command []string             `json:"command"`
}

func (r *runner) note(format string, a ...any) {
	r.mu.Lock()
	r.notes = append(r.notes, fmt.Sprintf(format, a...))
	r.mu.Unlock()
}

// onNotify keeps the items and mission summary of the session.
func (r *runner) onNotify(method string, params json.RawMessage) {
	if method != harness.NotificationMethod {
		return
	}
	var n harness.Notification
	if json.Unmarshal(params, &n) != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if n.SessionID != r.sid {
		return
	}
	for _, it := range n.Items {
		r.items[it.ID] = it
		if it.Seq > r.seq {
			r.seq = it.Seq
		}
	}
	if n.Mission != nil {
		for _, op := range n.Mission.Ops {
			switch op.Op {
			case harness.OpSummary:
				if op.Summary != nil {
					r.working = op.Summary.Working
					if r.working {
						r.workingSeen = true
					}
				}
			case harness.OpStatus:
				r.status = op.Status
			}
		}
	}
}

func (r *runner) call(ctx context.Context, method string, params, out any) error {
	return r.c.call(ctx, method, params, out)
}

// open (re)subscribes to the session's items and mission.
func (r *runner) open(ctx context.Context) error {
	var res harness.OpenResult
	if err := r.call(ctx, harness.MethodOpen, map[string]any{"id": r.sid, "after": 0, "limit": 500}, &res); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, it := range res.Items {
		r.items[it.ID] = it
		if it.Seq > r.seq {
			r.seq = it.Seq
		}
	}
	r.working = res.Mission.Summary.Working
	for _, s := range res.Mission.Sections {
		if s.Status != nil {
			r.status = s.Status
		}
	}
	return nil
}

func (r *runner) describe(ctx context.Context) error {
	var d harness.Descriptor
	if err := r.call(ctx, harness.MethodDescribe, map[string]string{"id": r.sid}, &d); err != nil {
		return err
	}
	r.desc = d
	if d.Adapter != nil {
		r.version = d.Adapter.HarnessVersion
		r.loadInput(d.Adapter.ID)
	}
	return nil
}

func (r *runner) has(c string) bool { return harness.Has(r.desc.Capabilities, c) }

func (r *runner) hasScreen(id string) (harness.ScreenRef, bool) {
	for _, s := range r.desc.Screens {
		if s.ID == id {
			return s, true
		}
	}
	return harness.ScreenRef{}, false
}

// loadInput reads the adapter file's input line patterns, used to check
// that typed text was submitted rather than left in the input line.
func (r *runner) loadInput(adapter string) {
	if r.inputRE != nil || r.opts.adapters == "" {
		return
	}
	files, _ := filepath.Glob(filepath.Join(r.opts.adapters, "*", adapter+".yaml"))
	if len(files) == 0 {
		return
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		return
	}
	var f struct {
		Input *struct {
			Line  string   `yaml:"line"`
			Empty []string `yaml:"empty"`
		} `yaml:"input"`
	}
	if yaml.Unmarshal(b, &f) != nil || f.Input == nil || f.Input.Line == "" {
		return
	}
	if re, err := regexp.Compile(f.Input.Line); err == nil {
		r.inputRE = re
	}
	for _, e := range f.Input.Empty {
		if re, err := regexp.Compile(e); err == nil {
			r.emptyRE = append(r.emptyRE, re)
		}
	}
}

func (r *runner) session(ctx context.Context) (*session, error) {
	var res struct {
		Sessions []session `json:"sessions"`
	}
	if err := r.call(ctx, "session.list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	for i := range res.Sessions {
		if res.Sessions[i].ID == r.sid {
			return &res.Sessions[i], nil
		}
	}
	return nil, nil
}

// workingNow reads the session's mission summary, and folds it into
// workingSeen.
func (r *runner) workingNow(ctx context.Context) (bool, error) {
	s, err := r.session(ctx)
	if err != nil || s == nil {
		return false, err
	}
	w := s.Mission != nil && s.Mission.Working
	r.mu.Lock()
	if w {
		r.workingSeen = true
	}
	r.working = w
	r.mu.Unlock()
	return w, nil
}

func (r *runner) capture(ctx context.Context) string {
	var res struct {
		Text string `json:"text"`
	}
	_ = r.call(ctx, "session.capture", map[string]any{"id": r.sid, "lines": 60, "join": true}, &res)
	return res.Text
}

// screenTail is the last n non-blank lines of the visible pane.
func screenTail(text string, n int) string {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (r *runner) pending(ctx context.Context) []wire.Approval {
	var res struct {
		Approvals []wire.Approval `json:"approvals"`
	}
	_ = r.call(ctx, wire.MethodApprovalList, map[string]any{"sessionId": r.sid}, &res)
	return res.Approvals
}

func (r *runner) screen(ctx context.Context, id string) (harness.Screen, error) {
	var s harness.Screen
	err := r.call(ctx, harness.MethodScreen, map[string]any{"id": r.sid, "screen": id}, &s)
	return s, err
}

func (r *runner) act(ctx context.Context, action, target string, args map[string]any) (harness.ActionResult, error) {
	p := map[string]any{"id": r.sid, "action": action}
	if target != "" {
		p["target"] = target
	}
	if args != nil {
		p["args"] = args
	}
	var res harness.ActionResult
	err := r.call(ctx, harness.MethodAction, p, &res)
	return res, err
}

var (
	// blockRE: text that says the harness cannot answer because of the
	// account or provider, not because of Togo.
	blockRE     = regexp.MustCompile(`(?i)(log ?in\b|login|sign ?in|signed out|authenticat|api key|unauthori[sz]ed|\b401\b|\b403\b|forbidden|access denied|insufficient|suspended|quota|billing|credit|re-login|not logged|subscription|payment required|token (has )?(expired|revoked)|invalid (api )?(key|token)|no (provider|model) (configured|selected)|configure a provider|first-run|welcome to|\b50[234]\b|cannot connect|connection refused|upstream error|econnrefused)`)
	trustRE     = regexp.MustCompile(`(?i)trust`)
	loginPrompt = regexp.MustCompile(`(?i)(log ?in|sign ?in|login|authenticat|api key).{0,100}(\?|\[y|\(y|press|choose|select|continue|:\s*$)|(press|choose|select).{0,40}(log ?in|sign ?in|login)`)
	setupDialog = regexp.MustCompile(`(?i)(welcome to .{0,40}(get you started|setup|set up)|first[- ]run setup|enter continue\s*$|^\W*save\s{2,}cancel\W*$)`)
	trustYes    = regexp.MustCompile(`(?i)^(yes|trust|allow|continue|proceed)\b`)
	declineRE   = regexp.MustCompile(`(?i)^(no|skip|not now|later|cancel|dismiss|don'?t|never)\b`)
	trustDialog = regexp.MustCompile(`(?i)(do you trust|trust (this|the) (folder|files|directory|workspace|project)|i trust this folder)`)
	updateRE    = regexp.MustCompile(`(?i)(update available|new version|upgrade available|update now|updating to)`)
	choiceRE    = regexp.MustCompile(`(?i)(\bskip\b|\bconfirm\b|\[y/n\]|\(y/n\)|\byes\b|\blater\b|\binstall\b|press enter)`)
)

func updatePrompt(pane string) bool {
	t := screenTail(pane, 30)
	return updateRE.MatchString(t) && choiceRE.MatchString(t)
}

// settle waits for the agent to draw its prompt after a start or
// restart. Folder-trust prompts for the run's own scratch folder are
// answered from the "phone"; sign-in and update prompts are never
// answered and leave the harness blocked.
func (r *runner) settle(ctx context.Context, max time.Duration) error {
	deadline := time.Now().Add(max)
	// Some prompts are drawn late (Copilot's folder trust follows a
	// resize): wait at least 10 s before calling the screen ready.
	minReady := time.Now().Add(10 * time.Second)
	var last string
	stable := 0
	trusted := map[string]bool{}
	deskTrust := false
	var trustSince time.Time
	for time.Now().Before(deadline) {
		if err := sleep(ctx, time.Second); err != nil {
			return err
		}
		s, err := r.session(ctx)
		if err != nil {
			return err
		}
		if s == nil {
			return errors.New("session vanished")
		}
		if s.Status == "ended" || s.Status == "failed" {
			return fmt.Errorf("agent exited at launch (%s)", exitText(s.Exit))
		}
		for _, a := range r.pending(ctx) {
			text := a.Title + " " + a.Snippet
			switch {
			case a.SignIn != nil || blockRE.MatchString(text):
				a := a
				r.signin = &a
				r.blocked = "sign-in prompt: " + clip(oneLine(a.Title), 70)
				return nil
			case trustRE.MatchString(text) && !trusted[a.RequestID]:
				trusted[a.RequestID] = true
				// Answer the way the phone does: tap the trusting option.
				decision := "approve"
				for _, o := range a.Options {
					if trustYes.MatchString(strings.TrimSpace(o.Label)) {
						decision = fmt.Sprintf("option:%d", o.N)
						break
					}
				}
				if err := r.call(ctx, wire.MethodApprovalAnswer, map[string]string{"requestId": a.RequestID, "decision": decision}, nil); err != nil {
					r.set("trust", fail, "folder-trust prompt %q reached Needs you but approval.answer failed: %v", clip(oneLine(a.Title), 50), err)
				} else {
					r.set("trust", pass, "folder-trust prompt %q reached Needs you; answered by id", clip(oneLine(a.Title), 50))
				}
				stable, minReady = 0, time.Now().Add(3*time.Second)
			case updateRE.MatchString(text):
				r.blocked = "update prompt at start (not answered): " + clip(oneLine(a.Title), 60)
				return nil
			case !trusted[a.RequestID]:
				// A setup question with a "no" answer (Copilot's terminal
				// key binding) is declined from the phone; anything else
				// stops the run for this harness.
				trusted[a.RequestID] = true
				n := 0
				for _, o := range a.Options {
					if declineRE.MatchString(strings.TrimSpace(o.Label)) {
						n = o.N
						break
					}
				}
				if n == 0 {
					r.blocked = "unexpected start-up prompt: " + clip(oneLine(a.Title), 60)
					return nil
				}
				if err := r.call(ctx, wire.MethodApprovalAnswer, map[string]string{"requestId": a.RequestID, "decision": fmt.Sprintf("option:%d", n)}, nil); err != nil {
					r.blocked = "start-up prompt could not be declined: " + err.Error()
					return nil
				}
				r.note("declined start-up prompt %q from the phone (option %d)", clip(oneLine(a.Title), 50), n)
				stable, minReady = 0, time.Now().Add(3*time.Second)
			}
		}
		pane := r.capture(ctx)
		if updatePrompt(pane) {
			r.blocked = "update dialog on screen (not answered, nothing typed)"
			return nil
		}
		if trustDialog.MatchString(screenTail(pane, 30)) && trustSince.IsZero() {
			trustSince = time.Now()
		}
		// Detection may take a few seconds (a redraw, the idle check): give
		// the prompt 15 s to reach Needs you before calling it a miss.
		if trustDialog.MatchString(screenTail(pane, 30)) && time.Since(trustSince) > 15*time.Second && !deskTrust {
			// On screen for two polls and still not in Needs you: the phone
			// could not get past it. Answer it at the desk to go on.
			deskTrust = true
			r.set("trust", fail, "folder-trust dialog on screen for 15s never reached Needs you (%s)", clip(oneLine(lineMatching(pane, trustDialog)), 70))
			if err := r.deskAnswer(ctx, pane); err != nil {
				r.blocked = "folder-trust dialog could not be answered: " + err.Error()
				return nil
			}
			stable, minReady = 0, time.Now().Add(3*time.Second)
			continue
		}
		if strings.TrimSpace(pane) != "" && pane == last {
			stable++
		} else {
			stable = 0
		}
		last = pane
		onTrust := trustDialog.MatchString(screenTail(pane, 30))
		if stable >= 2 && time.Now().After(minReady) && !onTrust {
			if l := lineMatching(screenTail(pane, 20), loginPrompt); l != "" {
				r.blocked = "sign-in prompt on screen (nothing typed): " + clip(oneLine(l), 90)
			} else if l := lineMatching(screenTail(pane, 20), setupDialog); l != "" {
				r.blocked = "setup dialog on screen (nothing typed): " + clip(oneLine(l), 90)
			}
			return nil
		}
	}
	return nil // a busy screen (spinners) never settles; go on
}

func exitText(e *wire.SessionExit) string {
	if e == nil {
		return "no exit info"
	}
	s := e.Reason
	if e.Code != nil {
		s += fmt.Sprintf(" code %d", *e.Code)
	}
	if e.Detail != nil {
		s += ": " + *e.Detail
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func lineMatching(text string, re *regexp.Regexp) string {
	for _, l := range strings.Split(text, "\n") {
		if re.MatchString(l) {
			return l
		}
	}
	return ""
}

var (
	cursorRE = regexp.MustCompile(`^\s*[│|]?\s*(❯|›|>|●|▶|→)\s`)
	yesRE    = regexp.MustCompile(`(?i)^\s*[│|]?\s*(?:[❯›>●▶→]\s*)?(?:(\d)[.)]\s*)?(yes|trust|allow|proceed)\b`)
)

// deskAnswer picks the "yes" row of an arrow or numbered menu on screen,
// as the owner would at the desk, and checks the dialog went away.
func (r *runner) deskAnswer(ctx context.Context, pane string) error {
	lines := strings.Split(screenTail(pane, 30), "\n")
	cursor, yes, digit := -1, -1, ""
	for i, l := range lines {
		if cursor < 0 && cursorRE.MatchString(l) {
			cursor = i
		}
		if m := yesRE.FindStringSubmatch(l); m != nil && yes < 0 {
			yes, digit = i, m[1]
		}
	}
	keys := []string{}
	switch {
	case digit != "":
		keys = append(keys, digit)
	case yes >= 0 && cursor >= 0:
		for i := cursor; i < yes; i++ {
			keys = append(keys, "Down")
		}
		for i := yes; i < cursor; i++ {
			keys = append(keys, "Up")
		}
		keys = append(keys, "Enter")
	default:
		return fmt.Errorf("no yes option found")
	}
	if _, err := r.sb.tmux(ctx, append([]string{"send-keys", "-t", "=" + r.sid + ":"}, keys...)...); err != nil {
		return err
	}
	_ = sleep(ctx, 1500*time.Millisecond)
	if p := r.capture(ctx); trustDialog.MatchString(screenTail(p, 30)) && digit != "" {
		_, _ = r.sb.tmux(ctx, "send-keys", "-t", "="+r.sid+":", "Enter")
		_ = sleep(ctx, 1500*time.Millisecond)
	}
	if p := r.capture(ctx); trustDialog.MatchString(screenTail(p, 30)) {
		return fmt.Errorf("still on screen after %v", keys)
	}
	r.note("answered the folder-trust dialog at the desk (%s)", strings.Join(keys, " "))
	return nil
}
