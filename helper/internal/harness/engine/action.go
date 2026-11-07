package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/session"
)

// Action limits and waits.
const (
	maxArgs       = 16
	maxArgString  = 4 << 10
	idleWait      = 10 * time.Second
	verifyPoll    = 150 * time.Millisecond
	defaultVerify = 3 * time.Second
)

func invalidArgs(msg string) error { return &session.InvalidParamsError{Msg: msg} }

// checkArgs bounds action args: at most 16 keys, strings of at most 4 KiB
// with no control characters (text may hold newlines and tabs).
func checkArgs(args map[string]json.RawMessage) (map[string]any, error) {
	if len(args) > maxArgs {
		return nil, invalidArgs("too many args")
	}
	out := map[string]any{}
	for k, raw := range args {
		if reservedVars[k] {
			return nil, invalidArgs("an arg names a value only the helper sets")
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, invalidArgs("args must be JSON")
		}
		if err := checkArgValue(k, v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

func checkArgValue(key string, v any) error {
	switch x := v.(type) {
	case string:
		if len(x) > maxArgString || !utf8.ValidString(x) {
			return invalidArgs("an arg is too long")
		}
		if strings.ContainsFunc(x, func(r rune) bool {
			return unicode.IsControl(r) && !(key == "text" && (r == '\n' || r == '\t'))
		}) {
			return invalidArgs("an arg has a control character")
		}
	case []any:
		for _, e := range x {
			if err := checkArgValue(key, e); err != nil {
				return err
			}
		}
	case map[string]any:
		if len(x) > maxArgs {
			return invalidArgs("an arg has too many keys")
		}
		for k, e := range x {
			if err := checkArgValue(k, e); err != nil {
				return err
			}
		}
	}
	return nil
}

// Act runs an adapter action.
func (e *Engine) Act(ctx context.Context, req harness.ActionRequest) (harness.ActionResult, error) {
	s, err := e.served(ctx, req.Session)
	if err != nil {
		return harness.ActionResult{}, err
	}
	if req.Action == harness.ActionStop && req.Target != "" {
		// Stop on a queued prompt cancels it before it is typed.
		if queued, sent := e.dropQueued(s, req.Target); queued {
			return harness.ActionResult{Result: harness.ResultApplied}, nil
		} else if sent {
			return harness.ActionResult{Result: harness.ResultFailed, Message: "The prompt was already sent"}, nil
		}
	}
	act, ok := s.ad.Actions[req.Action]
	if !ok {
		return harness.ActionResult{}, harness.ErrUnknownAction
	}
	args, err := checkArgs(req.Args)
	if err != nil {
		return harness.ActionResult{}, err
	}
	args["target"] = req.Target
	if _, ok := args["id"]; !ok && req.Target != "" {
		// Templates name the chosen item .id; a caller that sends only the
		// target means that item.
		args["id"] = req.Target
	}
	res, err := e.run(ctx, s, req.Action, act, args)
	if err != nil || res.Result != harness.ResultApplied {
		return res, err
	}
	if name := e.screenFor(s, req.Action); name != "" {
		if sc, err := e.buildScreen(ctx, s, name, s.ad.Screens[name], nil); err == nil {
			res.Screen = &sc
		}
	}
	return res, nil
}

// screenFor names the settings or picker screen an action came from, which is read
// again after it applies.
func (e *Engine) screenFor(s *sess, action string) string {
	for _, name := range s.ad.screenOrder {
		sc := s.ad.Screens[name]
		if sc.Primitive == harness.PrimSettings && sc.Source != "" {
			for _, f := range sc.Fields {
				if f.Action == action {
					return name
				}
			}
		}
		// A picker's choice or item action changes what it shows: which
		// model is selected, which model holds a role.
		if sc.Primitive == harness.PrimPicker && (sc.OnSelect == action || slices.Contains(sc.Actions, action)) {
			return name
		}
	}
	return ""
}

// run picks the route (the first branch whose condition holds) and runs
// it, then verifies.
func (e *Engine) run(ctx context.Context, s *sess, name string, act spec.Action, args map[string]any) (harness.ActionResult, error) {
	s.mu.Lock()
	data := s.templateDataLocked(args)
	s.mu.Unlock()
	route := act.Routes
	if len(act.Branch) > 0 {
		found := false
		for _, br := range act.Branch {
			cond, err := Render(br.When, data)
			if br.When == "" || (err == nil && Truthy(cond)) {
				route, found = br.Routes, true
				break
			}
		}
		if !found {
			return harness.ActionResult{Result: harness.ResultFallback}, nil
		}
	}
	switch {
	case route.Type != nil:
		return e.runType(ctx, s, act, route.Type, data)
	case route.Keys != nil:
		return e.runKeys(ctx, s, act, route.Keys, data)
	case route.CLI != nil:
		return e.runCLI(ctx, s, act, route.CLI, data)
	case route.Relaunch != nil:
		return e.runRelaunch(ctx, s, route.Relaunch, data)
	case route.Write != nil:
		return e.runWrite(ctx, s, act, route.Write, data)
	case route.Builtin != "":
		return e.runBuiltin(ctx, s, route.Builtin, args)
	case route.HTTP != nil:
		return e.runCall(ctx, s, act, spec.KindHTTP, route.HTTP, data)
	case route.RPC != nil:
		return e.runCall(ctx, s, act, spec.KindRPC, route.RPC, data)
	}
	return harness.ActionResult{Result: harness.ResultFallback}, nil
}

func (e *Engine) runType(ctx context.Context, s *sess, act spec.Action, t *spec.Type, data map[string]any) (harness.ActionResult, error) {
	text, err := Render(t.Text, data)
	if err != nil {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The command could not be built"}, nil
	}
	text, ok := commandText(text, t.Prompt)
	if !ok {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "Only one line commands can be typed"}, nil
	}
	if err := session.ValidateSendText(text); err != nil {
		return harness.ActionResult{}, err
	}
	if e.cfg.Typist == nil {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	s.mu.Lock()
	s.notePhoneLocked(e.newID(), text)
	s.mu.Unlock()
	if err := e.typeCommand(ctx, s, text, t.Submit, t.WhenIdle); err != nil {
		if errors.Is(err, harness.ErrBusy) || ctx.Err() != nil {
			return harness.ActionResult{}, err
		}
		return harness.ActionResult{Result: harness.ResultFailed, Message: "Typing into the session failed"}, nil
	}
	if e.verify(ctx, s, act.Verify, data) {
		return harness.ActionResult{Result: harness.ResultApplied}, nil
	}
	return harness.ActionResult{Result: harness.ResultTyped, Message: "Sent " + text}, nil
}

func (e *Engine) runKeys(ctx context.Context, s *sess, act spec.Action, k *spec.Keys, data map[string]any) (harness.ActionResult, error) {
	for _, key := range k.Keys {
		if !spec.AllowedKeys[key] || (key == "BTab" && !k.Cycle) {
			return harness.ActionResult{Result: harness.ResultFailed, Message: "That key is not allowed"}, nil
		}
	}
	if e.cfg.Typist == nil {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	if k.Cycle {
		return e.cycle(ctx, s, act, k, data)
	}
	if err := e.sendKeys(ctx, s, false, k.Keys...); err != nil {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "Sending keys failed"}, nil
	}
	if e.verify(ctx, s, act.Verify, data) {
		return harness.ActionResult{Result: harness.ResultApplied}, nil
	}
	return harness.ActionResult{Result: harness.ResultTyped}, nil
}

// maxCycle bounds the presses of one cycle whatever the adapter says.
const maxCycle = 8

// cycle presses the keys one press at a time until the verify value
// equals the target, reading the value from the screen after each press.
// It never presses blind: with no readable value it falls back, and it
// stops when a press changes nothing, when it reads a value the adapter
// does not list as a mode, when the values come round again, or after max
// presses.
func (e *Engine) cycle(ctx context.Context, s *sess, act spec.Action, k *spec.Keys, data map[string]any) (harness.ActionResult, error) {
	v := act.Verify
	if v == nil || v.Select == "" || e.cfg.Screen == nil {
		return harness.ActionResult{Result: harness.ResultFallback, Message: "This harness cannot confirm the change"}, nil
	}
	want, err := Render(v.Equals, data)
	if err != nil || want == "" {
		return harness.ActionResult{}, invalidArgs("missing target")
	}
	known := s.ad.modeIDs()
	if len(known) > 0 && !known[want] {
		return harness.ActionResult{}, invalidArgs("unknown mode")
	}
	within := v.Within
	if within <= 0 {
		within = defaultVerify
	}
	l := typingLock(s.id)
	l.Lock()
	defer l.Unlock()
	cur, ok := e.readValue(ctx, s, v, true)
	if !ok {
		return harness.ActionResult{Result: harness.ResultFallback, Message: "The current mode is not on screen"}, nil
	}
	if cur == want {
		return harness.ActionResult{Result: harness.ResultApplied}, nil
	}
	seen := map[string]bool{cur: true}
	max := min(k.Max, maxCycle)
	for press := 1; press <= max; press++ {
		if err := e.cfg.Typist.Keys(ctx, s.id, k.Keys...); err != nil {
			return harness.ActionResult{Result: harness.ResultFailed, Message: "Sending keys failed"}, nil
		}
		next, res, done := e.awaitChange(ctx, s, v, cur, within, press)
		if done {
			return res, ctx.Err()
		}
		cur = next
		switch {
		case cur == want:
			return harness.ActionResult{Result: harness.ResultApplied}, nil
		case len(known) > 0 && !known[cur]:
			return harness.ActionResult{Result: harness.ResultFailed, Message: "The mode changed to one Togo does not know; stopped"}, nil
		case seen[cur]:
			return harness.ActionResult{Result: harness.ResultFailed, Message: "The modes came round without reaching it; stopped"}, nil
		}
		seen[cur] = true
	}
	return harness.ActionResult{Result: harness.ResultFailed,
		Message: fmt.Sprintf("The mode was not reached in %d presses; stopped", max)}, nil
}

// awaitChange waits for the verify value to move off prev after a press.
func (e *Engine) awaitChange(ctx context.Context, s *sess, v *spec.Verify, prev string, within time.Duration, press int) (string, harness.ActionResult, bool) {
	deadline := time.Now().Add(e.scaled(within))
	for {
		select {
		case <-ctx.Done():
			return "", harness.ActionResult{}, true
		case <-time.After(e.scaled(verifyPoll)):
		}
		if c, ok := e.readValue(ctx, s, v, true); ok && c != prev {
			return c, harness.ActionResult{}, false
		}
		if time.Now().After(deadline) {
			return "", harness.ActionResult{Result: harness.ResultFailed,
				Message: fmt.Sprintf("The mode did not change after press %d; stopped", press)}, true
		}
	}
}

// readValue evaluates a verify select now. With screen set, state read
// from the pane's visible text overrides the mappers'.
//
// A feed's value is read from the state its mapper keeps (".model" is
// the model the feed last showed), not from its last raw record.
func (e *Engine) readValue(ctx context.Context, s *sess, v *spec.Verify, screen bool) (string, bool) {
	if v == nil || v.Select == "" {
		return "", false
	}
	s.mu.Lock()
	state := s.stateLocked()
	var input any = stateAny(state)
	readers := []ScreenReader{}
	for _, m := range s.mappers {
		if r, ok := m.(ScreenReader); ok {
			readers = append(readers, r)
		}
	}
	_, isFeed := s.ad.Feeds[v.Source]
	s.mu.Unlock()
	if screen {
		if len(readers) == 0 || e.cfg.Screen == nil {
			return "", false
		}
		text, err := e.cfg.Screen(ctx, s.id)
		if err != nil {
			return "", false
		}
		found := false
		for _, r := range readers {
			for k, val := range r.ReadScreen(text) {
				state[k] = val
				found = true
			}
		}
		if !found {
			return "", false
		}
	} else if !isFeed && v.Source != "" {
		data, _, err := e.fetch(ctx, s, v.Source, nil, true)
		if err != nil {
			return "", false
		}
		input = data
	}
	out, err := JQFirst(v.Select, input, state)
	if err != nil || out == nil {
		return "", false
	}
	return toString(out), true
}

// verify polls the verify select until it equals the rendered value or
// the window passes.
func (e *Engine) verify(ctx context.Context, s *sess, v *spec.Verify, data map[string]any) bool {
	if v == nil || v.Select == "" {
		return false
	}
	want, err := Render(v.Equals, data)
	if err != nil {
		return false
	}
	within := v.Within
	if within <= 0 {
		within = defaultVerify
	}
	deadline := time.Now().Add(e.scaled(within))
	for {
		if got, ok := e.readValue(ctx, s, v, false); ok && got == want {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(e.scaled(verifyPoll)):
		}
	}
}

func (e *Engine) runCLI(ctx context.Context, s *sess, act spec.Action, c *spec.CLI, data map[string]any) (harness.ActionResult, error) {
	argv := make([]string, 0, len(c.Argv))
	for _, a := range c.Argv {
		r, err := Render(a, data)
		if err != nil || flagFromInput(a, r) {
			return harness.ActionResult{Result: harness.ResultFailed, Message: "The command could not be built"}, nil
		}
		argv = append(argv, r)
	}
	ctx, cancel := context.WithTimeout(ctx, maxTimeout)
	defer cancel()
	if _, err := source.Run(ctx, argv, s.env); err != nil {
		e.cfg.Logf("harness: %s action %s (%s): %v", s.ad.Adapter.Adapter, argv[0], act.Stability, err)
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The command failed"}, nil
	}
	e.verify(ctx, s, act.Verify, data)
	return harness.ActionResult{Result: harness.ResultApplied}, nil
}

func (e *Engine) runBuiltin(ctx context.Context, s *sess, name string, args map[string]any) (harness.ActionResult, error) {
	switch name {
	case "approval_answer":
		answers := map[string][]string{}
		if m, ok := args["answers"].(map[string]any); ok {
			for k, v := range m {
				switch x := v.(type) {
				case []any:
					for _, e := range x {
						answers[k] = append(answers[k], toString(e))
					}
				case string:
					answers[k] = []string{x}
				}
			}
		}
		target, _ := args["target"].(string)
		return e.answerQuestion(ctx, s, target, answers)
	case "rpc_reply":
		return e.rpcReply(ctx, s, args)
	}
	return harness.ActionResult{Result: harness.ResultFallback}, nil
}
