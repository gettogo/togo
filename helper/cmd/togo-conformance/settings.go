package main

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

var loginWords = regexp.MustCompile(`(?i)(log ?in|sign ?in|login|authenticat|oauth)`)

// modelPlan carries the model scenario across turns.
type modelPlan struct {
	order   []pickerItem
	onSel   string
	cheap   pickerItem
	setErr  string
	preTurn bool // the pre-turn selection failed (not the screen)
	turned  bool // the switch ran a turn
	turn1   map[string]int64
	model1  string
	strong1 bool
}

// actOK is whether an action result means the change was made or sent.
func actOK(res harness.ActionResult) bool {
	switch res.Result {
	case harness.ResultApplied, harness.ResultTyped, harness.ResultRelaunched, harness.ResultQueued:
		return true
	}
	return false
}

// afterAct waits for a relaunched agent to come back.
// A typed action must leave the session idle: a "working" that never
// clears holds every later phone prompt in the queue.
func (r *runner) afterAct(ctx context.Context, res harness.ActionResult, scenario, what string) {
	if res.Result == harness.ResultRelaunched {
		_ = r.settle(ctx, 45*time.Second)
		_ = r.open(ctx)
		_ = r.describe(ctx)
	} else {
		_ = sleep(ctx, 1500*time.Millisecond)
	}
	if r.stuckWorking(ctx, what) {
		r.set(scenario, fail, "after %s (%s) mission.working stayed true at an idle prompt; phone prompts queue behind it", what, res.Result)
	}
}

// stuckWorking reports whether the session says working while its input
// line sits empty and idle, and then gets it unstuck: an interrupt, else
// one prompt typed at the desk.
func (r *runner) stuckWorking(ctx context.Context, what string) bool {
	for i := 0; i < 10; i++ {
		if w, err := r.workingNow(ctx); err != nil || !w {
			return false
		}
		if sleep(ctx, time.Second) != nil {
			return false
		}
	}
	a := r.capture(ctx)
	_ = sleep(ctx, 2*time.Second)
	b := r.capture(ctx)
	if line, found := r.inputLine(b); found && line != "" {
		return false
	} else if !found && a != b {
		return false
	}
	// An interrupt is no cure: with "working" stuck it presses Escape
	// until idle, which in Claude opens the rewind menu. A prompt typed at
	// the desk runs a real turn, whose end clears the state.
	r.note("working stuck after %s; typing one prompt at the desk to recover", what)
	_, _ = r.sb.tmux(ctx, "send-keys", "-t", "="+r.sid+":", "-l", okPrompt)
	_, _ = r.sb.tmux(ctx, "send-keys", "-t", "="+r.sid+":", "Enter")
	if r.waitIdle(ctx, 60*time.Second) {
		r.note("a prompt typed at the desk cleared it")
	} else {
		r.note("still working after a desk prompt")
	}
	return true
}

