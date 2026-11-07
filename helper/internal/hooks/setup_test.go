package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// allEnv is newEnv with every hook harness's config root.
func allEnv(t *testing.T) env {
	t.Helper()
	e := optionalEnv(t)
	root := filepath.Dir(e.o.ClaudeDir)
	e.o.GeminiDir, e.o.AugmentDir = filepath.Join(root, "gemini"), filepath.Join(root, "augment")
	for _, d := range []string{e.o.GeminiDir, e.o.AugmentDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func configOf(t *testing.T, e env, agent string) string {
	t.Helper()
	h, _ := Lookup(agent)
	return h.Config(e.o)
}

// For every harness: install over the user's own file, a second install
// writes nothing, the backup is the original, uninstall restores it byte
// for byte.
func TestEveryHarnessInstallIdempotentBackupRestore(t *testing.T) {
	e := allEnv(t)
	origs := map[string]string{}
	for _, a := range Agents {
		p := configOf(t, e, a)
		if a == Copilot {
			continue // Togo's own file; nothing of the user's to keep
		}
		orig := "{\n  \"mine\": true,\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"say done\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"
		write(t, p, []byte(orig))
		origs[p] = orig
	}
	for _, r := range Install(e.o, Agents) {
		if r.Err != nil || r.State != "installed" {
			t.Fatalf("%s: %s %v", r.Agent, r.State, r.Err)
		}
		if _, ok := origs[r.Path]; ok && r.Backup != r.Path+backupSuffix {
			t.Errorf("%s backup %q", r.Agent, r.Backup)
		}
	}
	after := map[string]string{}
	for _, a := range Agents {
		p := configOf(t, e, a)
		after[p] = read(t, p)
		if orig, ok := origs[p]; ok {
			if read(t, p+backupSuffix) != orig {
				t.Errorf("%s backup is not the original", a)
			}
			if !strings.Contains(after[p], "say done") || !strings.Contains(after[p], `"mine": true`) {
				t.Errorf("%s lost the user's entries: %s", a, after[p])
			}
		}
		if st := Status(e.o)[a]; st != StatusInstalled {
			t.Errorf("%s status %s", a, st)
		}
	}
	for _, r := range Install(e.o, Agents) {
		if r.Err != nil || r.State != "unchanged" {
			t.Errorf("%s second install: %s %v", r.Agent, r.State, r.Err)
		}
	}
	for p, b := range after {
		if read(t, p) != b {
			t.Errorf("second install rewrote %s", p)
		}
	}
	mustOK(t, Uninstall(e.o, Agents))
	for p, orig := range origs {
		if read(t, p) != orig {
			t.Errorf("%s not restored:\n%s", p, read(t, p))
		}
		if _, err := os.Stat(p + backupSuffix); err == nil {
			t.Errorf("%s backup left after an exact restore", p)
		}
	}
	if _, err := os.Stat(configOf(t, e, Copilot)); err == nil {
		t.Error("copilot's togo.json left behind")
	}
}

func TestGeminiEntries(t *testing.T) {
	e := allEnv(t)
	mustOK(t, Install(e.o, []string{Gemini}))
	got := read(t, configOf(t, e, Gemini))
	for _, ev := range geminiEvents {
		if !strings.Contains(got, "'/opt/togo/bin/togo' hook gemini "+ev) {
			t.Errorf("no %s in %s", ev, got)
		}
	}
	if strings.Contains(got, "BeforeTool") {
		t.Errorf("tool events hooked: %s", got)
	}
}

func TestAuggieEntriesRunWithoutShell(t *testing.T) {
	e := allEnv(t)
	mustOK(t, Install(e.o, []string{Auggie}))
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type, Command string
				Args          []string
			} `json:"hooks"`
			Metadata map[string]bool `json:"metadata"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(read(t, configOf(t, e, Auggie))), &doc); err != nil {
		t.Fatal(err)
	}
	for _, ev := range auggieEvents {
		g := doc.Hooks[ev]
		if len(g) != 1 || g[0].Hooks[0].Command != "/opt/togo/bin/togo" || strings.Join(g[0].Hooks[0].Args, " ") != "hook auggie "+ev ||
			!g[0].Metadata["includeConversationData"] {
			t.Errorf("%s: %+v", ev, g)
		}
	}
	// A new binary path is an update, not an edit.
	e.o.Binary = "/usr/local/bin/togo"
	if rs := Install(e.o, []string{Auggie}); rs[0].State != "installed" || rs[0].Err != nil {
		t.Fatalf("update: %+v", rs[0])
	}
	if got := read(t, configOf(t, e, Auggie)); strings.Contains(got, "/opt/togo") || strings.Count(got, "/usr/local/bin/togo") != len(auggieEvents) {
		t.Fatalf("update left %s", got)
	}
}

func TestAuggieRefusesEditedEntry(t *testing.T) {
	e := allEnv(t)
	p := configOf(t, e, Auggie)
	edited := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/opt/togo/bin/togo","args":["hook","auggie","Stop","--verbose"]}]}]}}`
	write(t, p, []byte(edited))
	rs := Install(e.o, []string{Auggie})
	if rs[0].State != "refused" || !errors.Is(rs[0].Err, ErrEdited) {
		t.Fatalf("%+v", rs[0])
	}
	if read(t, p) != edited {
		t.Fatal("edited file rewritten")
	}
}

// The backup is the file before Togo's first change; a later install over
// the user's own edits keeps it, and uninstall then removes Togo's entries
// and leaves the user's edits.
func TestBackupWrittenOnceAndUserEditsSurvive(t *testing.T) {
	e := allEnv(t)
	p := configOf(t, e, Gemini)
	orig := "{\n  \"theme\": \"dark\"\n}\n"
	write(t, p, []byte(orig))
	mustOK(t, Install(e.o, []string{Gemini}))
	var doc map[string]any
	if err := json.Unmarshal([]byte(read(t, p)), &doc); err != nil {
		t.Fatal(err)
	}
	doc["theme"] = "light"
	b, _ := json.MarshalIndent(doc, "", "  ")
	write(t, p, b)
	e.o.Binary = "/usr/local/bin/togo"
	mustOK(t, Install(e.o, []string{Gemini}))
	if read(t, p+backupSuffix) != orig {
		t.Fatal("a later install overwrote the first backup")
	}
	mustOK(t, Uninstall(e.o, []string{Gemini}))
	got := read(t, p)
	if !strings.Contains(got, `"light"`) || strings.Contains(got, "hook gemini") {
		t.Fatalf("uninstall after a user edit left %s", got)
	}
}

func TestReportsStates(t *testing.T) {
	e := allEnv(t)
	e.o.HasBinary = func(c string) bool { return c != "aider" }
	write(t, settingsPath(e), []byte(`{"statusLine":{"type":"command","command":"bash line.sh"}}`))
	mustOK(t, Install(e.o, []string{Codex}))
	seen := Seen{Codex: time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)}
	byID := map[string]Report{}
	for _, r := range Reports(e.o, seen) {
		byID[r.ID] = r
	}
	if r := byID[Claude]; r.State != StateNeedsSetup || r.StatusLine != LineOwn || r.Fix != "togo hooks install --agent claude" {
		t.Errorf("claude before %+v", r)
	}
	if r := byID[Codex]; r.State != StateReady || r.LastHookAt != "2026-11-07T09:00:00Z" || r.Fix != "" {
		t.Errorf("codex %+v", r)
	}
	if r := byID["kimi"]; r.State != StateNotNeeded || !r.Installed {
		t.Errorf("kimi %+v", r)
	}
	if r := byID["aider"]; r.State != StateNotInstalled {
		t.Errorf("aider %+v", r)
	}
	if r := byID["vibe"]; r.State != StateNotInstalled {
		t.Errorf("vibe without ~/.vibe %+v", r)
	}
	mustOK(t, Install(e.o, []string{Claude}))
	for _, r := range Reports(e.o, nil) {
		if r.ID == Claude && (r.State != StateReady || r.StatusLine != LineChained || r.Backup == "") {
			t.Errorf("claude after %+v", r)
		}
	}
}

