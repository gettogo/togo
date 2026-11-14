package omp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func line(feed, raw string) harness.Record {
	return harness.Record{Source: feed, At: t0, Data: json.RawMessage(raw)}
}

// Seen live on omp 18.20: /fork writes a new main conversation whose header
// names the one it came from as a bare id. It was taken for a subagent of
// another session and its replies never reached the chat; a cold read made
// it a phantom subagent row. A file of the session feed is the terminal's
// own conversation whatever its header says of a parent.
func TestForkedConversationIsTheMainOne(t *testing.T) {
	m := newSession(env())
	out := run(m, []harness.Record{
		line("session", `{"type":"session","version":3,"id":"aaaa1111","timestamp":"2026-11-14T00:00:00Z"}`),
		line("session", `{"type":"message","id":"u1","timestamp":"2026-11-14T00:00:01Z","message":{"role":"user","content":"first"}}`),
		line("session", `{"type":"session","version":3,"id":"bbbb2222","parentSession":"aaaa1111","timestamp":"2026-11-14T00:01:00Z"}`),
		line("session", `{"type":"message","id":"u2","timestamp":"2026-11-14T00:01:01Z","message":{"role":"user","content":"in the fork"}}`),
		line("session", `{"type":"message","id":"a2","parentId":"u2","timestamp":"2026-11-14T00:01:02Z","message":{"role":"assistant","content":[{"type":"text","text":"OMP_FORK_LOCAL"}]}}`),
	})
	st := m.State()
	if st["harness_session"] != "bbbb2222" || st["conversation"] != "bbbb2222" {
		t.Errorf("state = %q / %q, want the fork as the conversation", st["harness_session"], st["conversation"])
	}
	reply := false
	for _, it := range out.Items {
		if it.Kind == harness.KindAgent {
			t.Errorf("the fork became a subagent row: %+v", it.Agent)
		}
		if it.Assistant != nil && strings.Contains(it.Assistant.Text, "OMP_FORK_LOCAL") && it.Parent == "" {
			reply = true
		}
	}
	if !reply {
		t.Error("the fork's reply is not in the main chat")
	}
}

// A subagent's file still belongs to its parent: the subagents feed, and a
// header that names a parent, stay a subagent.
func TestSubagentHeaderStaysASubagent(t *testing.T) {
	m := newSession(env())
	out := run(m, []harness.Record{
		line("session", `{"type":"session","version":3,"id":"aaaa1111","timestamp":"2026-11-14T00:00:00Z"}`),
		line("subagents", `{"type":"session","version":3,"id":"cccc3333","parentSession":"/x/2026_aaaa1111.jsonl","title":"Explore","timestamp":"2026-11-14T00:00:05Z"}`),
	})
	if st := m.State(); st["conversation"] != "aaaa1111" {
		t.Errorf("conversation = %q, want the main one", st["conversation"])
	}
	agents := 0
	for _, it := range out.Items {
		if it.Kind == harness.KindAgent {
			agents++
		}
	}
	if agents != 1 {
		t.Errorf("agent rows = %d, want 1", agents)
	}
}

// Seen live on omp 18.20: /clear wrote reset_boundary and the chat gave no
// sign that the agent had dropped the messages above.
func TestClearLeavesANotice(t *testing.T) {
	m := newSession(env())
	out := run(m, []harness.Record{
		line("session", `{"type":"session","version":3,"id":"aaaa1111","timestamp":"2026-11-14T00:00:00Z"}`),
		line("session", `{"type":"message","id":"u1","timestamp":"2026-11-14T00:00:01Z","message":{"role":"user","content":"first"}}`),
		line("session", `{"type":"reset_boundary","id":"b0d498ce","parentId":"u1","timestamp":"2026-11-14T00:00:09Z"}`),
	})
	var notice *harness.NoticeBody
	for _, it := range out.Items {
		if it.Notice != nil {
			notice = it.Notice
		}
	}
	if notice == nil || !strings.Contains(notice.Text, "Context cleared") {
		t.Fatalf("notice = %+v, want one that says the context was cleared", notice)
	}
	if st := m.State(); st["working"] == "true" {
		t.Error("the turn still runs after /clear")
	}
}
