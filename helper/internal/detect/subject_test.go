package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

// subjectExpectation reads the subject part of a fixture's .json. Present
// tells a declared "subject": null apart from a missing key.
type subjectExpectation struct {
	Source  string          `json:"source"`
	Match   bool            `json:"match"`
	Subject json.RawMessage `json:"subject"`
}

// TestRealFixturesSubjects checks every capture, real or synthetic: a
// matching screen yields exactly the subject its .json declares (null when
// the prompt names no command, file or URL), and a screen that is not a
// prompt yields neither a match nor a subject. Adding a capture adds a case.
func TestRealFixturesSubjects(t *testing.T) {
	files, err := filepath.Glob("testdata/*/*.txt")
	if err != nil {
		t.Fatal(err)
	}
	real := 0
	for _, f := range files {
		name := strings.TrimSuffix(strings.TrimPrefix(f, "testdata/"), ".txt")
		t.Run(name, func(t *testing.T) {
			screen, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(strings.TrimSuffix(f, ".txt") + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var want subjectExpectation
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			if want.Source == "real" {
				real++
			}
			p, ok := Detect(string(screen))
			if !want.Match {
				if ok || p.Subject != nil {
					t.Fatalf("not a prompt, got match %v subject %+v", ok, p.Subject)
				}
				return
			}
			if want.Subject == nil {
				t.Fatal(`a matching fixture must declare "subject" (null when there is none)`)
			}
			var ws *wire.Subject
			if err := json.Unmarshal(want.Subject, &ws); err != nil {
				t.Fatal(err)
			}
			switch {
			case ws == nil && p.Subject != nil:
				t.Errorf("subject = %+v, want none", *p.Subject)
			case ws != nil && p.Subject == nil:
				t.Errorf("no subject, want %+v", *ws)
			case ws != nil && *p.Subject != *ws:
				t.Errorf("subject = %+v, want %+v", *p.Subject, *ws)
			}
		})
	}
	if real == 0 {
		t.Error("no real captures found")
	}
}

func TestCleanLineTable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "protocol", "fixtures", "render", "clean.json"))
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct{ Name, In, Want string }
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Cases) < 30 {
		t.Fatalf("clean.json has %d cases, want at least 30", len(table.Cases))
	}
	for _, c := range table.Cases {
		if got := CleanLine(c.In); got != c.Want {
			t.Errorf("%s: CleanLine(%q) = %q, want %q", c.Name, c.In, got, c.Want)
		}
	}
}

// hasBorderOrCursor reports box drawing or block elements anywhere in s, or
// a menu cursor at the start of a line.
func hasBorderOrCursor(s string) bool {
	if strings.ContainsFunc(s, func(r rune) bool { return r >= 0x2500 && r <= 0x259F }) {
		return true
	}
	for line := range strings.SplitSeq(s, "\n") {
		if strings.ContainsAny(firstRune(strings.TrimSpace(line)), "❯›▸>") {
			return true
		}
	}
	return false
}

func firstRune(s string) string {
	_, n := utf8.DecodeRuneInString(s)
	return s[:n]
}

func TestNoBorderCharsInTitleOrSubject(t *testing.T) {
	files, err := filepath.Glob("testdata/*/*.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		screen, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		p, ok := Detect(string(screen))
		if !ok {
			continue
		}
		if hasBorderOrCursor(p.Title) {
			t.Errorf("%s: title %q carries border or cursor", f, p.Title)
		}
		if p.Subject != nil {
			if hasBorderOrCursor(p.Subject.Text) {
				t.Errorf("%s: subject %q carries border or cursor", f, p.Subject.Text)
			}
			if err := p.Subject.Validate(); err != nil {
				t.Errorf("%s: subject invalid: %v", f, err)
			}
		}
		for _, o := range p.Options {
			if hasBorderOrCursor(o.Label) {
				t.Errorf("%s: option %q carries border or cursor", f, o.Label)
			}
		}
	}
}

