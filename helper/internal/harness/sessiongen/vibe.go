package sessiongen

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

// Continue, as a turn's Prompt, goes on with the turn the last one left
// open (Hook.Pending): its remaining tools, then its end.
const Continue = "\x00continue"

// Vibe writes the files of Mistral Vibe 3.1: the recovery journal, the
// snapshot it is folded into after each turn, and meta.json, all under
// ~/.vibe/logs/session/unified/<session id>/.
//
// What the adapter reads (adapters/vibe), and what this profile writes for it:
//
//	meta.json            parent_session_id, environment.working_directory,
//	                     origin_directory (journalFor, vibe.go:57-80: which
//	                     session works in this folder), config.active_model
//	                     (history.go:98, readMeta)
//	journal/<seq>.jsonl  one record per line. projection_delta: payload.delta
//	                     ops append_entry, replace_entry, set_history_entries
//	                     and set_envelope (session.go:170-185), the envelope's
//	                     latestTurn {id, status, startedAt, completedAt,
//	                     error{code, message, details}} (session.go:392-426)
//	                     and session.id (session.go:189-199). callback_registered
//	                     and callback_resolved carry an approval (ask.go:228,
//	                     :324); a user_input callback is ignored, the ask
//	                     tool's effect carries the question (ask.go:87).
//	entries              message (role user or assistant, content[].text),
//	                     reasoning (text), effect (detail.toolName, input,
//	                     state.status and output; session.go:312-356) and the
//	                     checkpoint of a failed compaction is not written.
//	CURRENT, generations/<n>/manifest.json (projection_state.chunks),
//	generations/<n>/projection-state.json (snapshot), chunks/<sha256>.json
//	                     the snapshot history.go:42-88 reads once, at the
//	                     first journal record: the entries of every finished
//	                     turn, at most 1,000 of them (maxHistoryEntries).
//
// Behaviour taken from the real tree under adapters/vibe/testdata/home: after
// a turn Vibe writes a snapshot generation, whose manifest names an empty
// next segment (manifest.json recovery_journal_segment.first_sequence 22,
// journal/0000000000000022.jsonl with no lines), and keeps the last two
// segments (vibe.go:46-49). A turn that is still open lives in the newest
// segment alone.
//
// Shapes are from journal-tool, journal-approval, journal-approval-deny,
// journal-ask, journal-interrupted and journal-auth in adapters/vibe/testdata.
// Not written, because no capture holds the record: a settled compaction, a
// model or agent change, a failed tool other than a denied one, a queued
// prompt. The profile returns an error for a script that asks for them.
//
// Assumptions no capture settles: the chunk size of a snapshot (64 entries
// here) and that a hook payload never reaches the engine (Togo installs none).
type Vibe struct {
	cur  *vibeSession
	all  []*vibeSession
	dirs map[string]bool
}

type vibeSession struct {
	id, dir   string
	created   time.Time
	seq       int // last sequence written
	watermark int
	segPath   string
	gen       int
	history   []map[string]any // entries of finished turns
	open      []map[string]any // entries of the turn being written
	turn      string
	turnStart time.Time
	turnEntry string
	prompt    string
	steps     int
	turns     int
	sealed    []string     // names of the full chunks written
	pending   bool         // a turn was left open
	finish    func() error // settles the call or question it waits on
}

func (v *Vibe) Name() string { return "vibe" }

func (v *Vibe) Open(g *Gen) error {
	v.dirs = map[string]bool{}
	return v.start(g)
}

func (v *Vibe) root(g *Gen) string {
	return filepath.Join(g.Home, ".vibe", "logs", "session", "unified")
}

func (v *Vibe) start(g *Gen) error {
	s := &vibeSession{id: uuid(g), created: g.now}
	s.dir = filepath.Join(v.root(g), s.id)
	v.cur = s
	v.all = append(v.all, s)
	meta := filepath.Join(s.dir, "meta.json")
	g.L.Mains = append(g.L.Mains, meta)
	if err := v.writeMeta(g, s); err != nil {
		return err
	}
	return v.newSegment(g, s)
}

