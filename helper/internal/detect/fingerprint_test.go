package detect

import (
	"os"
	"strings"
	"testing"
)

// An agent that asks for every command under one title with the same
// choices must still give each request its own fingerprint: an answer to
// the first would otherwise be typed into the second.
func TestFingerprintTellsCommandsApart(t *testing.T) {
	raw, err := os.ReadFile("testdata/crush/real-permission.txt")
	if err != nil {
		t.Fatal(err)
	}
	first := string(raw)
	const shown = "│   touch conformance-approval.txt"
	if strings.Count(first, shown) != 1 {
		t.Fatal("the fixture no longer shows the command once in its box")
	}
	second := strings.Replace(first, shown, "│   rm -f conformance-approval.txt", 1)
	a, ok := Detect(first)
	b, ok2 := Detect(second)
	if !ok || !ok2 {
		t.Fatalf("detected %v and %v", ok, ok2)
	}
	if b.Subject == nil || b.Subject.Text != "rm -f conformance-approval.txt" {
		t.Fatalf("second subject = %+v", b.Subject)
	}
	if a.Title != b.Title || len(a.Options) != len(b.Options) {
		t.Fatal("the two requests should differ only in their command")
	}
	if a.Fingerprint == b.Fingerprint {
		t.Fatal("two commands share one fingerprint")
	}
}

// The same request drawn again at another width is the same request.
func TestFingerprintIgnoresWhereLinesBreak(t *testing.T) {
	detail := "don't ask again for /Users/me/proj/src"
	wrapped := "don't ask again for /Users/me/pro j/src"
	p := Prompt{Title: "Do you want to proceed?", Detail: &detail}
	q := Prompt{Title: "Do you want\nto proceed?", Detail: &wrapped}
	if fingerprint(p) != fingerprint(q) {
		t.Fatal("a break inside a word changed the fingerprint")
	}
}
