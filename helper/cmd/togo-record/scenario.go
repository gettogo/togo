package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type recording struct {
	agent, scenario, prompt, model, cwd string
	// then is a second prompt, sent once the first turn settles; cwdAs is how the folder reads.
	then, cwdAs string
	args        []string
	deadline    time.Time
	c           *client
	tape        *tape
	session     string
	answered    int
	// changed names the agent config files the scenario changed (since restored).
	changed []string
}

const rpcWait = 30 * time.Second

func (r *recording) run() error {
	hello, err := r.c.call("hello", map[string]any{"protocol": 1,
		"client": map[string]any{"name": "togo-record", "version": "0.0.0"}, "capabilities": []string{}}, rpcWait)
	if err != nil {
		return err
	}
	if !strings.Contains(string(hello.Result), "harness.v1") {
		return errors.New("the helper does not serve harness.v1")
	}
	if _, err := r.c.call("events.subscribe", map[string]any{}, rpcWait); err != nil {
		return err
	}
	go r.keepalive()
	params := map[string]any{"agent": r.agent, "cwd": r.cwd, "muted": true}
	if len(r.args) > 0 {
		params["args"] = r.args
	}
	created, err := r.c.call("session.create", params, rpcWait)
	if err != nil {
		return err
	}
	var s struct {
		ID    string `json:"id"`
		Muted bool   `json:"muted"`
	}
	if err := json.Unmarshal(created.Result, &s); err != nil || s.ID == "" {
		return fmt.Errorf("session.create: no id")
	}
	r.session = s.ID
	r.tape.adopt(s.ID)
	if !s.Muted {
		return errors.New("the session did not start muted")
	}
	if err := r.described(); err != nil {
		return err
	}
	if _, err := r.c.call("harness.open", map[string]any{"id": r.session, "after": 0}, rpcWait); err != nil {
		return err
	}
	r.startup()
	switch r.scenario {
	case "model":
		if r.model == "" {
			return errors.New("-scenario model needs -model")
		}
		if _, err := r.c.call("harness.action", map[string]any{"id": r.session, "action": "set_model",
			"target": r.model, "args": map[string]any{"id": r.model}}, 60*time.Second); err != nil {
			return err
		}
		if err := r.settle(); err != nil {
			return err
		}
	}
	if err := r.send(r.prompt); err != nil {
		return err
	}
	if r.then != "" {
		// Two turns: the first plays out, then the scenario runs on the second.
		if err := r.settle(); err != nil {
			return err
		}
		r.tape.forgetWork()
		if err := r.send(r.then); err != nil {
			return err
		}
	}
	switch r.scenario {
	case "approve", "deny":
		v, err := r.wait("an approval", func(v view) bool { return len(v.approvals) > 0 })
		if err != nil {
			return err
		}
		for _, a := range v.approvals {
			if signIn(a) {
				return errors.New("the agent asks to sign in; left unanswered")
			}
		}
	case "question":
		if _, err := r.wait("a question", func(v view) bool { return len(v.questions) > 0 }); err != nil {
			return err
		}
	case "interrupt":
		if _, err := r.wait("work", func(v view) bool { return v.busy }); err != nil {
			return err
		}
		time.Sleep(3 * time.Second)
		if _, err := r.c.call("harness.interrupt", map[string]any{"id": r.session}, 60*time.Second); err != nil {
			return err
		}
	}
	return r.settle()
}

// described waits for the adapter to know the session.
func (r *recording) described() error {
	for time.Now().Before(r.deadline) {
		f, err := r.c.call("harness.describe", map[string]any{"id": r.session}, rpcWait)
		if err == nil && !strings.Contains(string(f.Result), `"adapter":null`) {
			return nil
		}
		time.Sleep(time.Second)
	}
	return errors.New("no adapter described the session")
}