func TestSeenRoundTrip(t *testing.T) {
	home := t.TempDir()
	s := Seen{Claude: time.Date(2026, 11, 7, 1, 2, 3, 0, time.UTC)}
	if err := s.Save(home); err != nil {
		t.Fatal(err)
	}
	if got := LoadSeen(home); !got[Claude].Equal(s[Claude]) {
		t.Fatalf("%v", got)
	}
	if len(LoadSeen(filepath.Join(home, "none"))) != 0 {
		t.Fatal("missing file is not empty")
	}
}

func TestPlainCommand(t *testing.T) {
	for cmd, want := range map[string]bool{
		"'/opt/togo/bin/togo' hook gemini Stop":    true,
		"/Users/a/.togo/bin/togo hook gemini Stop": true,
		"'/a b/it'\\''s/togo' hook gemini Stop":    true,
		"FOO=1 togo hook gemini Stop":              false,
		"'/opt/togo/bin/togo' hook gemini Stop  ":  false,
		"'/opt/togo/bin/other' hook gemini Stop":   false,
		"'/opt/togo/bin/togo' -v hook gemini Stop": false,
		"'/opt/togo/bin/togo' hook claude Stop":    false,
	} {
		if got := plainCommand(cmd, Gemini); got != want {
			t.Errorf("plainCommand(%q) = %v", cmd, got)
		}
	}
}