// modelSetup picks the cheapest model before the first turn, so every
// turn of the run is cheap.
func (r *runner) modelSetup(ctx context.Context) modelPlan {
	var m modelPlan
	if !r.has(harness.CapModel) {
		return m
	}
	if _, ok := r.hasScreen("model"); !ok {
		return m
	}
	s, err := r.screen(ctx, "model")
	if err != nil || s.Picker == nil {
		m.setErr = "model screen: " + errText(err)
		return m
	}
	var items []pickerItem
	for _, it := range s.Picker.Items {
		items = append(items, pickerItem{ID: it.ID, Title: it.Title, Subtitle: it.Subtitle, Group: it.Group, Disabled: it.Disabled})
	}
	m.order, m.onSel = cheapest(items), s.Picker.OnSelect
	if len(m.order) == 0 {
		m.setErr = "model picker lists no usable model"
		return m
	}
	m.cheap = m.order[0]
	if want := r.opts.models[r.id]; want != "" {
		// -model harness=id: this model first, listed by the picker or not
		// (a static picker types what it is given).
		it := pickerItem{ID: want, Title: want}
		for _, x := range items {
			if x.ID == want || strings.HasSuffix(x.ID, "/"+want) {
				it = x
				break
			}
		}
		m.cheap = it
		rest := make([]pickerItem, 0, len(m.order))
		for _, o := range m.order {
			if o.ID != it.ID {
				rest = append(rest, o)
			}
		}
		m.order = append([]pickerItem{it}, rest...)
	} else if r.opts.router != nil {
		// -openrouter: the OpenRouter model first, so every turn runs on
		// the test key; the switch scenario still picks a second model.
		if it, ok := r.opts.router.pick(items); ok {
			m.cheap = it
			rest := make([]pickerItem, 0, len(m.order))
			for _, o := range m.order {
				if o.ID != it.ID {
					rest = append(rest, o)
				}
			}
			m.order = append([]pickerItem{it}, rest...)
		} else {
			r.note("openrouter: the model picker lists no %s; the agent runs on its own account", r.opts.router.model)
		}
	}
	if sameModel(m.cheap.ID, s.Picker.Selected) && s.Picker.Selected != "" {
		return m
	}
	res, err := r.act(ctx, m.onSel, m.cheap.ID, map[string]any{"id": m.cheap.ID})
	if err != nil || !actOK(res) {
		m.setErr = "selecting " + m.cheap.ID + ": " + firstNonEmpty(errText(err), res.Result+" "+res.Message)
		m.preTurn = true
		return m
	}
	r.note("model set to %s before the first turn (%s)", m.cheap.ID, res.Result)
	r.afterAct(ctx, res, "model_switch", "set model "+m.cheap.ID)
	return m
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// modelSwitch picks a second model, runs a turn, and checks the turn ran
// on it according to the harness's own session records.
func (r *runner) modelSwitch(ctx context.Context, m *modelPlan) {
	if !r.has(harness.CapModel) {
		r.set("model_switch", skip, "adapter declares no model capability")
		return
	}
	if m.setErr != "" && !m.preTurn {
		r.set("model_switch", fail, "%s", m.setErr)
		return
	}
	if m.preTurn {
		// Picking before the first prompt failed; the switch after it
		// is still the check.
		r.note("before the first prompt: %s", m.setErr)
		m.strong1 = false
	}
	if len(m.order) < 2 {
		r.set("model_switch", skip, "picker offers %d usable model(s); nothing to switch to", len(m.order))
		return
	}
	if m.strong1 && !sameModel(m.cheap.ID, m.model1) {
		r.set("model_switch", fail, "picked %s before turn 1, but turn 1 ran on %s (harness records)", m.cheap.ID, m.model1)
	}
	// The next turn must run on a model other than the one turn 1 used.
	// A model whose provider refuses the account (out of credits, no
	// key) says nothing about Togo: the next one is tried instead.
	var tries []pickerItem
	for _, it := range append(append([]pickerItem{}, m.order[1:]...), m.order[0]) {
		if m.model1 == "" || !sameModel(it.ID, m.model1) {
			tries = append(tries, it)
		}
	}
	if len(tries) == 0 {
		tries = []pickerItem{m.order[1]}
	}
	// Back to the cheap one for the remaining turns, whatever happened:
	// a later scenario on a model that fails would fail for that alone.
	switched := false
	defer func() {
		if !switched {
			return
		}
		if res, err := r.act(ctx, m.onSel, m.cheap.ID, map[string]any{"id": m.cheap.ID}); err == nil && actOK(res) {
			r.afterAct(ctx, res, "model_switch", "set model back")
		}
	}()
	var blocked []string
	refused := map[string]bool{} // provider groups that refused
	n := 0
	for _, second := range tries {
		if n == 3 {
			break
		}
		if second.Group != "" && refused[second.Group] {
			continue
		}
		n++
		res, err := r.act(ctx, m.onSel, second.ID, map[string]any{"id": second.ID})
		if err != nil || !actOK(res) {
			r.set("model_switch", fail, "selecting %s: %s", second.ID, firstNonEmpty(errText(err), res.Result+" "+res.Message))
			return
		}
		switched = true
		r.afterAct(ctx, res, "model_switch", "set model "+second.ID)
		before := truthFiles(r.id, r.marker, r.start)
		t := r.turn(ctx, okPrompt, false, nil)
		m.turned = true
		if !saysOK(t.reply) && blockRE.MatchString(t.errText) {
			blocked = append(blocked, second.ID+": "+clip(oneLine(t.errText), 80))
			refused[second.Group] = second.Group != ""
			r.note("model %s is blocked by its provider (%s); trying another", second.ID, clip(oneLine(t.errText), 80))
			continue
		}
		if !saysOK(t.reply) {
			r.set("model_switch", fail, "switched to %s (%s) but the next turn failed: %s", second.ID, res.Result, firstNonEmpty(t.errText, "no reply"))
			return
		}
		r.judgeSwitch(ctx, m, second, res, before)
		return
	}
	r.set("model_switch", skip, "blocked: every model tried is refused by its provider (%s)", strings.Join(blocked, "; "))
}

// judgeSwitch checks the turn after selecting second ran on it.
func (r *runner) judgeSwitch(ctx context.Context, m *modelPlan, second pickerItem, res harness.ActionResult, before map[string]int64) {
	files := truthFiles(r.id, r.marker, r.start)
	got, strong := modelSince(files, before)
	switch {
	case strong && sameModel(second.ID, got):
		r.set("model_switch", pass, "turn 1 on %s; after selecting %s (%s) the next turn ran on %s (harness records)", firstNonEmpty(m.model1, m.cheap.ID), second.ID, res.Result, got)
	case strong:
		r.set("model_switch", fail, "selected %s (action %s) but the next turn ran on %s (harness records)", second.ID, res.Result, got)
	default:
		// No reply record names a model: fall back to what the harness
		// reports in the status block, then the picker's selection.
		st := ""
		r.mu.Lock()
		if r.status != nil {
			st = r.status.Model
		}
		r.mu.Unlock()
		sel := ""
		if s, err := r.screen(ctx, "model"); err == nil && s.Picker != nil {
			sel = s.Picker.Selected
		}
		switch {
		case st != "" && sameModel(second.ID, st):
			r.set("model_switch", pass, "status-only: harness status reports %s after selecting %s (no reply record readable)", st, second.ID)
		case st != "":
			r.set("model_switch", fail, "selected %s (action %s) but the harness status still reports %s", second.ID, res.Result, st)
		case sel != "" && sameModel(second.ID, sel):
			r.set("model_switch", pass, "state-only: picker shows %s selected; no harness record names the model", sel)
		default:
			r.set("model_switch", fail, "selected %s (action %s); nothing confirms it (record %q, status %q, picker %q)", second.ID, res.Result, got, st, sel)
		}
	}
}

type modePlan struct{ orig, target string }

var safeModes = []string{"plan", "acceptEdits", "accept-edits", "auto_edit", "auto-edit", "ask", "architect", "default", "build", "code", "chat", "approve", "smart_approve"}
var riskyModes = map[string]bool{"bypassPermissions": true, "yolo": true, "dontAsk": true}

func (r *runner) modeChange(ctx context.Context) modePlan {
	var p modePlan
	if !r.has(harness.CapMode) || len(r.desc.Modes) < 2 {
		r.set("mode", skip, "adapter declares no mode switch")
		return p
	}
	_ = r.describe(ctx)
	p.orig = r.desc.Mode
	ids := map[string]bool{}
	for _, c := range r.desc.Modes {
		ids[c.ID] = true
	}
	for _, s := range safeModes {
		if ids[s] && s != p.orig {
			p.target = s
			break
		}
	}
	if p.target == "" {
		for _, c := range r.desc.Modes {
			if c.ID != p.orig && !riskyModes[c.ID] {
				p.target = c.ID
				break
			}
		}
	}
	if p.target == "" {
		r.set("mode", skip, "no safe mode to switch to")
		return p
	}
	res, err := r.act(ctx, harness.ActionSetMode, "", map[string]any{"id": p.target})
	if err != nil || !actOK(res) {
		r.set("mode", fail, "set_mode %s: %s", p.target, firstNonEmpty(errText(err), res.Result+" "+res.Message))
		p.target = ""
		return p
	}
	r.afterAct(ctx, res, "mode", "set_mode "+p.target)
	if !r.waitMode(ctx, p.target, 15*time.Second) {
		r.set("mode", fail, "set_mode %s answered %s but describe.mode is %q", p.target, res.Result, r.desc.Mode)
		p.target = ""
		return p
	}
	return p
}

func (r *runner) waitMode(ctx context.Context, want string, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if err := r.describe(ctx); err == nil && r.desc.Mode == want {
			return true
		}
		if sleep(ctx, time.Second) != nil {
			return false
		}
	}
	return false
}

