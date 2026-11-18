package opencode

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Seen live on 2.3.0 and 2.2.5: after a helper restart the conversation was
// unchanged and a freshly opened chat was empty. testdata/v2/messages.json
// is the messages route's answer recorded from 2.2.5 for a conversation of
// two prompts, the second answered with reasoning, a shell call and text.
// Each message becomes the items the live stream gave it, under the same
// ids, so a message seen both ways is one item.
func TestPastMessagesBecomeTheSameItemsAsTheStream(t *testing.T) {
	raw, err := os.ReadFile("testdata/v2/messages.json")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	m := NewEvents2(harness.Env{Cwd: "/w"})
	var got []string
	ids := map[string]bool{}
	// The route answers newest first; the adapter's `items` reverses it.
	for i := len(page.Data) - 1; i >= 0; i-- {
		rec, _ := json.Marshal(map[string]any{"type": "session.message", "conversation": "ses_x", "data": page.Data[i]})
		items, _ := m.Map(harness.Record{Source: "events", At: time.Now(), Data: rec})
		for _, it := range items {
			ids[it.ID] = true
			switch {
			case it.User != nil:
				got = append(got, "user:"+it.User.Text)
			case it.Assistant != nil:
				got = append(got, "assistant:"+it.Assistant.Text)
			case it.Thinking != nil:
				got = append(got, "thinking")
			case it.Tool != nil:
				got = append(got, "tool:"+it.Tool.Name+":"+it.Tool.State)
			}
			if it.TurnID == "" {
				t.Errorf("item %s has no turn", it.ID)
			}
		}
	}
	want := []string{
		"user:Reply with exactly FIRST_ONE and nothing else. Do not use tools.",
		"assistant:FIRST_ONE",
		"user:Run the shell command: echo HIST_TOOL . Then reply with exactly SECOND_ONE.",
		"thinking",
		"tool:shell:" + harness.StateOK,
		"assistant:SECOND_ONE",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("items:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The ids the stream gave these parts in the session they were recorded from.
	for _, id := range []string{"msg_1dba662ae001gqqFMvtJNFpnrf-t0", "msg_1dba67d04001QbIPPdasYUlofY-r0", "call_53e2d3e0b90a4770a8f4ac54", "msg_1dba685d0001LzdWIdvVEwMwAZ-t0"} {
		if !ids[id] {
			t.Errorf("no item with the stream's id %s", id)
		}
	}
	if st := m.State(); st["harness_session"] != "ses_x" || st["conversation"] != "ses_x" {
		t.Errorf("the past did not name its conversation: %v", st)
	}
	// A message of another conversation is not this chat's.
	rec, _ := json.Marshal(map[string]any{"type": "session.message", "conversation": "ses_other", "data": page.Data[len(page.Data)-1]})
	if items, _ := m.Map(harness.Record{Source: "events", At: time.Now(), Data: rec}); len(items) != 0 {
		t.Errorf("another conversation's message was shown: %+v", items)
	}
}