func (v *Vibe) writeMeta(g *Gen, s *vibeSession) error {
	m := map[string]any{
		"session_id": s.id, "parent_session_id": nil,
		"start_time": s.created.UTC().Format("2006-01-02T15:04:05.000000+00:00"), "end_time": nil,
		"git_commit": nil, "git_branch": nil,
		"environment":      map[string]any{"working_directory": g.Cwd},
		"origin_directory": g.Cwd, "username": "user", "child_sessions": []any{}, "loops": []any{},
		"title": nil, "title_source": "auto",
		"bumped_at": s.created.UTC().Format("2006-01-02T15:04:05.000000+00:00"), "pinned_at": nil, "archived_at": nil,
		"unseen_at": nil, "seen_at": nil, "experiments": nil,
		"config":       map[string]any{"active_model": "qwen5090", "model_name": "qwen3.8-27b"},
		"runtime_path": "direct", "import_provenance": nil, "created_worktree": nil, "project": nil, "artifacts": []any{},
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	return g.Replace(filepath.Join(s.dir, "meta.json"), g.Tick(), string(raw))
}

// newSegment starts the next journal segment, named by its first sequence.
func (v *Vibe) newSegment(g *Gen, s *vibeSession) error {
	s.segPath = filepath.Join(s.dir, "journal", fmt.Sprintf("%016d.jsonl", s.seq+1))
	return g.Replace(s.segPath, g.Tick(), "")
}

func vibeMS(t time.Time) int64 { return t.UnixMilli() }

func (v *Vibe) record(g *Gen, typ string, payload map[string]any, at time.Time) error {
	s := v.cur
	s.seq++
	return g.Emit(s.segPath, at, map[string]any{"payload": payload, "recovery_journal_record_version": 1, "sequence": s.seq, "type": typ})
}

func (v *Vibe) envelope(g *Gen, status string, at time.Time, fail map[string]any) map[string]any {
	s := v.cur
	lt := map[string]any{"id": s.turn, "inputEntryId": s.turnEntry, "queueItemId": nil, "sessionId": s.id, "startedAt": vibeMS(s.turnStart), "status": status}
	sess := map[string]any{"contextUsage": nil, "createdAt": vibeMS(s.created), "id": s.id, "parentSessionId": nil, "preview": s.prompt,
		"rootSessionId": s.id, "title": nil, "titleSource": "auto", "tokenUsage": nil, "updatedAt": vibeMS(at)}
	switch status {
	case "in_progress":
		sess["status"] = map[string]any{"activeTurnId": s.turn, "type": "running"}
	case "completed":
		lt["completedAt"], lt["stopReason"] = vibeMS(at), nil
		sess["status"] = map[string]any{"type": "idle"}
	case "interrupted":
		lt["completedAt"], lt["reason"] = vibeMS(at), "client interrupt"
		sess["status"] = map[string]any{"type": "idle"}
	case "failed":
		lt["completedAt"], lt["error"] = vibeMS(at), fail
		sess["status"] = map[string]any{"type": "failed", "message": fail["message"]}
	}
	return map[string]any{"activeCallbacks": []any{}, "format": "harness.public-session-state/v1",
		"history":    map[string]any{"cursor": map[string]any{"after": nil, "before": nil}, "entries": []any{}, "range": "latest"},
		"latestTurn": lt, "session": sess, "turnQueue": map[string]any{"items": []any{}, "maxItems": 32, "paused": false}}
}

// delta writes a projection_delta with ops and the envelope.
func (v *Vibe) delta(g *Gen, at time.Time, status string, fail map[string]any, ops ...map[string]any) error {
	s := v.cur
	s.watermark++
	list := make([]any, 0, len(ops)+1)
	for _, o := range ops {
		list = append(list, o)
	}
	list = append(list, map[string]any{"op": "set_envelope", "state": v.envelope(g, status, at, fail)})
	return v.record(g, "projection_delta", map[string]any{"delta": list, "watermark": s.watermark}, at)
}

func (v *Vibe) add(e map[string]any) map[string]any {
	v.cur.open = append(v.cur.open, e)
	return map[string]any{"op": "append_entry", "entry": e}
}

func (v *Vibe) swap(e map[string]any) map[string]any {
	for i, o := range v.cur.open {
		if o["id"] == e["id"] {
			v.cur.open[i] = e
		}
	}
	return map[string]any{"op": "replace_entry", "entry": e, "id": e["id"]}
}

func (v *Vibe) base(typ, id string, at time.Time) map[string]any {
	s := v.cur
	return map[string]any{"type": typ, "id": id, "sessionId": s.id, "turnId": s.turn, "inputEntryId": s.turnEntry,
		"relatedEntryId": nil, "createdAt": vibeMS(at), "updatedAt": vibeMS(at), "generationStatus": "completed"}
}

func vibeDisplay(verb, settled, status string) map[string]any {
	return map[string]any{"message": "file_system.bash", "settledMessage": "file_system.bash", "settledVerb": "Ran",
		"statusText": status + " file_system.bash", "summary": "file_system.bash", "verb": verb}
}

func (v *Vibe) Turn(g *Gen, t Turn) error {
	h := t.Hook
	if h == nil {
		h = &HookTurn{}
	}
	switch {
	case t.Compact:
		return fmt.Errorf("sessiongen: vibe: no capture holds a settled compaction")
	case t.Jobs > 0 || t.Agents != nil:
		return fmt.Errorf("sessiongen: vibe has no background job or subagent record")
	case h.Model != "" || h.Steer || h.Aborted || h.NoStop:
		return fmt.Errorf("sessiongen: vibe: no capture holds a model change, a queued prompt or a missing end")
	}
	switch {
	case h.Resume && len(v.all) > 1:
		// vibe --resume <id>: the earlier session continues.
		v.cur = v.all[len(v.all)-2]
		v.all[len(v.all)-2], v.all[len(v.all)-1] = v.all[len(v.all)-1], v.all[len(v.all)-2]
		if err := v.newSegment(g, v.cur); err != nil {
			return err
		}
	case t.New:
		if err := v.start(g); err != nil {
			return err
		}
	}
	s := v.cur
	cont := t.Prompt == Continue
	if cont && !s.pending {
		return fmt.Errorf("sessiongen: vibe has no open turn to continue")
	}
	s.pending = false
	if cont && s.finish != nil {
		f := s.finish
		s.finish = nil
		if err := f(); err != nil {
			return err
		}
	}
	if !cont {
		prompt := t.Prompt
		if prompt == "" {
			prompt = fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts+1, g.Text(40))
		}
		g.L.Truth.Prompts++
		s.turns++
		s.turn = "turn-" + g.Hex(32)
		s.turnStart = g.Tick()
		s.turnEntry = fmt.Sprintf("user-%s-1", s.turn)
		s.prompt, s.steps = prompt, 0
		s.open = nil
		user := v.base("message", s.turnEntry, s.turnStart)
		user["role"], user["source"], user["content"] = "user", "turn_start", []any{map[string]any{"type": "text", "text": prompt}}
		user["inputEntryId"] = nil
		if err := v.delta(g, s.turnStart, "in_progress", nil, v.add(user)); err != nil {
			return err
		}
	}
	reason := func() error {
		at := g.Tick()
		e := v.base("reasoning", "reasoning-"+uuid(g)+"-0", at)
		e["outcome"], e["summary"], e["text"] = map[string]any{"type": "committed"}, []any{}, g.Text(60)+"\n"
		return v.delta(g, at, "in_progress", nil, v.add(e))
	}
	for i := 0; i < t.Tools; i++ {
		if err := reason(); err != nil {
			return err
		}
		if err := v.tool(g, t, h, i); err != nil {
			return err
		}
		if h.Pending && i == t.Tools-1 && h.Ask == nil {
			s.pending = true
			return nil
		}
	}
	if a := h.Ask; a != nil {
		if err := reason(); err != nil {
			return err
		}
		if err := v.ask(g, a, h.Pending); err != nil {
			return err
		}
		if h.Pending {
			s.pending = true
			return nil
		}
	}
	at := g.Tick()
	switch {
	case t.Interrupt:
		if err := v.delta(g, at, "interrupted", nil); err != nil {
			return err
		}
		g.L.Truth.Last = s.turn
	case t.Error:
		fail := map[string]any{"code": "model_unauthorized", "message": firstNonEmpty(h.ErrorText, "Invalid API key. Please check your API key and try again."),
			"details": map[string]any{"exceptionType": "HTTPStatusError", "httpStatus": 401, "model": "qwen3.8-27b",
				"provider": "qwen5090", "purpose": "agent", "reason": "http_unauthorized"}}
		if err := v.delta(g, at, "failed", fail); err != nil {
			return err
		}
		g.L.Truth.Last = s.turn + ":error"
	default:
		if err := reason(); err != nil {
			return err
		}
		at = g.Tick()
		id := "assistant-" + uuid(g)
		e := v.base("message", id, at)
		e["role"], e["source"], e["outcome"] = "assistant", "harness", map[string]any{"type": "committed"}
		e["content"] = []any{map[string]any{"type": "text", "text": g.Text(max(t.Say, 1))}}
		if err := v.delta(g, at, "completed", nil, v.add(e)); err != nil {
			return err
		}
		g.L.Truth.Last = id
	}
	status := "completed"
	switch {
	case t.Error:
		status = "failed"
	case t.Interrupt:
		status = "interrupted"
	}
	if err := v.snapshot(g, at, status); err != nil {
		return err
	}
	if t.Exit {
		g.L.Truth.Exited = true
	}
	return nil
}

