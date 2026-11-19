package sessiongen

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

// Goose writes the session database of Goose 1.57,
// ~/.local/share/goose/sessions/sessions.db: one sqlite file, tables
// sessions and messages, which the helper reads read-only with the system
// sqlite3 (source/sqlite.go) and adapters/goose maps (goose.yaml feeds).
//
// What the adapter reads, and what this profile writes for it:
//
//	sessions  id, working_dir, goose_mode, name, created_at, updated_at,
//	          session_type (the feed's where, goose.yaml:30-35); the mapper
//	          picks the row working in the folder and made since the Togo
//	          session began (session.go:145-207) and keeps goose_mode
//	messages  id, message_id, session_id, role, content_json,
//	          created_timestamp, metadata_json (session.go:70-78). Parts:
//	          text, thinking (session.go:274-336), toolRequest and
//	          toolResponse (:312-319, :283), actionRequired with
//	          toolConfirmation and toolConfirmationResponse, or
//	          elicitation and elicitationResponse (question.go:56-86), and
//	          error (:327). metadata_json userVisible false hides a row
//	          (session.go:258).
//
// Shapes are from adapters/goose/testdata: messages.jsonl (a reply, a
// shell call), approvals.jsonl (confirmation allowed and denied, a tool
// call interrupted), elicitation.jsonl, errors.jsonl and sessions.jsonl,
// which are rows of a real 1.57 database. The column types are Goose's: id
// is the integer rowid, created_timestamp seconds, the other times text.
//
// Not written, because no capture holds the row: a compaction part, a
// confirmation prompt (a security finding), a cancelled elicitation, an
// elicitation that is still open (Goose stores it only once answered,
// goose.yaml:20-21), and a failed tool result of status error. The profile
// returns an error for a script that asks for one.
type Goose struct {
	// Mode is the sessions row's goose_mode: auto, approve, smart_approve
	// or chat (default auto).
	Mode string

	db      sqliteFile
	cwd     string
	sess    []string // session ids, oldest first
	cur     string
	pending func(g *Gen) error
	n       int // messages written, for ids
	mode    string
}

func (o *Goose) Name() string { return "goose" }

const gooseSchema = `CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, name TEXT, description TEXT, working_dir TEXT, goose_mode TEXT, session_type TEXT, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS messages (id INTEGER PRIMARY KEY AUTOINCREMENT, message_id TEXT, session_id TEXT NOT NULL, role TEXT NOT NULL, content_json TEXT NOT NULL, created_timestamp INTEGER NOT NULL, metadata_json TEXT);`

func gooseTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05") }

func (o *Goose) Open(g *Gen) error {
	o.db = sqliteFile{path: filepath.Join(g.Home, ".local", "share", "goose", "sessions", "sessions.db"), schema: gooseSchema}
	o.cwd, o.mode = g.Cwd, firstNonEmpty(o.Mode, "auto")
	g.L.Mains = append(g.L.Mains, o.db.path)
	return o.start(g)
}

func (o *Goose) start(g *Gen) error {
	at := g.Tick()
	o.cur = fmt.Sprintf("%s_%d", at.UTC().Format("20060102"), len(o.sess)+1)
	o.sess = append(o.sess, o.cur)
	o.db.add("INSERT INTO sessions (id, name, working_dir, goose_mode, session_type, created_at, updated_at) VALUES (%s, %s, %s, %s, 'user', %s, %s)",
		sq(o.cur), sq("CLI Session"), sq(o.cwd), sq(o.mode), sq(gooseTime(at)), sq(gooseTime(at)))
	return nil
}

// msg queues one message row.
func (o *Goose) msg(g *Gen, role string, parts []any, meta map[string]any, id string) {
	at := g.Tick()
	o.n++
	raw, _ := json.Marshal(parts)
	m, _ := json.Marshal(meta)
	o.db.add("INSERT INTO messages (message_id, session_id, role, content_json, created_timestamp, metadata_json) VALUES (%s, %s, %s, %s, %d, %s)",
		sq(id), sq(o.cur), sq(role), sq(string(raw)), at.Unix(), sq(string(m)))
	o.db.add("UPDATE sessions SET updated_at = %s WHERE id = %s", sq(gooseTime(at)), sq(o.cur))
}

