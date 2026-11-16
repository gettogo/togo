package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileLoads(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if a.Feeds["session"].Mapper != MapperSession {
		t.Fatal("mapper")
	}
	var logs []string
	engine.New(engine.Config{Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	for _, l := range logs {
		if strings.Contains(l, " pi") {
			t.Fatal(l)
		}
	}
	if f, ok := harness.LookupMapper(MapperSession); !ok || f(harness.Env{}).Name() != MapperSession {
		t.Fatal("mapper not registered")
	}
}

// The feed glob names Pi's folder for the working directory, spaces kept.
func TestSessionFolder(t *testing.T) {
	a, _ := spec.Parse(AdapterFile())
	got, err := engine.Render(a.Feeds["session"].JSONL.Glob, map[string]any{"cwd": "/Users/me/Dev/Delta RE"})
	if err != nil || got != "~/.pi/agent/sessions/--Users-me-Dev-Delta RE--/*.jsonl" {
		t.Fatalf("%q %v", got, err)
	}
}

// The model list table becomes picker rows.
func TestModelTable(t *testing.T) {
	a, _ := spec.Parse(AdapterFile())
	table := "provider       model                   context  max-out  thinking  images\n" +
		"moonshotai     kimi-k2.6               262.1K   262.1K   yes       yes   \n" +
		"anthropic      claude-sonnet-4-5       200K     64K      yes       yes   \n"
	raw, _ := json.Marshal(table)
	var in any
	json.Unmarshal(raw, &in)
	out, err := engine.JQ(a.Sources["models"].Fallback.Select, in, nil)
	if err != nil || len(out) != 1 {
		t.Fatalf("%v %v", out, err)
	}
	rows := out[0].([]any)
	first := rows[0].(map[string]any)
	if len(rows) != 2 || first["id"] != "moonshotai/kimi-k2.6" || first["thinking"] != true || first["context"] != "262.1K" {
		t.Fatalf("%v", rows)
	}
}

// The Resume picker passes the session's id, from the header record, and
// titles an unnamed session with its first message.
func TestResumeRows(t *testing.T) {
	a, _ := spec.Parse(AdapterFile())
	session, err := os.ReadFile("../pisession/testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var head []any
	for _, line := range strings.Split(strings.TrimSpace(string(session)), "\n")[:8] {
		var rec any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		head = append(head, rec)
	}
	in := []any{map[string]any{"name": "2026-11-06T14-00-00-000Z_7c1e2a90", "head": head, "tail": []any{}, "mtime": "2026-11-06T14:05:00Z"}}
	out, err := engine.JQ(a.Sources["sessions"].Select, in, nil)
	if err != nil || len(out) != 1 {
		t.Fatalf("%v %v", out, err)
	}
	row := out[0].([]any)[0].(map[string]any)
	if row["id"] != "7c1e2a90-3b4d-4c55-9e10-aa11bb22cc33" || row["title"] != "Fix the failing test in parser.go" {
		t.Fatalf("%v", row)
	}
}

// A folder with glob characters in its name still matches its own
// session files.
func TestSessionFolderWithGlobCharacters(t *testing.T) {
	a, _ := spec.Parse(AdapterFile())
	for _, cwd := range []string{"/work/[client] site", "/work/app/[slug]", "/work/what?", "/work/a*b"} {
		root := t.TempDir()
		dir := filepath.Join(root, ".pi/agent/sessions", "-"+strings.ReplaceAll(cwd, "/", "-")+"--")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "2026-11-12T15-57-52-145Z_abc.jsonl")
		if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		pattern, err := engine.Render(a.Feeds["session"].JSONL.Glob, map[string]any{"cwd": cwd})
		if err != nil {
			t.Fatal(err)
		}
		got, err := filepath.Glob(strings.Replace(pattern, "~", root, 1))
		if err != nil || len(got) != 1 || got[0] != file {
			t.Errorf("%s: pattern %q matched %v (%v)", cwd, pattern, got, err)
		}
	}
}
