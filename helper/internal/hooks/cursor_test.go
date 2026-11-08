package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func cursorEnv(t *testing.T) (env, string) {
	t.Helper()
	e := newEnv(t)
	e.o.CursorDir = filepath.Join(filepath.Dir(e.o.ClaudeDir), "cursor")
	if err := os.MkdirAll(e.o.CursorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return e, filepath.Join(e.o.CursorDir, "hooks.json")
}

// Install keeps the user's own Cursor hooks, writes one guarded Togo
// command per event, is idempotent, and uninstall restores the file byte
// for byte.
func TestCursorInstallKeepsHooksAndUninstallRestores(t *testing.T) {
	e, path := cursorEnv(t)
	orig := "{\n  \"version\": 1,\n  \"hooks\": {\n    \"stop\": [\n      {\n        \"command\": \"./hooks/audit.sh\"\n      }\n    ]\n  }\n}\n"
	write(t, path, []byte(orig))
	if !Known(Cursor) || !slices.Contains(Default(e.o), Cursor) {
		t.Fatal("cursor must take hooks and be in the default set when installed")
	}
	mustOK(t, Install(e.o, []string{Cursor}))
	got := read(t, path)
	var doc struct {
		Version int                                   `json:"version"`
		Hooks   map[string][]struct{ Command string } `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || len(doc.Hooks["stop"]) != 2 || doc.Hooks["stop"][0].Command != "./hooks/audit.sh" {
		t.Fatalf("stop %+v", doc)
	}
	for _, ev := range cursorEvents {
		h := doc.Hooks[ev]
		if len(h) == 0 || !strings.HasPrefix(h[len(h)-1].Command, "[ -x '/opt/togo/bin/togo' ] && exec '/opt/togo/bin/togo' hook cursor "+ev) {
			t.Fatalf("%s: %+v", ev, h)
		}
	}
	if st := Status(e.o)[Cursor]; st != StatusInstalled {
		t.Fatalf("status %s", st)
	}
	mustOK(t, Install(e.o, []string{Cursor}))
	if read(t, path) != got {
		t.Fatal("second install changed the file")
	}
	mustOK(t, Uninstall(e.o, []string{Cursor}))
	if read(t, path) != orig {
		t.Fatalf("not restored:\n%s", read(t, path))
	}
}

// A hooks.json Togo created is deleted on uninstall; one the user edited
// since keeps the edit and loses only Togo's entries.
func TestCursorUninstallAfterUserEdit(t *testing.T) {
	e, path := cursorEnv(t)
	mustOK(t, Install(e.o, []string{Cursor}))
	if !strings.Contains(read(t, path), `"version": 1`) {
		t.Fatalf("new file lacks version: %s", read(t, path))
	}
	mustOK(t, Uninstall(e.o, []string{Cursor}))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("hooks.json Togo created was left behind")
	}
	mustOK(t, Install(e.o, []string{Cursor}))
	cur := strings.Replace(read(t, path), `"stop": [`, `"stop": [
      { "command": "say done" },`, 1)
	write(t, path, []byte(cur))
	mustOK(t, Uninstall(e.o, []string{Cursor}))
	left := read(t, path)
	if strings.Contains(left, "hook cursor") || !strings.Contains(left, "say done") {
		t.Fatalf("after uninstall: %s", left)
	}
}

func TestCursorRefusesEditedEntry(t *testing.T) {
	e, path := cursorEnv(t)
	write(t, path, []byte(`{"version":1,"hooks":{"stop":[{"command":"/opt/togo/bin/togo --debug hook cursor stop"}]}}`))
	res := Install(e.o, []string{Cursor})
	if res[0].State != "refused" {
		t.Fatalf("%+v", res[0])
	}
}
