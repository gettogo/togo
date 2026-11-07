package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

type expectation struct {
	Source       string              `json:"source"`
	AgentVersion *string             `json:"agentVersion"`
	Match        bool                `json:"match"`
	Kind         string              `json:"kind"`
	Title        string              `json:"title"`
	Options      []wire.Option       `json:"options"`
	Keys         map[string][]string `json:"keys"`
}

func TestFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*/*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 15 {
		t.Fatalf("found %d fixtures, want at least 15", len(files))
	}
	for _, f := range files {
		t.Run(strings.TrimSuffix(strings.TrimPrefix(f, "testdata/"), ".txt"), func(t *testing.T) {
			screen, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(strings.TrimSuffix(f, ".txt") + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var want expectation
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			if want.Source != "real" && want.Source != "synthetic" {
				t.Fatalf("source = %q, want real or synthetic", want.Source)
			}
			p, ok := Detect(string(screen))
			if ok != want.Match {
				t.Fatalf("match = %v, want %v (got %+v)", ok, want.Match, p)
			}
			if !ok {
				return
			}
			if p.Kind != want.Kind || p.Title != want.Title {
				t.Errorf("kind, title = %q, %q; want %q, %q", p.Kind, p.Title, want.Kind, want.Title)
			}
			if !reflect.DeepEqual(p.Options, want.Options) {
				t.Errorf("options = %+v\nwant %+v", p.Options, want.Options)
			}
			keys := map[string][]string{}
			for n, k := range p.Keys {
				keys[strconv.Itoa(n)] = k
			}
			if !reflect.DeepEqual(keys, want.Keys) {
				t.Errorf("keys = %v, want %v", keys, want.Keys)
			}
			if p.Fingerprint == "" || p.Snippet == "" {
				t.Errorf("fingerprint %q or snippet empty", p.Fingerprint)
			}
		})
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustDetect(t *testing.T, screen string) Prompt {
	t.Helper()
	p, ok := Detect(screen)
	if !ok {
		t.Fatalf("no prompt detected in:\n%s", screen)
	}
	return p
}

func TestFingerprintIgnoresCursorMove(t *testing.T) {
	a := mustDetect(t, fixture(t, "opencode/edit-permission"))
	moved := strings.Replace(fixture(t, "opencode/edit-permission"), "  ┃   Allow once\n  ┃ > Allow always",
		"  ┃ > Allow once\n  ┃   Allow always", 1)
	b := mustDetect(t, "agent: reading files\n"+moved)
	if a.Keys[1][0] == b.Keys[1][0] {
		t.Fatalf("cursor move not applied: keys %v and %v", a.Keys, b.Keys)
	}
	if a.Fingerprint != b.Fingerprint {
		t.Errorf("fingerprint changed on cursor move: %s vs %s", a.Fingerprint, b.Fingerprint)
	}

	c := mustDetect(t, fixture(t, "claude/bash-permission"))
	d := mustDetect(t, strings.Replace(strings.Replace(fixture(t, "claude/bash-permission"),
		"│ ❯ 1. Yes ", "│   1. Yes ", 1), "│   2. Yes,", "│ ❯ 2. Yes,", 1))
	if c.Fingerprint != d.Fingerprint {
		t.Errorf("numbered fingerprint changed on cursor move")
	}
}

func TestFingerprintChangesWithOptions(t *testing.T) {
	a := mustDetect(t, fixture(t, "claude/bash-permission"))
	b := mustDetect(t, strings.Replace(fixture(t, "claude/bash-permission"),
		"make commands in ~/src/app", "rm commands in ~/src/app  ", 1))
	if a.Fingerprint == b.Fingerprint {
		t.Error("fingerprint unchanged when an option label changed")
	}
	c := mustDetect(t, strings.Replace(fixture(t, "claude/bash-permission"),
		"Do you want to proceed?", "Do you want to continue?", 1))
	if a.Fingerprint == c.Fingerprint {
		t.Error("fingerprint unchanged when the title changed")
	}
}

func TestNoMatchOnDiffLines(t *testing.T) {
	screens := []string{
		fixture(t, "codex/negative-diff"),
		"@@ -1,3 +1,4 @@\n Do you want to proceed?\n-❯ 1. Yes\n+❯ 1. Yes, proceed\n+  2. No\n",
		"diff --git a/p.md b/p.md\n+Do you want to proceed?\n+❯ 1. Yes\n+  2. No\n+Overwrite? [y/n]\n index 3f2a..9c1b\n",
	}
	for _, s := range screens {
		if p, ok := Detect(s); ok {
			t.Errorf("matched diff lines as %+v:\n%s", p, s)
		}
	}
}

func TestOnlyBottomOfScreenConsidered(t *testing.T) {
	prompt := fixture(t, "claude/bash-permission")
	var out strings.Builder
	for i := 0; i < windowLines; i++ {
		out.WriteString("  compiling module " + strconv.Itoa(i) + "\n\n")
	}
	if _, ok := Detect(prompt + out.String()); ok {
		t.Error("matched a prompt scrolled above the inspected window")
	}
	if _, ok := Detect(out.String() + prompt + "\n\n\n"); !ok {
		t.Error("missed a prompt at the bottom below lots of output")
	}
	if _, ok := Detect(prompt + "line one\nline two\nline three\nline four\nline five\n"); ok {
		t.Error("matched a menu followed by more output than a footer")
	}
}

func TestWrappedOptionLabelJoined(t *testing.T) {
	screen := "Do you want to proceed?\n" +
		"❯ 1. Yes\n" +
		"  2. Yes, and don't ask again for npm run build commands in\n" +
		"     /Users/dev/projects/app\n" +
		"  3. No, and tell the agent what to do differently (esc)\n"
	p := mustDetect(t, screen)
	want := "Yes, and don't ask again for npm run build commands in /Users/dev/projects/app"
	if len(p.Options) != 3 || p.Options[1].Label != want || p.Options[1].Role != wire.RoleApproveAlways {
		t.Errorf("options = %+v", p.Options)
	}
}

func TestDetailCarriesTheCommand(t *testing.T) {
	p := mustDetect(t, fixture(t, "claude/bash-permission"))
	want := "Bash command\nrm -rf ./build && make\nClean and rebuild the project"
	if p.Detail == nil || *p.Detail != want {
		t.Errorf("detail = %v, want %q", p.Detail, want)
	}
	c := mustDetect(t, fixture(t, "codex/exec-approval"))
	if c.Detail == nil || *c.Detail != "$ cargo test --workspace" {
		t.Errorf("detail = %v, want the command", c.Detail)
	}
}

func TestLimitsApplied(t *testing.T) {
	long := strings.Repeat("x", 300)
	screen := "Allow " + long + "?\n❯ 1. Yes " + long + "\n  2. No\n"
	p := mustDetect(t, screen)
	if n := len([]rune(p.Title)); n > wire.MaxTitle {
		t.Errorf("title is %d runes", n)
	}
	if n := len([]rune(p.Options[0].Label)); n > wire.MaxOptionLabel {
		t.Errorf("label is %d runes", n)
	}
	big := strings.Repeat(strings.Repeat("y", 200)+"\n", 60) + screen
	p = mustDetect(t, big)
	if len(p.Snippet) > wire.MaxSnippetBytes || strings.Count(p.Snippet, "\n")+1 > wire.MaxSnippetLines {
		t.Errorf("snippet is %d bytes, %d lines", len(p.Snippet), strings.Count(p.Snippet, "\n")+1)
	}
}

func TestEscapeSequencesIgnored(t *testing.T) {
	screen := "\x1b[1mDo you want to proceed?\x1b[0m\n\x1b[36m❯ 1. Yes\x1b[0m\n  2. No\n"
	p := mustDetect(t, screen)
	if p.Title != "Do you want to proceed?" || p.Options[0].Label != "Yes" {
		t.Errorf("got %+v", p)
	}
}

func FuzzDetectNeverPanics(f *testing.F) {
	files, _ := filepath.Glob("testdata/*/*.txt")
	for _, name := range files {
		b, err := os.ReadFile(name)
		if err == nil {
			f.Add(string(b))
		}
	}
	f.Add("❯ 1.\n")
	f.Add("> \n  \n")
	f.Add("[y/n]")
	f.Add("\xff\xfe❯ 1. Yes\n  2. No\x1b[")
	f.Fuzz(func(t *testing.T, s string) {
		p, ok := Detect(s)
		if !ok {
			return
		}
		if len(p.Options) < 2 || len(p.Keys) != len(p.Options) || p.Fingerprint == "" {
			t.Fatalf("inconsistent prompt %+v", p)
		}
	})
}