// modePersist runs after the model-switch turn: the mode must have held
// across it, and switching back must work.
func (r *runner) modePersist(ctx context.Context, p modePlan) {
	_ = r.describe(ctx)
	held := r.desc.Mode
	if held != p.target {
		r.set("mode", fail, "switched %q → %s, but after the next turn describe.mode is %q", p.orig, p.target, held)
	}
	if p.orig == "" {
		r.set("mode", fail, "switch to %s worked, but the current mode was unknown before it (chip would say \"Mode\")", p.target)
		return
	}
	res, err := r.act(ctx, harness.ActionSetMode, "", map[string]any{"id": p.orig})
	if err != nil || !actOK(res) {
		r.set("mode", fail, "switching back to %s: %s", p.orig, firstNonEmpty(errText(err), res.Result+" "+res.Message))
		return
	}
	r.afterAct(ctx, res, "mode", "set_mode "+p.orig)
	if !r.waitMode(ctx, p.orig, 15*time.Second) {
		r.set("mode", fail, "switching back to %s answered %s but describe.mode is %q", p.orig, res.Result, r.desc.Mode)
		return
	}
	r.set("mode", pass, "%s → %s held across a turn → back to %s", p.orig, p.target, p.orig)
}

type effortPlan struct {
	screen, action, field, orig, target string
	picker                              bool
}

