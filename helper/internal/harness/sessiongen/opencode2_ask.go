package sessiongen

import (
	"encoding/json"
	"errors"
	"strconv"
)

// What an OpenCode 2.x turn can stop on (Ask), as the 2.2.5 recordings
// have it.
const (
	// AskPermission: the shell tool asks to run a command
	// (events.sse permission.asked).
	AskPermission = "permission"
	// AskQuestion: the question tool's form, one field with two options
	// and free text allowed (events.sse form.created, kind question).
	AskQuestion = "question"
	// AskRelease: a form with a required boolean, a required field shown
	// only when the boolean is true, a multi-select with a preset, a
	// field shown only for one of its values, a number with a preset and a
	// field shown only when the boolean is false (forms.sse "Release
	// settings").
	AskRelease = "release"
)

// releaseFields are the fields of forms.sse's "Release settings" form,
// as recorded.
const releaseFields = `[{"key":"notify","title":"Send a notice","required":true,"type":"boolean","default":false},` +
	`{"key":"channel","title":"Channel","required":true,"when":[{"key":"notify","op":"eq","value":true}],"type":"string","default":"mail","options":[{"value":"mail","label":"Mail"},{"value":"chat","label":"Chat"}],"custom":false},` +
	`{"key":"targets","title":"Targets","type":"multiselect","options":[{"value":"ios","label":"iOS"},{"value":"mac","label":"Mac"}],"default":["ios"]},` +
	`{"key":"mac_note","title":"Mac note","when":[{"key":"targets","op":"eq","value":"mac"}],"type":"string"},` +
	`{"key":"retries","title":"Retries","type":"integer","default":2},` +
	`{"key":"quiet_reason","title":"Why no notice","when":[{"key":"notify","op":"neq","value":true}],"type":"string"}]`

// Ask starts a turn that stops on a permission or a form and returns the
// request's id. The reply is the server's to write (permission.replied,
// form.replied) when the helper answers; EndAsk finishes the turn.
func (p *OpenCode2) Ask(l *Layout, kind string) (string, error) {
	g := l.g
	if p.ask != nil {
		return "", errors.New("sessiongen: a turn already waits")
	}
	if _, err := p.Prompt(g, p.main, "Step "+strconv.Itoa(g.L.Truth.Prompts+1)+": "+g.Text(30)); err != nil {
		return "", err
	}
	if err := p.emit(g, g.Tick(), "session.execution.started", false, map[string]any{"sessionID": p.main}); err != nil {
		return "", err
	}
	msg, err := p.step(g, p.main)
	if err != nil {
		return "", err
	}
	a := &ask2{msg: msg, call: g.Hex(32)}
	base := map[string]any{"sessionID": p.main, "assistantMessageID": msg, "id": a.call}
	var id string
	switch kind {
	case AskPermission:
		const cmd = "echo hi > a.txt"
		if err := p.emit(g, g.Tick(), "session.tool.input.started", true, with(base, "name", "shell")); err != nil {
			return "", err
		}
		if err := p.emit(g, g.Tick(), "session.tool.called", true, with(base, "input", map[string]any{"command": cmd})); err != nil {
			return "", err
		}
		id = ocID(g, "per")
		err = p.emit(g, g.Tick(), "permission.asked", true, map[string]any{"id": id, "sessionID": p.main, "action": "shell",
			"resources": []any{cmd}, "source": map[string]any{"type": "tool", "id": a.call}})
	case AskQuestion:
		a.form = true
		qs := []any{map[string]any{"question": "Do you prefer red or blue?", "header": "Color preference",
			"options": []any{map[string]any{"label": "Red", "description": "I prefer red"}, map[string]any{"label": "Blue", "description": "I prefer blue"}}}}
		if err := p.emit(g, g.Tick(), "session.tool.called", true, with(base, "input", map[string]any{"questions": qs})); err != nil {
			return "", err
		}
		id = ocID(g, "frm")
		field := map[string]any{"key": "q0", "title": "Color preference", "description": "Do you prefer red or blue?", "type": "string",
			"options": []any{map[string]any{"value": "Red", "label": "Red", "description": "I prefer red"},
				map[string]any{"value": "Blue", "label": "Blue", "description": "I prefer blue"}}, "custom": true}
		err = p.emit(g, g.Tick(), "form.created", true, map[string]any{"form": map[string]any{"id": id, "sessionID": p.main, "title": "Questions",
			"metadata": map[string]any{"kind": "question", "tool": map[string]any{"messageID": msg, "id": a.call}}, "fields": []any{field}}})
	case AskRelease:
		a.form, a.call = true, ""
		var fields []any
		if err := json.Unmarshal([]byte(releaseFields), &fields); err != nil {
			return "", err
		}
		id = ocID(g, "frm")
		err = p.emit(g, g.Tick(), "form.created", true, map[string]any{"form": map[string]any{"id": id, "sessionID": p.main,
			"title": "Release settings", "metadata": map[string]any{"kind": "release"}, "fields": fields}})
	default:
		return "", errors.New("sessiongen: no recording of an OpenCode 2.x " + kind)
	}
	if err != nil {
		return "", err
	}
	p.ask = a
	return id, g.flush()
}

