package sessiongen

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	aiderFile   = regexp.MustCompile(`[\w./-]+\.(py|go|txt|md)\b`)
	aiderHex    = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	aiderNumber = regexp.MustCompile(`\d+`)
	aiderLaunch = regexp.MustCompile(`^/\S*/aider --\S.*$`)
	aiderHeader = regexp.MustCompile(`^# aider chat started at \d{4}-\d\d-\d\d \d\d:\d\d:\d\d$`)
)

// aiderShape reduces a tool line to its words with file names, hashes and
// numbers as placeholders, so a generated line can be compared with the
// recorded ones.
func aiderShape(l string) string {
	l = strings.TrimSpace(l)
	if aiderLaunch.MatchString(l) {
		return "LAUNCH"
	}
	l = aiderFile.ReplaceAllString(l, "F")
	l = aiderHex.ReplaceAllString(l, "H")
	return aiderNumber.ReplaceAllString(l, "N")
}

func aiderLines(t *testing.T, files []string) []string {
	t.Helper()
	var out []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Split(string(raw), "\n")...)
	}
	return out
}

// The Aider profile writes lines of the shapes in aider's recorded
// histories (adapters/aider/testdata): the header with a local time, prompt
// lines and tool lines ending in two spaces, tool lines whose words are
// in a recording, and replies that end in no two spaces.
func TestAiderMatchesRecordedLines(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	l, err := Write(home, &Aider{Extra: map[int]AiderTurn{
		1: {Files: 2, Edits: 2, Commit: true},
		3: {Unfinished: true},
	}}, Script{Seed: 7, Start: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), Cwd: cwd, Turns: []Turn{
		{Say: 200, Tools: 3, Output: 10}, {Say: 20}, {Say: 20}, {Error: true}, {New: true, Say: 30},
	}})
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.Glob("../adapters/aider/testdata/*.history.md")
	var recorded []string
	for _, f := range real {
		if filepath.Base(f) == "source-limits.history.md" {
			continue // written from aider's source, not recorded
		}
		recorded = append(recorded, f)
	}
	known := map[string]bool{}
	for _, line := range aiderLines(t, recorded) {
		if strings.HasPrefix(line, "> ") && strings.HasSuffix(line, "  ") {
			known[aiderShape(strings.TrimPrefix(line, ">"))] = true
		}
	}
	headers, prompts := 0, 0
	for _, line := range aiderLines(t, l.Mains) {
		switch {
		case line == "":
		case strings.HasPrefix(line, "# aider chat started at"):
			headers++
			if !aiderHeader.MatchString(line) {
				t.Errorf("header %q", line)
			}
		case strings.HasPrefix(line, "####"):
			prompts++
			if !strings.HasSuffix(line, "  ") {
				t.Errorf("prompt line %q lacks the two trailing spaces", line)
			}
		case strings.HasPrefix(line, "> "):
			if !strings.HasSuffix(line, "  ") {
				t.Errorf("tool line %q lacks the two trailing spaces", line)
			}
			if s := aiderShape(strings.TrimPrefix(line, ">")); !known[s] {
				t.Errorf("tool line %q (%q) is in no recording", line, s)
			}
		default:
			if strings.HasSuffix(line, "  ") {
				t.Errorf("reply line %q ends in the two spaces of a prompt or tool line", line)
			}
		}
	}
	if headers != 2 || prompts != 5 {
		t.Errorf("%d headers and %d prompts, want 2 and 5", headers, prompts)
	}
}

func TestAiderRefusesWhatItCannotShow(t *testing.T) {
	home := t.TempDir()
	for _, turn := range []Turn{{Agents: &Batch{Size: 1}}, {Jobs: 1}, {Exit: true}, {Interrupt: true}, {Compact: true}} {
		_, err := Write(home, &Aider{}, Script{Seed: 1, Start: time.Now(), Cwd: filepath.Join(home, "p"), Turns: []Turn{turn}})
		if err == nil {
			t.Errorf("%+v written without a real recording", turn)
		}
	}
}
