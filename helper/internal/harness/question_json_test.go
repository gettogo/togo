package harness

import (
	"encoding/json"
	"strings"
	"testing"
)

// A question without options goes out as an empty list: the app decodes
// the whole frame or none of it, and null is not an array.
func TestQuestionWithoutOptionsMarshalsEmptyList(t *testing.T) {
	b, err := json.Marshal(QuestionBody{Questions: []Question{{ID: "0", Prompt: "Kimi is waiting for you"}}})
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.Contains(s, `"options":[]`) || strings.Contains(s, "null") {
		t.Fatalf("got %s", s)
	}
	var back Question
	if err := json.Unmarshal([]byte(`{"id":"1","prompt":"p","options":[{"id":"a","label":"A"}]}`), &back); err != nil || len(back.Options) != 1 {
		t.Fatalf("round trip: %v %+v", err, back)
	}
}
