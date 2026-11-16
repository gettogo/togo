package harness

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQuestionBodyNeverWritesANullList(t *testing.T) {
	b, err := json.Marshal(Item{ID: "q", Kind: KindQuestion, Question: &QuestionBody{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"questions":[]`) {
		t.Fatalf("wrote %s, want an empty list", b)
	}
	var back Item
	if err := json.Unmarshal(b, &back); err != nil || back.Question == nil {
		t.Fatalf("read back %+v %v", back, err)
	}
}
