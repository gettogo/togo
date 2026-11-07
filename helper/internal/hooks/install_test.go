package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type env struct {
	o Options
}

func newEnv(t *testing.T) env {
	t.Helper()
	root := t.TempDir()
	o := Options{
		TogoHome:  filepath.Join(root, "togo"),
		Binary:    "/opt/togo/bin/togo",
		ClaudeDir: filepath.Join(root, "claude"),
		CodexDir:  filepath.Join(root, "codex"),
		Cwd:       filepath.Join(root, "project"),
	}
	for _, d := range []string{o.TogoHome, o.ClaudeDir, o.CodexDir, o.Cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return env{o}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustOK(t *testing.T, rs []Result) {
	t.Helper()
	for _, r := range rs {
		if r.Err != nil {
			t.Fatalf("%s: %v", r.Agent, r.Err)
		}
	}
}

func settingsPath(e env) string { return filepath.Join(e.o.ClaudeDir, "settings.json") }

func TestInstallAddsEntriesPreservesOthers(t *testing.T) {
	e := newEnv(t)
	write(t, settingsPath(e), fixture(t, "settings.json"))
	mustOK(t, Install(e.o, []string{Claude}))

	var doc map[string]any
	if err := json.Unmarshal([]byte(read(t, settingsPath(e))), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["model"] != "opus" || doc["permissions"] == nil {
		t.Errorf("unknown keys lost: %v", doc)
	}
	got := read(t, settingsPath(e))
	for _, want := range []string{"notify-me", "audit.sh", "'/opt/togo/bin/togo' hook claude Stop", "hook claude SessionStart", "hook claude Notification", "hook claude statusline"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if !strings.Contains(got, `"matcher": "AskUserQuestion"`) || strings.Count(got, "hook claude PermissionRequest") != 1 {
		t.Error("PermissionRequest is not hooked for AskUserQuestion alone without --block")
	}
	if idx := strings.Index(got, `"model"`); idx > strings.Index(got, `"hooks"`) {
		t.Error("key order changed")
	}
	if Status(e.o)[Claude] != StatusInstalled {
		t.Errorf("status = %v", Status(e.o))
	}
}

func TestBlockAddsPermissionRequestAndDropsItWithout(t *testing.T) {
	e := newEnv(t)
	e.o.Block = true
	mustOK(t, Install(e.o, []string{Claude}))
	if !strings.Contains(read(t, settingsPath(e)), "hook claude PermissionRequest") {
		t.Fatal("PermissionRequest missing with --block")
	}
	if strings.Contains(read(t, settingsPath(e)), "AskUserQuestion") {
		t.Fatal("--block kept the AskUserQuestion matcher")
	}
	e.o.Block = false
	mustOK(t, Install(e.o, []string{Claude}))
	got := read(t, settingsPath(e))
	if strings.Count(got, "hook claude PermissionRequest") != 1 || !strings.Contains(got, `"matcher": "AskUserQuestion"`) {
		t.Errorf("reinstall without --block did not narrow PermissionRequest to AskUserQuestion:\n%s", got)
	}
}

func TestInstallTwiceNoOp(t *testing.T) {
	e := newEnv(t)
	write(t, settingsPath(e), fixture(t, "settings.json"))
	write(t, filepath.Join(e.o.CodexDir, "config.toml"), fixture(t, "config.toml"))
	mustOK(t, Install(e.o, claudeCodex))
	first := read(t, settingsPath(e))
	firstToml := read(t, filepath.Join(e.o.CodexDir, "config.toml"))
	firstHooks := read(t, filepath.Join(e.o.CodexDir, "hooks.json"))
	rs := Install(e.o, claudeCodex)
	mustOK(t, rs)
	for _, r := range rs {
		if r.State != "unchanged" {
			t.Errorf("%s second install state = %s", r.Agent, r.State)
		}
	}
	if read(t, settingsPath(e)) != first || read(t, filepath.Join(e.o.CodexDir, "config.toml")) != firstToml ||
		read(t, filepath.Join(e.o.CodexDir, "hooks.json")) != firstHooks {
		t.Error("second install changed a file")
	}
}

func TestMovedBinaryUpdatesCommandInPlace(t *testing.T) {
	e := newEnv(t)
	mustOK(t, Install(e.o, []string{Claude}))
	e.o.Binary = "/new/togo"
	mustOK(t, Install(e.o, []string{Claude}))
	got := read(t, settingsPath(e))
	if strings.Contains(got, "/opt/togo") || strings.Count(got, "hook claude Stop\"") != 1 {
		t.Errorf("stale or duplicated entry:\n%s", got)
	}
}

func TestUninstallRestoresExactBytes(t *testing.T) {
	e := newEnv(t)
	orig := fixture(t, "settings.json")
	write(t, settingsPath(e), orig)
	tomlPath := filepath.Join(e.o.CodexDir, "config.toml")
	write(t, tomlPath, fixture(t, "config.toml"))
	mustOK(t, Install(e.o, claudeCodex))
	if _, err := os.Stat(settingsPath(e) + backupSuffix); err != nil {
		t.Errorf("no backup copy: %v", err)
	}
	e.o.Block = true
	mustOK(t, Install(e.o, claudeCodex))
	mustOK(t, Uninstall(e.o, claudeCodex))
	if read(t, settingsPath(e)) != string(orig) {
		t.Errorf("settings not restored:\n%s", read(t, settingsPath(e)))
	}
	if read(t, tomlPath) != string(fixture(t, "config.toml")) {
		t.Errorf("config.toml not restored:\n%s", read(t, tomlPath))
	}
	if _, err := os.Stat(filepath.Join(e.o.CodexDir, "hooks.json")); !os.IsNotExist(err) {
		t.Error("hooks.json created by togo was left behind")
	}
	if st := Status(e.o); st[Claude] != StatusAbsent || st[Codex] != StatusAbsent {
		t.Errorf("status after uninstall = %v", st)
	}
}

func TestUninstallAfterUserEditRemovesOnlyOurs(t *testing.T) {
	e := newEnv(t)
	write(t, settingsPath(e), fixture(t, "settings.json"))
	mustOK(t, Install(e.o, []string{Claude}))
	edited := strings.Replace(read(t, settingsPath(e)), `"model": "opus"`, `"model": "sonnet"`, 1)
	write(t, settingsPath(e), []byte(edited))
	mustOK(t, Uninstall(e.o, []string{Claude}))
	got := read(t, settingsPath(e))
	if !strings.Contains(got, `"sonnet"`) || !strings.Contains(got, "notify-me") || !strings.Contains(got, "audit.sh") {
		t.Errorf("user content lost:\n%s", got)
	}
	if strings.Contains(got, "togo") {
		t.Errorf("togo entries remain:\n%s", got)
	}
	if strings.Contains(got, "statusLine") {
		t.Error("togo statusLine remains")
	}
}

func TestSkipsAgentWithoutConfigRoot(t *testing.T) {
	e := newEnv(t)
	e.o.ClaudeDir = filepath.Join(t.TempDir(), "missing")
	rs := Install(e.o, []string{Claude})
	mustOK(t, rs)
	if rs[0].State != "skipped" {
		t.Errorf("state = %s", rs[0].State)
	}
	if _, err := os.Stat(e.o.ClaudeDir); !os.IsNotExist(err) {
		t.Error("config root was created")
	}
	if Status(e.o)[Claude] != StatusNoAgent {
		t.Errorf("status = %v", Status(e.o))
	}
}

func TestUsersStatusLineIsChainedNotReplaced(t *testing.T) {
	e := newEnv(t)
	write(t, settingsPath(e), []byte(`{"statusLine":{"type":"command","command":"my-line.sh"}}`))
	mustOK(t, Install(e.o, []string{Claude}))
	got := read(t, settingsPath(e))
	if !strings.Contains(got, "hook claude statusline --then 'my-line.sh'") {
		t.Errorf("user statusLine not chained:\n%s", got)
	}
	mustOK(t, Uninstall(e.o, []string{Claude}))
	if !strings.Contains(read(t, settingsPath(e)), `"my-line.sh"`) {
		t.Error("user statusLine not restored on uninstall")
	}
}

func TestLocalWritesSettingsLocal(t *testing.T) {
	e := newEnv(t)
	e.o.Local = true
	mustOK(t, Install(e.o, []string{Claude}))
	local := filepath.Join(e.o.Cwd, ".claude", "settings.local.json")
	if !strings.Contains(read(t, local), "hook claude Stop") {
		t.Error("local settings missing entries")
	}
	if _, err := os.Stat(settingsPath(e)); !os.IsNotExist(err) {
		t.Error("global settings written in local mode")
	}
}

func TestCodexTomlBlockAddedAndRemoved(t *testing.T) {
	e := newEnv(t)
	toml := filepath.Join(e.o.CodexDir, "config.toml")

	write(t, toml, []byte("model = \"gpt-5\"\n"))
	mustOK(t, Install(e.o, []string{Codex}))
	got := read(t, toml)
	if !strings.Contains(got, "[features]\nhooks = true") || !strings.Contains(got, tomlBegin) {
		t.Errorf("block missing:\n%s", got)
	}
	if strings.Contains(got, "daemon_auto_start") {
		t.Error("daemon_auto_start touched")
	}
	mustOK(t, Uninstall(e.o, []string{Codex}))
	if read(t, toml) != "model = \"gpt-5\"\n" {
		t.Errorf("not removed:\n%s", read(t, toml))
	}

	write(t, toml, fixture(t, "config.toml"))
	mustOK(t, Install(e.o, []string{Codex}))
	got = read(t, toml)
	if !strings.Contains(got, "[features]\nhooks = true # togo\nweb_search = true") {
		t.Errorf("flag not added to existing table:\n%s", got)
	}
}

func TestCodexHooksFlagAlreadyTrueIsKept(t *testing.T) {
	e := newEnv(t)
	toml := filepath.Join(e.o.CodexDir, "config.toml")
	src := "[features]\nhooks = true\n"
	write(t, toml, []byte(src))
	mustOK(t, Install(e.o, []string{Codex}))
	if read(t, toml) != src {
		t.Errorf("config.toml changed:\n%s", read(t, toml))
	}
	mustOK(t, Uninstall(e.o, []string{Codex}))
	if read(t, toml) != src {
		t.Errorf("user's own flag removed:\n%s", read(t, toml))
	}
}

func TestCodexTomlRefusesAmbiguousLayouts(t *testing.T) {
	cases := map[string]string{
		"hooks false":                   "[features]\nhooks = false\n",
		"legacy key false":              "[features]\ncodex_hooks = false\n",
		"quoted header":                 "[\"features\"]\nweb_search = true\n",
		"spaced quoted":                 "[ 'features' ]\nweb_search = true\n",
		"dotted key":                    "features.web_search = true\n",
		"inline table":                  "features = { web_search = true }\n",
		"header in basic string":        "note = \"\"\"\n[features]\nhooks = true\n\"\"\"\n[features]\nweb_search = true\n",
		"header only in literal string": "note = '''\n[features]\n'''\nfeatures = { a = 1 }\n",
		"two plain headers in strings":  "a = \"\"\"\n[features]\n\"\"\"\nb = \"\"\"\n[features]\n\"\"\"\n[features]\nx = 1\n",
		"invalid toml":                  "[features\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			path := filepath.Join(e.o.CodexDir, "config.toml")
			write(t, path, []byte(src))
			rs := Install(e.o, []string{Codex})
			if rs[0].Err == nil {
				t.Fatalf("install succeeded, config.toml now:\n%s", read(t, path))
			}
			if read(t, path) != src {
				t.Errorf("config.toml modified:\n%s", read(t, path))
			}
			if _, err := os.Stat(filepath.Join(e.o.CodexDir, "hooks.json")); !os.IsNotExist(err) {
				t.Error("hooks.json written although config.toml was refused")
			}
		})
	}
}

func TestCodexTomlRoundTripKeepsComplexConfig(t *testing.T) {
	src := "# my config\nmodel = \"gpt-5\"\nnote = \"\"\"\n[features]\nhooks = false\n\"\"\"\nraw = '''\n[features]\n'''\nlist = [\n  1,\n  2,\n]\ninline = { a = 1, b = [\"x\"] }\n\n[projects.\"/tmp/x\"]\ntrust_level = \"trusted\"\n"
	e := newEnv(t)
	path := filepath.Join(e.o.CodexDir, "config.toml")
	write(t, path, []byte(src))
	mustOK(t, Install(e.o, []string{Codex}))
	got := read(t, path)
	if !strings.HasPrefix(got, src) || !strings.Contains(got, "[features]\nhooks = true") {
		t.Errorf("original text not kept as a prefix:\n%s", got)
	}
	write(t, path, []byte("# edited\n"+got))
	mustOK(t, Uninstall(e.o, []string{Codex}))
	if read(t, path) != "# edited\n"+src {
		t.Errorf("uninstall changed user text:\n%s", read(t, path))
	}
}

func TestCodexTomlInsertsUnderPlainHeaderAmongComplexValues(t *testing.T) {
	src := "list = [\n  \"[features]\",\n]\n\n[features] # flags\nweb_search = true\n\n[other]\nk = \"v\"\n"
	e := newEnv(t)
	path := filepath.Join(e.o.CodexDir, "config.toml")
	write(t, path, []byte(src))
	mustOK(t, Install(e.o, []string{Codex}))
	want := strings.Replace(src, "[features] # flags\n", "[features] # flags\nhooks = true # togo\n", 1)
	if read(t, path) != want {
		t.Errorf("got:\n%s", read(t, path))
	}
	mustOK(t, Uninstall(e.o, []string{Codex}))
	if read(t, path) != src {
		t.Errorf("not restored:\n%s", read(t, path))
	}
}

func TestUnusualJSONFormattingKeepsUntouchedText(t *testing.T) {
	e := newEnv(t)
	src := "{\n\t\"model\": \"opus\",\n\t\"permissions\": {\"allow\": [\"Bash(git status)\", \"Read\"], \"deny\": []},\n\t\"env\":   {\"A\":\"1\"}\n}"
	write(t, settingsPath(e), []byte(src))
	mustOK(t, Install(e.o, []string{Claude}))
	got := read(t, settingsPath(e))
	for _, keep := range []string{`"permissions": {"allow": ["Bash(git status)", "Read"], "deny": []}`, `{"A":"1"}`} {
		if !strings.Contains(got, keep) {
			t.Errorf("lost %q in\n%s", keep, got)
		}
	}
	if !strings.Contains(got, "\n\t\"hooks\": {\n\t\t\"SessionStart\"") {
		t.Errorf("tab indentation not used for new keys:\n%s", got)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, got)
	}
	mustOK(t, Uninstall(e.o, []string{Claude}))
	if read(t, settingsPath(e)) != src {
		t.Errorf("not restored:\n%s", read(t, settingsPath(e)))
	}
}

func TestUninstallWithMissingFileDropsItsRecord(t *testing.T) {
	e := newEnv(t)
	mustOK(t, Install(e.o, []string{Claude}))
	if err := os.Remove(settingsPath(e)); err != nil {
		t.Fatal(err)
	}
	mustOK(t, Uninstall(e.o, []string{Claude}))
	if strings.Contains(read(t, filepath.Join(e.o.TogoHome, "hooks.json")), "settings.json") {
		t.Error("record for a missing file kept")
	}
}

func TestParseErrorRefusesAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	bad := []byte(`{"hooks": `)
	write(t, settingsPath(e), bad)
	rs := Install(e.o, []string{Claude})
	if rs[0].Err == nil || rs[0].State != "refused" {
		t.Fatalf("result = %+v", rs[0])
	}
	if read(t, settingsPath(e)) != string(bad) {
		t.Error("file modified")
	}
	if _, err := os.Stat(settingsPath(e) + backupSuffix); !os.IsNotExist(err) {
		t.Error("backup written for a refused file")
	}
}