var effortOrder = []string{"low", "minimal", "medium", "off", "none", "high"}

func (r *runner) effortChange(ctx context.Context) effortPlan {
	var p effortPlan
	if !r.has(harness.CapEffort) {
		r.set("effort", skip, "adapter declares no effort setting")
		return p
	}
	if _, ok := r.hasScreen("effort"); !ok {
		r.set("effort", skip, "effort capability without an effort screen")
		return p
	}
	p.screen = "effort"
	s, err := r.screen(ctx, p.screen)
	if err != nil {
		r.set("effort", fail, "effort screen: %v", err)
		return p
	}
	var options []string
	switch {
	case s.Picker != nil:
		p.picker, p.action, p.orig = true, s.Picker.OnSelect, s.Picker.Selected
		for _, it := range s.Picker.Items {
			if !it.Disabled {
				options = append(options, it.ID)
			}
		}
	case s.Settings != nil:
		for _, sec := range s.Settings.Sections {
			for _, f := range sec.Fields {
				if f.Action != "" && !f.ReadOnly && p.action == "" {
					p.action, p.field, p.orig = f.Action, f.ID, rawString(f.Value)
					for _, o := range f.Options {
						options = append(options, o.ID)
					}
				}
			}
		}
	}
	if p.action == "" || len(options) == 0 {
		r.set("effort", fail, "effort screen offers no settable value")
		return p
	}
	for _, want := range append(effortOrder, options...) {
		for _, o := range options {
			if strings.EqualFold(o, want) && o != p.orig && p.target == "" {
				p.target = o
			}
		}
	}
	args := map[string]any{"value": p.target}
	target := p.field
	if p.picker {
		args, target = map[string]any{"id": p.target}, p.target
	}
	res, err := r.act(ctx, p.action, target, args)
	if err != nil || !actOK(res) {
		r.set("effort", fail, "%s %s: %s", p.action, p.target, firstNonEmpty(errText(err), res.Result+" "+res.Message))
		p.target = ""
		return p
	}
	r.afterAct(ctx, res, "effort", p.action+" "+p.target)
	if got := r.effortValue(ctx, p); got == "" {
		r.note("effort screen empty right after %s %s (%s); judged after the next turn", p.action, p.target, res.Result)
	} else if got != p.target {
		r.set("effort", fail, "%s %s answered %s but the effort screen reads %q", p.action, p.target, res.Result, got)
		p.target = ""
	}
	return p
}

func (r *runner) effortValue(ctx context.Context, p effortPlan) string {
	got := ""
	for i := 0; i < 10; i++ {
		s, err := r.screen(ctx, p.screen)
		if err == nil {
			if p.picker && s.Picker != nil {
				got = s.Picker.Selected
			}
			if !p.picker && s.Settings != nil {
				for _, sec := range s.Settings.Sections {
					for _, f := range sec.Fields {
						if f.ID == p.field {
							got = rawString(f.Value)
						}
					}
				}
			}
		}
		if got == p.target || sleep(ctx, 1500*time.Millisecond) != nil {
			break
		}
	}
	return got
}

func (r *runner) effortPersist(ctx context.Context, p effortPlan) {
	got := r.effortValue(ctx, p)
	if got != p.target {
		r.set("effort", fail, "set %s → %s, but after the next turn the screen reads %q", p.orig, p.target, got)
		return
	}
	extra := ""
	r.mu.Lock()
	st := ""
	if r.status != nil {
		st = r.status.Effort
	}
	r.mu.Unlock()
	if st != "" {
		extra = "; harness status effort " + st
		if !strings.EqualFold(st, p.target) {
			r.set("effort", fail, "screen reads %s after the turn but the harness status reports effort %s", p.target, st)
			return
		}
	}
	r.set("effort", pass, "%q → %s held across a turn%s", p.orig, p.target, extra)
}

func rawString(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return strings.Trim(string(v), `"`)
}