func TestFingerprintCoversDetail(t *testing.T) {
	a := mustDetect(t, fixture(t, "claude/bash-permission"))
	b := mustDetect(t, strings.Replace(fixture(t, "claude/bash-permission"),
		"rm -rf ./build && make ", "rm -rf ./dist && make  ", 1))
	if a.Fingerprint == b.Fingerprint {
		t.Error("same fingerprint for the same question about a different command")
	}
	c := mustDetect(t, strings.Replace(fixture(t, "claude/bash-permission"),
		"│   rm -rf ./build && make ", "│     rm   -rf ./build && make", 1))
	if a.Fingerprint != c.Fingerprint {
		t.Error("fingerprint changed when only whitespace in the detail changed")
	}
}

func TestKeysForFollowsCursorMovedBetweenCaptures(t *testing.T) {
	first := mustDetect(t, fixture(t, "opencode/bash-permission"))
	if got := first.Keys[3]; !reflect.DeepEqual(got, []string{"Down", "Down", "Enter"}) {
		t.Fatalf("keys at first capture = %v", got)
	}
	moved := strings.Replace(strings.Replace(fixture(t, "opencode/bash-permission"),
		"┃ > Allow once", "┃   Allow once", 1), "┃   Reject", "┃ > Reject", 1)
	keys, ok := KeysFor(moved, first.Fingerprint, "Reject")
	if !ok || !reflect.DeepEqual(keys, []string{"Enter"}) {
		t.Errorf("Reject after cursor moved onto it = %v, %v; want [Enter]", keys, ok)
	}
	keys, ok = KeysFor(moved, first.Fingerprint, "Allow once")
	if !ok || !reflect.DeepEqual(keys, []string{"Up", "Up", "Enter"}) {
		t.Errorf("Allow once after cursor moved down = %v, %v; want [Up Up Enter]", keys, ok)
	}
}