var (
	gooseVisible = map[string]any{"userVisible": true, "agentVisible": true}
	gooseHidden  = map[string]any{"userVisible": false, "agentVisible": false}
)

func (o *Goose) genID(g *Gen) string { return fmt.Sprintf("gen-%d-%s", g.now.Unix(), g.Hex(20)) }

func (o *Goose) Turn(g *Gen, t Turn) error {
	h := t.Hook
	if h == nil {
		h = &HookTurn{}
	}
	switch {
	case t.Compact:
		return fmt.Errorf("sessiongen: goose: no capture holds a compaction part")
	case t.Jobs > 0 || t.Agents != nil:
		return fmt.Errorf("sessiongen: goose has no background job or subagent row")
	case h.Model != "" || h.Steer || h.Aborted || h.NoStop || h.Ask != nil && (h.Ask.Cancel || h.Pending || h.Ask.Plan != ""):
		return fmt.Errorf("sessiongen: goose: no capture holds a cancelled or open elicitation, a model change or a queued prompt")
	case t.Interrupt && t.Tools == 0:
		return fmt.Errorf("sessiongen: goose: an interrupt is recorded only on a tool call")
	}
	switch {
	case h.Resume && len(o.sess) > 1:
		o.cur = o.sess[len(o.sess)-2]
	case t.New:
		if err := o.start(g); err != nil {
			return err
		}
	}
	if t.Prompt == Continue {
		if o.pending == nil {
			return fmt.Errorf("sessiongen: goose has no open turn to continue")
		}
		f := o.pending
		o.pending = nil
		if err := f(g); err != nil {
			return err
		}
		return o.finish(g, t)
	}
	o.pending = nil
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts+1, g.Text(40))
	}
	g.L.Truth.Prompts++
	o.msg(g, "user", []any{map[string]any{"type": "text", "text": prompt}}, gooseVisible, "msg_"+uuid(g))
	o.msg(g, "user", []any{map[string]any{"type": "text", "text": "<turn-context>\n<current-time>" + g.now.Format("2006-01-02 15:04:00 -07:00") +
		"</current-time>\n<working-directory>" + o.cwd + "</working-directory>\n</turn-context>"}},
		map[string]any{"userVisible": false, "agentVisible": true, "turnContext": true}, "msg_"+uuid(g))
	for i := 0; i < t.Tools; i++ {
		if err := o.tool(g, t, h, i); err != nil {
			return err
		}
		if o.pending != nil {
			return nil
		}
	}
	if a := h.Ask; a != nil {
		o.elicit(g, a)
	}
	return o.finish(g, t)
}

// tool writes one shell call: its request, an optional confirmation, and
// its response. A pending call stops before its response.
func (o *Goose) tool(g *Gen, t Turn, h *HookTurn, i int) error {
	call := g.Hex(32)
	cmd := "echo " + g.Hex(4)
	parts := []any{map[string]any{"type": "thinking", "thinking": g.Text(50), "signature": ""},
		map[string]any{"type": "toolRequest", "id": call, "toolCall": map[string]any{"status": "success", "value": map[string]any{"name": "shell",
			"arguments": map[string]any{"command": cmd}}}, "_meta": map[string]any{"goose.toolCall.providerIndex": 0, "goose_extension": "developer", "goose.executable": false}}}
	o.msg(g, "assistant", parts, gooseVisible, o.genID(g))
	approval := i < h.Approve
	denied := approval && i < h.Fail
	answer := func(g *Gen) error {
		perm := "allow_once"
		out := g.Text(max(t.Output, h.Large, 1))
		isErr := false
		if denied {
			perm = "deny_once"
			out = "The user has declined to run this tool. DO NOT attempt to call this tool again. If there are no alternative methods to proceed, clearly explain the situation and STOP."
			isErr = true
		}
		if approval {
			o.msg(g, "user", []any{map[string]any{"type": "actionRequired", "data": map[string]any{"actionType": "toolConfirmationResponse", "id": call, "permission": perm}}},
				gooseHidden, "msg_"+uuid(g))
		}
		if t.Interrupt && i == t.Tools-1 {
			out, isErr = "Tool call was interrupted before completing", true
			g.L.Truth.Last = call
		}
		o.msg(g, "user", []any{map[string]any{"type": "toolResponse", "id": call, "toolResult": map[string]any{"status": "success", "value": map[string]any{
			"resultType": "complete", "content": []any{map[string]any{"type": "text", "text": out, "annotations": map[string]any{"priority": 0.0}}},
			"structuredContent": map[string]any{"stdout": out, "stderr": "", "exit_code": 0}, "isError": isErr}}}}, gooseVisible, "msg_"+uuid(g))
		return nil
	}
	if approval {
		o.msg(g, "assistant", []any{map[string]any{"type": "actionRequired", "data": map[string]any{"actionType": "toolConfirmation", "id": call,
			"toolName": "shell", "arguments": map[string]any{"command": cmd}, "prompt": nil}}}, map[string]any{"userVisible": true, "agentVisible": false}, o.genID(g))
	}
	if h.Pending && i == t.Tools-1 && h.Ask == nil {
		o.pending = answer
		g.L.Truth.Last = map[bool]string{true: "confirm-" + call, false: call}[approval]
		return nil
	}
	return answer(g)
}

