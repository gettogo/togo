package engine

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Text over the limit ends with a line saying it was cut; text under it is
// left alone.
func TestClipItemTextMarksTheCut(t *testing.T) {
	long := strings.Repeat("é", harness.MaxItemText)
	got := clipItemText(long)
	if len(got) > harness.MaxItemText || !strings.HasSuffix(got, textCutMarker) {
		t.Fatalf("len %d, tail %q", len(got), got[len(got)-60:])
	}
	if !strings.HasPrefix(got, "éééé") || !strings.Contains(got, "é\n\n[Text cut") {
		t.Fatal("cut inside a character")
	}
	short := strings.Repeat("a", harness.MaxItemText)
	if clipItemText(short) != short {
		t.Fatal("text at the limit was changed")
	}
}