func TestSubjectFromScreenShapes(t *testing.T) {
	cases := []struct {
		name, screen string
		want         *wire.Subject
	}{
		{
			"url under a fetch label",
			"╭────────────────────────────╮\n│ Fetch                      │\n│   https://example.com/docs │\n│ Do you want to allow this? │\n│ ❯ 1. Yes                   │\n│   2. No                    │\n╰────────────────────────────╯\n",
			&wire.Subject{Kind: wire.SubjectURL, Text: "https://example.com/docs"},
		},
		{
			"non-ASCII path in a write label",
			"│ Write file             │\n│   ~/Проекты/app/заметки.md │\n│ Do you want to create заметки.md? │\n│ ❯ 1. Yes │\n│   2. No │\n",
			&wire.Subject{Kind: wire.SubjectFile, Text: "~/Проекты/app/заметки.md"},
		},
		{
			"column-aligned command keeps single spaces",
			"Would you like to run the following command?\n\n$ git    log   --oneline\n\n› 1. Yes, proceed (y)\n  2. No (esc)\n",
			&wire.Subject{Kind: wire.SubjectCommand, Text: "git log --oneline"},
		},
		{
			"question with no subject",
			"Which runner should I use?\n❯ 1. vitest\n  2. jest\n",
			nil,
		},
		{
			"agent output above an unboxed question is not a subject when far away",
			"$ make deploy\n" + strings.Repeat("building step\n", maxAbove+2) + "Proceed with the release?\n❯ 1. Yes\n  2. No\n",
			nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := mustDetect(t, c.screen)
			switch {
			case c.want == nil && p.Subject != nil:
				t.Errorf("subject = %+v, want none", *p.Subject)
			case c.want != nil && (p.Subject == nil || *p.Subject != *c.want):
				t.Errorf("subject = %+v, want %+v", p.Subject, *c.want)
			}
		})
	}
}

func TestSubjectKindForTool(t *testing.T) {
	cases := map[string]string{
		"Bash": wire.SubjectCommand, "exec_command": wire.SubjectCommand, "shell": wire.SubjectCommand,
		"Edit": wire.SubjectFile, "Write": wire.SubjectFile, "MultiEdit": wire.SubjectFile,
		"apply_patch": wire.SubjectFile, "NotebookEdit": wire.SubjectFile,
		"WebFetch": wire.SubjectURL,
		"Task":     wire.SubjectText, "mcp__github__create_issue": wire.SubjectText, "": wire.SubjectText,
	}
	for tool, want := range cases {
		if got := SubjectKindForTool(tool); got != want {
			t.Errorf("SubjectKindForTool(%q) = %s, want %s", tool, got, want)
		}
	}
}

func TestNewSubjectAlwaysValid(t *testing.T) {
	if s := NewSubject(wire.SubjectCommand, " │ ❯ ‮\x07 │ "); s != nil {
		t.Errorf("subject from nothing readable = %+v", *s)
	}
	if s := NewSubject("weird", "ls"); s != nil {
		t.Errorf("subject with an unknown kind = %+v", *s)
	}
	long := NewSubject(wire.SubjectCommand, strings.Repeat("é", 3000))
	if long == nil || utf8.RuneCountInString(long.Text) != wire.MaxSubjectText {
		t.Fatalf("long subject = %v", long)
	}
	for _, in := range []string{"rm -rf ~/x‮gnp.txt", "a\x00b\r\nc\x85d", "\x1b[31mls\x1b[0m", "x\xffy"} {
		s := NewSubject(wire.SubjectCommand, in)
		if s == nil {
			t.Fatalf("no subject for %q", in)
		}
		if err := s.Validate(); err != nil {
			t.Errorf("NewSubject(%q) = %q: %v", in, s.Text, err)
		}
	}
}

func FuzzNewSubjectValid(f *testing.F) {
	f.Add("ls", "│ ❯ npm test │")
	f.Add("x", "⁦a⁩\n\n\nb\nc\nd")
	f.Fuzz(func(t *testing.T, tool, text string) {
		s := NewSubject(SubjectKindForTool(tool), text)
		if s == nil {
			return
		}
		if err := s.Validate(); err != nil {
			t.Fatalf("NewSubject(%q) = %q: %v", text, s.Text, err)
		}
	})
}
