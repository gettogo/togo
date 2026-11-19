package opencode

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// chat stands in for the engine: when the mapper names another
// conversation, the chat starts over with a fresh mapper, which is handed
// what the old one carries and maps the announcing record again.
type chat struct {
	fresh func() harness.Mapper
	m     harness.Mapper
	conv  string
	overs int
}

func (c *chat) feed(r harness.Record) {
	c.m.Map(r)
	got := c.m.State()[harness.StateConversation]
	if got == "" || got == c.conv {
		return
	}
	if c.conv == "" {
		c.conv = got
		return
	}
	next := c.fresh()
	if carrier, ok := c.m.(harness.Carrier); ok {
		carrier.Carry(next)
	}
	c.m, c.overs = next, c.overs+1
	c.m.Map(r)
	c.conv = c.m.State()[harness.StateConversation]
}

// recordLine returns the recorded event on the given line of an sse file
// (1-based), with each old/new pair replaced.
func recordLine(t *testing.T, path string, line int, pairs ...string) harness.Record {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	data := strings.TrimPrefix(lines[line-1], "data: ")
	data = strings.NewReplacer(pairs...).Replace(data)
	if !json.Valid([]byte(data)) {
		t.Fatalf("%s:%d is not an event after the replacements", path, line)
	}
	return harness.Record{Source: "events", At: t0, Data: json.RawMessage(data)}
}

// OpenCode 1.x and Kilo Code: a new session is opened while a task
// subagent of the old one still runs. The chat starts over on the new
// session; the subagent's prompt updated afterwards (its summary, when its
// turn ends) is not a switch of the TUI to it, so the chat stays.
func TestChildOfTheLeftSessionNeverBecomesMain(t *testing.T) {
	const child, fresh = "ses_e4c1childsubagent00000001", "ses_e4c1newsessionattheTUI01"
	c := &chat{fresh: func() harness.Mapper { return NewEvents(harness.Env{SessionID: "s_oc_child"}) }}
	c.m = c.fresh()
	for _, r := range recorded(t) {
		c.feed(r)
	}
	const file = "testdata/events.sse"
	// The task tool's child session and its prompt.
	c.feed(recordLine(t, file, 3, `"id": "`+recordedSession+`", "slug"`, `"id": "`+child+`", "parentID": "`+recordedSession+`", "slug"`, recordedSession, child))
	c.feed(recordLine(t, file, 7, recordedSession, child, "msg_1b3e1efe3001r05Q37r0vH1Go0", "msg_childprompt00000000000001"))
	if c.conv != recordedSession || c.overs != 0 {
		t.Fatalf("before the new session: conversation %q, %d start-overs", c.conv, c.overs)
	}
	// A prompt in a new top-level session: the TUI moved to it.
	c.feed(recordLine(t, file, 7, recordedSession, fresh, "msg_1b3e1efe3001r05Q37r0vH1Go0", "msg_newsessionprompt000000001"))
	if c.conv != fresh || c.overs != 1 {
		t.Fatalf("after the new session: conversation %q, %d start-overs", c.conv, c.overs)
	}
	// The subagent's prompt updated with its summary.
	c.feed(recordLine(t, file, 7, recordedSession, child, "msg_1b3e1efe3001r05Q37r0vH1Go0", "msg_childprompt00000000000001",
		`"time": {`, `"summary": {"diffs": []}, "time": {`))
	if c.conv != fresh || c.overs != 1 {
		t.Fatalf("the subagent's late update moved the chat: conversation %q, %d start-overs", c.conv, c.overs)
	}
}

// OpenCode 2.x: the same, with a child of the first recorded session still
// running when the TUI moves to the second; the parent's next prompt to
// the child afterwards does not take the chat to the child.
func TestChildOfTheLeftSessionNeverBecomesMain2(t *testing.T) {
	const child = "ses_e486childsubagent0000001"
	const file = "testdata/v2/events.sse"
	c := &chat{fresh: func() harness.Mapper { return NewEvents2(harness.Env{SessionID: "s_oc2_child"}) }}
	c.m = c.fresh()
	recs := recorded2(t)
	split := -1
	for i, r := range recs {
		if strings.Contains(string(r.Data), `"session.created"`) && strings.Contains(string(r.Data), v2Probe) {
			split = i
			break
		}
	}
	if split < 0 {
		t.Fatal("the fixture changed: no second session")
	}
	for _, r := range recs[:split] {
		c.feed(r)
	}
	c.feed(recordLine(t, file, 179, `"sessionID": "`+v2Probe+`"`, `"sessionID": "`+child+`", "parentID": "`+v2Session+`"`, v2Probe, child))
	c.feed(recordLine(t, file, 3, v2Session, child, "msg_1b795ebb7001s6w6OWTldQYFJJ", "msg_childprompt00000000000001"))
	if c.conv != v2Session || c.overs != 0 {
		t.Fatalf("before the second session: conversation %q, %d start-overs", c.conv, c.overs)
	}
	for _, r := range recs[split:] {
		c.feed(r)
	}
	if c.conv != v2Probe || c.overs != 1 {
		t.Fatalf("after the second session: conversation %q, %d start-overs", c.conv, c.overs)
	}
	c.feed(recordLine(t, file, 3, v2Session, child, "msg_1b795ebb7001s6w6OWTldQYFJJ", "msg_childprompt00000000000002"))
	if c.conv != v2Probe || c.overs != 1 {
		t.Fatalf("the child's prompt moved the chat: conversation %q, %d start-overs", c.conv, c.overs)
	}
}