func (v *Vibe) tool(g *Gen, t Turn, h *HookTurn, i int) error {
	s := v.cur
	at := g.Tick()
	id := "effect-" + uuid(g)
	cmd := "echo " + g.Hex(4)
	in := map[string]any{"command": cmd, "timeout_seconds": 120}
	e := v.base("effect", id, at)
	e["generationStatus"] = "in_progress"
	e["title"] = "file_system.bash"
	e["detail"] = map[string]any{"display": vibeDisplay("Running", "Ran", "Running"), "input": in, "kind": "tool", "toolName": "file_system.bash"}
	e["state"] = map[string]any{"outputText": "", "status": "running"}
	s.steps++
	if err := v.delta(g, at, "in_progress", nil, v.add(e)); err != nil {
		return err
	}
	approval := i < h.Approve
	denied := approval && i < h.Fail
	if approval {
		cb := "approval-" + g.Hex(32) + "-" + g.Hex(8)
		at = g.Tick()
		routing := map[string]any{"callbackId": cb, "createdAt": vibeMS(at), "generationStatus": "in_progress", "id": cb,
			"inputEntryId": s.turnEntry, "relatedEntryId": id, "sessionId": s.id, "state": map[string]any{"status": "open"},
			"title": "Approve file_system.bash", "turnId": s.turn, "type": "callback", "updatedAt": vibeMS(at),
			"detail": map[string]any{"choices": []any{"approve", "approve_for_session", "approve_permanently", "deny", "cancel_turn"},
				"effect": map[string]any{"display": map[string]any{"content": nil, "message": "file_system.bash", "settledMessage": "file_system.bash",
					"settledVerb": "Ran", "statusText": "Waiting for approval to run file_system.bash", "suffix": "", "summary": "file_system.bash", "verb": "Run"},
					"input": in, "kind": "tool", "toolName": "file_system.bash"},
				"kind": "approval", "reason": nil, "relatedEntryId": id,
				"requiredPermissions": []any{map[string]any{"invocationPattern": cmd, "label": "echo *", "pathScopeRoot": nil, "scope": "command_pattern", "sessionPattern": "echo *"}}}}
		if err := v.record(g, "callback_registered", map[string]any{"callback_id": cb, "kind": "approval", "routing": routing}, at); err != nil {
			return err
		}
		resolve := func() error {
			at := g.Tick()
			dec := "approve"
			if denied {
				dec = "deny"
			}
			if err := v.record(g, "callback_resolved", map[string]any{"callback_id": cb, "state": "resolved", "result": map[string]any{
				"callbackId": cb, "output": map[string]any{"decision": map[string]any{"type": dec}, "feedback": nil, "type": "approval"}}}, at); err != nil {
				return err
			}
			if err := v.delta(g, at, "in_progress", nil); err != nil {
				return err
			}
			return v.settle(g, e, in, g.Tick(), approval, denied, t.Output)
		}
		if h.Pending && i == t.Tools-1 && h.Ask == nil {
			s.finish = resolve
			g.L.Truth.Last = cb
			return nil
		}
		return resolve()
	}
	if h.Pending && i == t.Tools-1 && h.Ask == nil {
		s.finish = func() error { return v.settle(g, e, in, g.Tick(), false, false, t.Output) }
		g.L.Truth.Last = id
		return nil // the call is still running
	}
	return v.settle(g, e, in, g.Tick(), approval, denied, t.Output)
}