// elicit writes an MCP tool that asks one enum question (elicitation.jsonl):
// the request, then the response, which Goose stores before the request.
func (o *Goose) elicit(g *Gen, a *HookAsk) {
	call, id := g.Hex(32), uuid(g)
	enum := make([]any, 0, len(a.Options))
	for _, v := range a.Options {
		enum = append(enum, v)
	}
	o.msg(g, "assistant", []any{map[string]any{"type": "toolRequest", "id": call, "toolCall": map[string]any{"status": "success", "value": map[string]any{
		"name": "python3__pick", "arguments": map[string]any{}}}, "_meta": map[string]any{"goose.toolCall.providerIndex": 0, "goose_extension": "python3"}}}, gooseVisible, o.genID(g))
	o.msg(g, "user", []any{map[string]any{"type": "actionRequired", "data": map[string]any{"actionType": "elicitationResponse", "id": id,
		"user_data": map[string]any{"colour": a.Answer}, "action": "accept"}}}, map[string]any{"userVisible": false, "agentVisible": true}, "msg_"+uuid(g))
	o.msg(g, "assistant", []any{map[string]any{"type": "actionRequired", "data": map[string]any{"actionType": "elicitation", "id": id, "message": a.Prompt,
		"requested_schema": map[string]any{"type": "object", "properties": map[string]any{"colour": map[string]any{"type": "string", "title": "Colour", "description": "Banner colour", "enum": enum}},
			"required": []any{"colour"}}}}}, gooseVisible, o.genID(g))
	o.msg(g, "user", []any{map[string]any{"type": "toolResponse", "id": call, "toolResult": map[string]any{"status": "success", "value": map[string]any{
		"content": []any{map[string]any{"type": "text", "text": "elicitation result: " + a.Answer}}}}}}, gooseVisible, "msg_"+uuid(g))
}

// finish writes the end of the turn: the reply, an error, or nothing for
// an interrupt.
func (o *Goose) finish(g *Gen, t Turn) error {
	h := t.Hook
	if h == nil {
		h = &HookTurn{}
	}
	switch {
	case t.Interrupt:
		// The last record is the call's interrupted response (its id is
		// Last); the turn has no reply.
	case t.Error:
		id := o.genID(g)
		words := firstNonEmpty(h.ErrorText, "Network error: Could not connect to 127.0.0.1:9 — check your network connection and try again.\n\nPlease resend your message to try again.")
		o.msg(g, "assistant", []any{map[string]any{"type": "error", "kind": "other", "message": words}}, map[string]any{"userVisible": true, "agentVisible": false}, id)
		g.L.Truth.Last = id + ":0"
	default:
		id := o.genID(g)
		o.msg(g, "assistant", []any{map[string]any{"type": "thinking", "thinking": g.Text(40), "signature": ""},
			map[string]any{"type": "text", "text": g.Text(max(t.Say, 1))}}, gooseVisible, id)
		g.L.Truth.Last = id + ":1"
	}
	if t.Exit {
		g.L.Truth.Exited = true
	}
	return nil
}

// Flushed writes the queued rows.
func (o *Goose) Flushed(g *Gen) error { return o.db.flush() }