// startup gives the agent time to draw its prompt, answering a folder-trust
// dialog if it shows one, and declining an offer to initialise the project
// (Crush's AGENTS.md turn), to trust new hooks (Codex) or to index the
// folder (Auggie), the way a user
// who came to send a prompt would. Nothing else is answered before the
// prompt goes.
func (r *recording) startup() {
	end := time.Now().Add(6 * time.Second)
	for time.Now().Before(end) {
		if r.answerStartup() {
			end = time.Now().Add(6 * time.Second)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// answerStartup answers the start-up dialogs startup names, and reports
// whether it answered one. Slow agents draw them late, so send asks again.
func (r *recording) answerStartup() bool {
	answered := false
	for _, a := range r.tape.view().approvals {
		title := strings.ToLower(str(a["title"]))
		text := strings.ToLower(str(a["title"]) + " " + str(a["detail"]) + " " + str(a["snippet"]))
		switch {
		case strings.Contains(title, "initialize this project"), strings.Contains(title, "hooks need review"),
			strings.Contains(text, "index this workspace"):
			// Codex 0.167 asks to trust new hooks at every start; going on
			// without them changes no setting. Auggie offers to index a new
			// folder; "Skip indexing for this session" changes none either.
			r.answer(str(a["requestId"]), "deny")
			answered = true
		case strings.Contains(text, "trust"):
			r.answer(str(a["requestId"]), "approve")
			answered = true
		}
	}
	return answered
}

// send types the prompt, retrying while the agent is still starting.
func (r *recording) send(text string) error {
	var last error
	var blocked time.Time
	for time.Now().Before(r.deadline) {
		if r.answerStartup() {
			time.Sleep(3 * time.Second)
		}
		_, err := r.c.call("harness.send", map[string]any{"id": r.session, "text": text, "mode": "now"}, rpcWait)
		if err == nil {
			return nil
		}
		last = err
		// A dialog in the terminal (an update offer, a sign-in) refuses every
		// send; nothing here answers it, so give up after a while.
		if strings.Contains(err.Error(), "fallback") {
			if blocked.IsZero() {
				blocked = time.Now()
			} else if time.Since(blocked) > 20*time.Second {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("send: %w", last)
}

// wait polls the session's state until ok, answering what the scenario
// answers on the way.
func (r *recording) wait(what string, ok func(view) bool) (view, error) {
	for time.Now().Before(r.deadline) {
		v := r.tape.view()
		if ok(v) {
			return v, nil
		}
		if v.gone {
			return v, fmt.Errorf("the session ended before %s", what)
		}
		time.Sleep(300 * time.Millisecond)
	}
	return view{}, fmt.Errorf("timed out waiting for %s", what)
}

// settle answers requests per the scenario until the session is idle and
// quiet: no turn running, nothing waiting, no frame for four seconds.
func (r *recording) settle() error {
	began := time.Now()
	for time.Now().Before(r.deadline) {
		v := r.tape.view()
		if len(v.approvals) > 0 {
			if !r.answerApprovals(v) {
				return errors.New("the agent asks to sign in; left unanswered")
			}
			continue
		}
		if len(v.questions) > 0 {
			r.answerQuestions(v)
			continue
		}
		if v.gone {
			return nil
		}
		if !v.busy && v.quiet > 4*time.Second && (v.sawWork || time.Since(began) > 15*time.Second) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return errors.New("timed out waiting for the session to settle")
}

func (r *recording) answerApprovals(v view) bool {
	if r.answerStartup() {
		// A start-up dialog drawn after the prompt went (Codex's hooks
		// review comes late) is answered as at start.
		time.Sleep(time.Second)
		return true
	}
	decision := "deny"
	if r.scenario == "approve" && r.answered < 3 || r.scenario == "task" && r.answered < 12 {
		decision = "approve"
	}
	for _, a := range v.approvals {
		if signInIn(a, r.scenario == "task") {
			// Never sign in, and never let a default answer open a login.
			return false
		}
		r.answer(str(a["requestId"]), decision)
	}
	time.Sleep(time.Second)
	return true
}

// signIn reports whether a request is a login, sign-in or credential prompt.
func signIn(a map[string]any) bool {
	return signInIn(a, false)
}

// signInIn is signIn with the snippet read loosely for a task: a task's
// snippet is the screen around the request, so it shows the folder's own
// code, and a repo with a /login route says "login" and "password" there.
// A command's title can be the folder's code too ("test/login.test.ts"),
// so a task reads the title and detail loosely as well; the helper's own
// sign-in flag always counts.
func signInIn(a map[string]any, task bool) bool {
	strict := []string{"login", "log in", "sign in", "sign-in", "authenticat", "api key", "credential", "password", "browser"}
	loose := []string{"log in", "sign in", "sign-in", "authenticat", "api key", "browser"}
	head := strings.ToLower(str(a["title"]) + " " + str(a["detail"]))
	snippet := strings.ToLower(str(a["snippet"]))
	words := strict
	if task {
		words = loose
	}
	for _, w := range words {
		if strings.Contains(head, w) {
			return true
		}
	}
	for _, w := range words {
		if strings.Contains(snippet, w) {
			return true
		}
	}
	return a["signIn"] != nil
}

func (r *recording) answer(rid, decision string) {
	r.answered++
	_, err := r.c.call("approval.answer", map[string]any{"requestId": rid, "decision": decision}, rpcWait)
	if err != nil {
		fmt.Fprintln(os.Stderr, "togo-record: answer:", err)
	}
}

// answerQuestions picks each question's first option (the first two for a
// multi-select) and sends them all at once, the way the question card does.
func (r *recording) answerQuestions(v view) {
	for _, it := range v.questions {
		q, _ := it["question"].(map[string]any)
		answers := map[string]any{}
		qs, _ := q["questions"].([]any)
		for i, raw := range qs {
			one, _ := raw.(map[string]any)
			opts, _ := one["options"].([]any)
			var pick []string
			for j, o := range opts {
				om, _ := o.(map[string]any)
				if j == i%len(opts) || (one["multi"] == true && j < 2) {
					pick = append(pick, str(om["id"]))
				}
			}
			answers[str(one["id"])] = pick
		}
		_, err := r.c.call("harness.action", map[string]any{"id": r.session, "action": "answer",
			"target": str(it["id"]), "args": map[string]any{"answers": answers}}, 60*time.Second)
		if err != nil {
			fmt.Fprintln(os.Stderr, "togo-record: question:", err)
		}
		// One try per question: a failed answer is part of what the fixture records.
		r.markAnswered(str(it["id"]))
	}
	time.Sleep(time.Second)
}

func (r *recording) markAnswered(id string) {
	r.tape.mu.Lock()
	defer r.tape.mu.Unlock()
	if it := r.tape.items[id]; it != nil {
		if q, ok := it["question"].(map[string]any); ok {
			q["answered"] = map[string]any{"_recorder": []any{"tried"}}
		}
	}
}

func (r *recording) keepalive() {
	for {
		time.Sleep(10 * time.Second)
		if _, err := r.c.call("events.keepalive", map[string]any{}, rpcWait); err != nil {
			return
		}
	}
}

// cleanup kills the session through the helper and, if it lingers, in tmux.
func (r *recording) cleanup() {
	if r.session == "" {
		return
	}
	_, _ = r.c.call("harness.close", map[string]any{"id": r.session}, 5*time.Second)
	_, _ = r.c.call("session.kill", map[string]any{"id": r.session}, rpcWait)
	time.Sleep(time.Second)
	_ = exec.Command("tmux", "-L", "togo", "kill-session", "-t", r.session).Run()
}
