package surface

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPickerQueryRequiresTheExactOriginalQuery(t *testing.T) {
	for _, tc := range []struct {
		name, kind    string
		before, query any
		valid         bool
	}{
		{"literal", "picker", "old", "<div> 100%! /resume 🧑🏽‍💻", true},
		{"stale", "picker", "other", "new", false},
		{"another field", "input", "old", "new", false},
		{"null before", "picker", nil, "new", false},
		{"null query", "picker", "old", nil, false},
		{"oversized", "picker", "old", strings.Repeat("x", 4097), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"before": tc.before, "query": tc.query})
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			err := validatePickerQuery(fields, &Node{Kind: tc.kind, Props: map[string]any{"query": "old"}})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}
