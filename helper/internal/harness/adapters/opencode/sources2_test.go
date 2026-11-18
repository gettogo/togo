package opencode

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// fixture2 decodes a recorded 2.x API answer from testdata/v2.
func fixture2(t *testing.T, name string) any {
	t.Helper()
	b, err := os.ReadFile("testdata/v2/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// Seen live on 2.3.0: an archived session stayed in the resume picker. The
// terminal's list asks for archived=false, so the picker asks the same, and
// drops an archived entry should the server send one anyway.
// testdata/v2/sessions-archived.json is the first three sessions a real
// /api/session answered, the second one archived (folder names replaced).
func TestResumePickerLeavesOutArchivedSessions(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	src := a.Sources["sessions"]
	path, err := engine.Render(src.HTTP.Path, map[string]any{"cwd": "/w/project"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "&archived=false") {
		t.Errorf("sessions path %s does not ask for archived=false", path)
	}
	v, err := engine.JQFirst(src.Select, fixture2(t, "sessions-archived.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, s := range v.([]any) {
		titles = append(titles, s.(map[string]any)["title"].(string))
	}
	if got := strings.Join(titles, "|"); got != "Audit fixture API create probe|Audit fixture recent 55" {
		t.Fatalf("picker lists %s", got)
	}
}

// Seen live on 2.3.0: /api/agent listed a custom primary agent
// (audit-observer) and set_mode took its id, but the mode chip offered only
// Build and Plan. The chip reads the agents source, which lists what the
// terminal's agent switch does: primary agents that are not hidden.
// testdata/v2/agents-custom.json is what the server listed (id, name, mode
// and hidden of each agent).
func TestModesListCustomPrimaryAgents(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	if a.Modes == nil || a.Modes.Source != "agents" {
		t.Fatalf("modes do not read the agents source: %+v", a.Modes)
	}
	var written []string
	for _, c := range a.Modes.Choices {
		written = append(written, c.ID)
	}
	if got := strings.Join(written, " "); got != "build plan" {
		t.Fatalf("choices served when the source is down: %s", got)
	}
	v, err := engine.JQFirst(a.Sources["agents"].Select, fixture2(t, "agents-custom.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range v.([]any) {
		ids = append(ids, m.(map[string]any)["id"].(string))
	}
	if got := strings.Join(ids, " "); got != "build plan audit-observer" {
		t.Fatalf("agents source lists %s", got)
	}
}

// Seen live on 2.3.0: a command added to the project was listed by
// /api/command after a reload, and the / menu kept its five-minute copy.
// The server says when its commands and agents changed (command.updated,
// agent.updated); the mapper turns each into a new commands_rev or
// agents_rev, and the two sources are read again when theirs changes.
// testdata/v2/reload.sse is what a 2.2.5 server sent after
// POST /api/location/reload with a new command and a new agent on disk.
func TestReloadMakesCommandsAndAgentsStale(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	for src, rev := range map[string]string{"commands": "commands_rev", "agents": "agents_rev"} {
		if got := a.Sources[src].CacheRev; got != rev {
			t.Errorf("source %s is renewed by %q, want %q", src, got, rev)
		}
	}
	m := NewEvents2(harness.Env{Cwd: "/w"})
	for _, rec := range recorded2(t) {
		m.Map(rec)
	}
	before := m.State()
	for _, rec := range recordedSSE(t, "testdata/v2/reload.sse") {
		m.Map(rec)
	}
	after := m.State()
	for _, rev := range []string{"commands_rev", "agents_rev"} {
		if after[rev] == "" || after[rev] == before[rev] {
			t.Errorf("%s was %q and is %q after the reload", rev, before[rev], after[rev])
		}
	}
	// Events sent while the stream was down are lost, a reload among them:
	// after a reconnect both lists are read again.
	m.Map(harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(`{"type":"` + httpsrc.Reconnected + `"}`)})
	for _, rev := range []string{"commands_rev", "agents_rev"} {
		if got := m.State()[rev]; got == "" || got == after[rev] {
			t.Errorf("%s is still %q after a reconnect", rev, got)
		}
	}
}

// Seen live on 2.3.0: one finished child session was two rows under Agents,
// the mapper's (its chat and Stop) and the children source's. The engine
// keeps one row when both name the child alike, so both must use the
// session id. testdata/v2/children.json is what /api/session?parentID=
// answered for a real child; the session.created event is built from that
// child's own record in the event's recorded shape.
func TestChildIsNamedAlikeByTheMapperAndTheChildrenSource(t *testing.T) {
	const parent, child = "ses_e28f4c58affePFJ6qUEQH9BCR7", "ses_e28d51c92ffeuZKPOJEAMrXUPw"
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Mission[harness.SectionAgents].Source; got != "children" {
		t.Fatalf("agents rows come from %q", got)
	}
	v, err := engine.JQFirst(a.Sources["children"].Select, fixture2(t, "children.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := v.([]any)
	if len(rows) != 1 {
		t.Fatalf("children source lists %v", rows)
	}
	listed := rows[0].(map[string]any)
	if listed["id"] != child || listed["state"] != "done" {
		t.Fatalf("children source row %v", listed)
	}

	m := NewEvents2(harness.Env{Argv: []string{"opencode", "--session", parent}})
	created := `{"id":"evt_1d72c8a33001","created":1794611274611,"type":"session.created","data":{"sessionID":"` + child +
		`","parentID":"` + parent + `","projectID":"be040d472edc93e9f486658824b9d19137761562","agent":"general",` +
		`"slug":"audit-child","version":"2.3.0","title":"Audit read-only child","permissions":[],"location":{"directory":"/w/project"}}}`
	_, ops := m.Map(harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(created)})
	if len(ops) != 1 || ops[0].Row == nil || ops[0].Section != harness.SectionAgents {
		t.Fatalf("mapper ops %+v", ops)
	}
	if ops[0].Row.ID != listed["id"] {
		t.Fatalf("the mapper names the child %q, the source %q", ops[0].Row.ID, listed["id"])
	}
}

// Seen live on 2.3.0: a conversation moved to another folder and the @ menu
// kept listing the first one. The mapper reports the folder a session.moved
// names as conversation_cwd, which the engine lists files from, and forgets
// it when the terminal goes to another conversation, whose folder no event
// names.
// testdata/v2/moved.sse is what a 2.2.5 server sent for a session made in
// /tmp/work and moved with POST /api/session/{id}/move (folders renamed).
func TestMovedConversationReportsItsFolder(t *testing.T) {
	m := NewEvents2(harness.Env{Cwd: "/tmp/work"})
	recs := recordedSSE(t, "testdata/v2/moved.sse")
	for _, rec := range recs[:1] {
		m.Map(rec)
	}
	if st := m.State(); st["harness_session"] != "ses_e248793fbffe1C9pGgD1Q9qW5k" || st["conversation_cwd"] != "" {
		t.Fatalf("before the move: %v", st)
	}
	for _, rec := range recs[1:] {
		m.Map(rec)
	}
	if got := m.State()["conversation_cwd"]; got != "/tmp/alternate" {
		t.Fatalf("folder after the move = %q", got)
	}
	// Another session's move is not this conversation's.
	other := `{"type":"session.moved","data":{"sessionID":"ses_other","location":{"directory":"/tmp/elsewhere"},"projectID":"p","subpath":""}}`
	m.Map(harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(other)})
	if got := m.State()["conversation_cwd"]; got != "/tmp/alternate" {
		t.Fatalf("folder after another session moved = %q", got)
	}
	// A prompt in another top-level session: the terminal went there.
	enq := `{"type":"session.inbox.enqueued","data":{"sessionID":"ses_next","inboxID":"msg_1","item":{"type":"user","payload":{"text":"hi"}}}}`
	m.Map(harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(enq)})
	if st := m.State(); st["harness_session"] != "ses_next" || st["conversation_cwd"] != "" {
		t.Fatalf("after the terminal moved to another conversation: %v", st)
	}
}

// Seen live on 2.3.0: 56 sessions existed, the picker listed 50, and typing
// the oldest one's name found nothing though OpenCode's own search did. The
// picker's search text now goes to the server.
func TestResumePickerSearchGoesToTheServer(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	sc := a.Screens["resume"]
	if !sc.Search || sc.SearchMode != "remote" {
		t.Fatalf("resume screen search = %v %q, want remote", sc.Search, sc.SearchMode)
	}
	path := a.Sources["sessions"].HTTP.Path
	with, err := engine.Render(path, map[string]any{"cwd": "/w", "search": "oldest target"})
	if err != nil || !strings.HasSuffix(with, "&search=oldest+target") {
		t.Errorf("with a search: %q %v", with, err)
	}
	without, err := engine.Render(path, map[string]any{"cwd": "/w"})
	if err != nil || strings.Contains(without, "search") {
		t.Errorf("without a search: %q %v", without, err)
	}
}
