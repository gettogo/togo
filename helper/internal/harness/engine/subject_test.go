package engine

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Every subject that leaves the helper passes the rules the app decodes
// with, whatever the agent ran.
func TestSendableSubjectAlwaysValidates(t *testing.T) {
	for name, text := range map[string]string{
		"tab in a heredoc":  "cat > Makefile <<EOF\nall:\n\tgo build ./...\nEOF",
		"carriage return":   "printf 'a\r\nb'",
		"zero width joiner": "touch fam\u200dily.txt",
		"bidi override":     "cat \u202egnp.exe",
		"escape":            "echo \x1b[31mred",
		"invalid utf8":      "echo \xff\xfe ok",
		"too long":          strings.Repeat("x", wire.MaxSubjectText+500),
		"plain":             "go test ./...",
	} {
		got := sendableSubject(wire.Subject{Kind: wire.SubjectCommand}, text)
		if got == nil {
			t.Errorf("%s: dropped, want a readable subject", name)
			continue
		}
		if err := got.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := sendableSubject(wire.Subject{Kind: wire.SubjectCommand}, "go test ./..."); got.Text != "go test ./..." {
		t.Errorf("plain text changed to %q", got.Text)
	}
	if got := sendableSubject(wire.Subject{Kind: wire.SubjectCommand}, "cat <<EOF\n\tx\nEOF"); got.Text != "cat <<EOF\n x\nEOF" {
		t.Errorf("tab not turned into a space, newlines kept: %q", got.Text)
	}
	for name, text := range map[string]string{"empty": "", "only controls": "\t\r\x00", "only invisible": "\u200b\u200d"} {
		if got := sendableSubject(wire.Subject{Kind: wire.SubjectCommand}, text); got != nil {
			t.Errorf("%s: kept %q, want none", name, got.Text)
		}
	}
	if got := sendableSubject(wire.Subject{Kind: "weird"}, "x"); got != nil {
		t.Error("an unknown kind was sent")
	}
}