func TestKeysForRefusesWhenPromptChanged(t *testing.T) {
	first := mustDetect(t, fixture(t, "opencode/bash-permission"))
	other := strings.Replace(fixture(t, "opencode/bash-permission"), "vitest", "jest", 1)
	if keys, ok := KeysFor(other, first.Fingerprint, "Allow once"); ok {
		t.Errorf("typed %v into a different prompt", keys)
	}
	if keys, ok := KeysFor("$ ", first.Fingerprint, "Allow once"); ok {
		t.Errorf("typed %v with no prompt on screen", keys)
	}
	if keys, ok := KeysFor(fixture(t, "opencode/bash-permission"), first.Fingerprint, "Allow forever"); ok {
		t.Errorf("typed %v for a label not offered", keys)
	}
}

func TestRoleOfFailsClosed(t *testing.T) {
	cases := map[string]string{
		"Yes":                               wire.RoleApprove,
		"Allow":                             wire.RoleApprove,
		"Approve":                           wire.RoleApprove,
		"Run":                               wire.RoleApprove,
		"Yes, proceed":                      wire.RoleApprove,
		"Allow once":                        wire.RoleApprove,
		"Yes, allow once":                   wire.RoleApprove,
		"Yes, and don't ask again for git":  wire.RoleApproveAlways,
		"Don't ask again":                   wire.RoleApproveAlways,
		"Yes, remember this decision":       wire.RoleApproveAlways,
		"Allow in this project":             wire.RoleApproveAlways,
		"Yes, trust this folder":            wire.RoleApproveAlways,
		"Approve permanently":               wire.RoleApproveAlways,
		"Always allow":                      wire.RoleApproveAlways,
		"Yes, and manually approve edits":   wire.RoleOther,
		"Yes, but only for files under src": wire.RoleOther,
		"Run with elevated privileges":      wire.RoleOther,
		"No, and tell the agent what to do": wire.RoleDeny,
		"Reject":                            wire.RoleDeny,
		"Something else":                    wire.RoleOther,
	}
	for label, want := range cases {
		if got := roleOf(label); got != want {
			t.Errorf("roleOf(%q) = %s, want %s", label, got, want)
		}
	}
}

func TestConfirmTypesLetterThenEnter(t *testing.T) {
	p := mustDetect(t, fixture(t, "generic/yn"))
	if !reflect.DeepEqual(p.Keys[1], []string{"y", "Enter"}) || !reflect.DeepEqual(p.Keys[2], []string{"n", "Enter"}) {
		t.Errorf("confirm keys = %v", p.Keys)
	}
}

func TestOversizedScreenKeepsTail(t *testing.T) {
	screen := strings.Repeat("x", 2*maxScreenBytes) + "\n" + fixture(t, "claude/bash-permission")
	mustDetect(t, screen)
}

func TestRealFixturesForEachAgent(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "omp"} {
		files, err := filepath.Glob(filepath.Join("testdata", agent, "real-*.json"))
		if err != nil {
			t.Fatal(err)
		}
		real := 0
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var e expectation
			if err := json.Unmarshal(raw, &e); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			if e.Source != "real" || e.AgentVersion == nil || *e.AgentVersion == "" {
				t.Errorf("%s: source %q, agentVersion %v; want real with a version", f, e.Source, e.AgentVersion)
				continue
			}
			real++
		}
		if real == 0 {
			t.Errorf("no real captures for %s", agent)
		}
	}
}