func TestCommandPathQuotedWithSpaces(t *testing.T) {
	e := newEnv(t)
	e.o.Binary = "/Users/a b/it's/togo"
	mustOK(t, Install(e.o, []string{Claude}))
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string }
		}
	}
	if err := json.Unmarshal([]byte(read(t, settingsPath(e))), &doc); err != nil {
		t.Fatal(err)
	}
	want := `'/Users/a b/it'\''s/togo' hook claude Stop`
	if got := doc.Hooks["Stop"][0].Hooks[0].Command; got != want {
		t.Errorf("command = %q, want %q", got, want)
	}
	if !owned(want, Claude) {
		t.Error("quoted command not recognised as togo's")
	}
}

func TestInstallKeepsFileMode(t *testing.T) {
	e := newEnv(t)
	write(t, settingsPath(e), []byte("{}\n"))
	if err := os.Chmod(settingsPath(e), 0o640); err != nil {
		t.Fatal(err)
	}
	mustOK(t, Install(e.o, []string{Claude}))
	st, err := os.Stat(settingsPath(e))
	if err != nil || st.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, err = %v", st.Mode(), err)
	}
}

func TestInstallAddsSessionEndAndStopFailure(t *testing.T) {
	e := newEnv(t)
	user := `{"hooks":{"SessionEnd":[{"hooks":[{"type":"command","command":"backup.sh"}]}]}}`
	write(t, settingsPath(e), []byte(user))
	mustOK(t, Install(e.o, []string{Claude}))
	first := read(t, settingsPath(e))
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(first), &doc); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{"SessionEnd", "StopFailure"} {
		var cmds []string
		for _, g := range doc.Hooks[ev] {
			for _, h := range g.Hooks {
				cmds = append(cmds, h.Command)
			}
		}
		want := []string{"'/opt/togo/bin/togo' hook claude " + ev}
		if ev == "SessionEnd" {
			want = append([]string{"backup.sh"}, want...)
		}
		if strings.Join(cmds, "|") != strings.Join(want, "|") {
			t.Errorf("%s commands %q, want %q", ev, cmds, want)
		}
	}
	rs := Install(e.o, []string{Claude})
	mustOK(t, rs)
	if rs[0].State != "unchanged" || read(t, settingsPath(e)) != first {
		t.Errorf("second install changed the file (state %s)", rs[0].State)
	}
}

// claudeCodex are the two harnesses newEnv sets up.
var claudeCodex = []string{Claude, Codex}
