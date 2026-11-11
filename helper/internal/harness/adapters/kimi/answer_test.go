package kimi

import (
	"encoding/json"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A resolution that names none of the questions still closes every one.
func TestQuestionClosesUnmatched(t *testing.T) {
	m := NewWire(harness.Env{SessionID: "s1"})
	items, _ := m.Map(harness.Record{Data: json.RawMessage(`{"type":"interaction.request","time":1,"id":"q1","kind":"question","request":{"questions":[{"question":"A?"},{"question":"B?"}]}}`)})
	if len(items) != 1 || items[0].Question.Answerable != nil || !items[0].Question.Questions[0].AllowText {
		t.Fatalf("asked %+v", items)
	}
	items, _ = m.Map(harness.Record{Data: json.RawMessage(`{"type":"interaction.resolved","time":2,"id":"q1","response":{"answers":{"A?":"yes","C?":"x"}}}`)})
	if len(items) != 1 {
		t.Fatalf("resolved %+v", items)
	}
	got := items[0].Question.Answered
	if len(got) != 2 || len(got["0"]) != 1 || got["0"][0] != "yes" || got["1"] == nil || len(got["1"]) != 0 {
		t.Fatalf("answered %#v", got)
	}
}
