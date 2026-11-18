package omp

import (
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// The adapter states how omp's own parser reads a file mention (its
// utils/file-mentions.ts): a bare mention is /[^\s@]+/ with leading
// `"'([{< and trailing )]}>.,;:!?"'` stripped; @"..." and @'...' take a
// whole path. Completion writes mentions from these, so they must be
// exactly the parser's.
func TestTheAdapterStatesOmpsMentionRules(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	want := &spec.Mentions{Quotes: []string{`"`, "'"}, BareStops: "@", BareLeading: "`\"'([{<", BareTrailing: ")]}>.,;:!?\"'`"}
	if !reflect.DeepEqual(a.Mentions, want) {
		t.Fatalf("mentions = %+v, want %+v", a.Mentions, want)
	}
}