// EndAsk finishes the turn Ask started, after the request was answered:
// the call's result, a reply, the end of the turn.
func (p *OpenCode2) EndAsk(l *Layout) error {
	g, a := l.g, p.ask
	if a == nil {
		return errors.New("sessiongen: no turn waits")
	}
	p.ask = nil
	if a.call != "" {
		base := map[string]any{"sessionID": p.main, "assistantMessageID": a.msg, "id": a.call}
		if err := p.toolDone(g, base, "(no output)"); err != nil {
			return err
		}
	}
	reply, err := p.step(g, p.main)
	if err != nil {
		return err
	}
	last, err := p.text(g, p.main, reply, 30)
	if err != nil {
		return err
	}
	g.L.Truth.Last = last
	if err := p.emit(g, g.Tick(), "session.execution.succeeded", false, map[string]any{"sessionID": p.main}); err != nil {
		return err
	}
	return g.flush()
}

// Hold starts a turn and stops in the middle of it, the agent at work: a
// prompt, the execution started, a step with reasoning. Finish or
// Interrupt ends it.
func (p *OpenCode2) Hold(l *Layout, prompt string) (msg string, err error) {
	g := l.g
	if _, err = p.Prompt(g, p.main, prompt); err != nil {
		return "", err
	}
	if err = p.emit(g, g.Tick(), "session.execution.started", false, map[string]any{"sessionID": p.main}); err != nil {
		return "", err
	}
	if msg, err = p.step(g, p.main); err != nil {
		return "", err
	}
	if err = p.thought(g, p.main, msg); err != nil {
		return "", err
	}
	return msg, g.flush()
}

// Finish ends a held turn with a reply.
func (p *OpenCode2) Finish(l *Layout, msg string) error {
	g := l.g
	if _, err := p.shell(g, p.main, msg, "echo held", 20); err != nil {
		return err
	}
	reply, err := p.step(g, p.main)
	if err != nil {
		return err
	}
	last, err := p.text(g, p.main, reply, 40)
	if err != nil {
		return err
	}
	g.L.Truth.Last = last
	if err := p.emit(g, g.Tick(), "session.execution.succeeded", false, map[string]any{"sessionID": p.main}); err != nil {
		return err
	}
	return g.flush()
}

// Stop ends a held turn as interrupted.
func (p *OpenCode2) Stop(l *Layout, msg string) error {
	if err := p.Interrupted(l.g, msg); err != nil {
		return err
	}
	return l.g.flush()
}

// Event writes one recorded event of a kind the turns do not (an agent or
// model switch, a revert, a reload, a moved session), with its data as
// recorded. It is for scenarios that replay what the terminal or the
// server does on its own.
func (p *OpenCode2) Event(l *Layout, typ string, loc bool, data map[string]any) error {
	if err := p.emit(l.g, l.g.Tick(), typ, loc, data); err != nil {
		return err
	}
	return l.g.flush()
}

// SetMain makes sess the session the terminal shows (after a resume).
func (p *OpenCode2) SetMain(sess string) { p.main = sess }

// Created writes a new top-level session, as the terminal's /new followed
// by a prompt makes one, and returns its id.
func (p *OpenCode2) Created(l *Layout) (string, error) {
	id, err := p.NewSession(l.g, "", "")
	if err != nil {
		return "", err
	}
	return id, l.g.flush()
}