func (v *Vibe) settle(g *Gen, e map[string]any, in map[string]any, at time.Time, approval, denied bool, size int) error {
	n := map[string]any{}
	for k, val := range e {
		n[k] = val
	}
	n["generationStatus"], n["updatedAt"] = "completed", vibeMS(at)
	src, typ := "config", "always"
	if approval {
		src, typ = "user", "ask"
	}
	st := map[string]any{"approvalSource": src, "approvalType": typ}
	if denied {
		st["decision"] = "skip"
		st["display"] = map[string]any{"message": "Tool execution denied by approval callback", "success": false}
		st["error"] = map[string]any{"code": "tool_denied", "details": nil, "message": "Tool execution denied by approval callback"}
		st["output"] = map[string]any{"_meta": map[string]any{"approval": map[string]any{"approvalSource": src, "approvalType": typ, "decision": "skip"}},
			"content": []any{}, "error": map[string]any{"code": "tool_denied", "message": "Tool execution denied by approval callback", "retryable": false}, "type": "failure"}
		st["outputText"], st["status"] = "", "failed"
	} else {
		st["decision"] = "execute"
		st["display"] = map[string]any{"message": "file_system.bash", "success": true}
		st["output"] = map[string]any{"_meta": map[string]any{"approval": map[string]any{"approvalSource": src, "approvalType": typ, "decision": "execute"}},
			"content": []any{}, "type": "success", "structured_content": map[string]any{"command": in["command"], "returncode": 0, "stderr": "",
				"stdout": g.Text(max(size, 1)) + "\n", "timed_out": false, "was_truncated": false}}
		st["outputText"], st["status"] = "", "completed"
	}
	n["state"] = st
	return v.delta(g, at, "in_progress", nil, v.swap(n))
}
