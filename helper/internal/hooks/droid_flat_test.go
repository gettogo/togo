package hooks

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type droidGroup struct {
	Matcher string `json:"matcher"`
	Hooks   []struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	} `json:"hooks"`
}

func droidDoc(t *testing.T, s string) map[string]json.RawMessage {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("%v in %s", err, s)
	}
	return doc
}

func droidGroups(t *testing.T, doc map[string]json.RawMessage, ev string) []droidGroup {
	t.Helper()
	var gs []droidGroup
	if err := json.Unmarshal(doc[ev], &gs); err != nil {
		t.Fatalf("%s: %v", ev, err)
	}
	return gs
}

// Droid reads hooks.json as event name -> matcher groups at top level; a
// "hooks" key is rejected as an unknown event.
func TestDroidInstallWritesFlatShape(t *testing.T) {
	e := optionalEnv(t)
	path := filepath.Join(e.o.FactoryDir, "hooks.json")
	mustOK(t, Install(e.o, []string{Droid}))
	doc := droidDoc(t, read(t, path))
	if _, ok := doc["hooks"]; ok {
		t.Fatalf("nested hooks key written: %s", read(t, path))
	}
	if len(doc) != len(droidEvents) {
		t.Fatalf("keys %d, want %d: %s", len(doc), len(droidEvents), read(t, path))
	}
	for _, ev := range droidEvents {
		gs := droidGroups(t, doc, ev)
		if len(gs) != 1 || len(gs[0].Hooks) != 1 || gs[0].Hooks[0].Type != "command" ||
			gs[0].Hooks[0].Command != hookCommand(e.o.Binary, Droid, ev) {
			t.Fatalf("%s: %+v", ev, gs)
		}
	}
	if st := Status(e.o)[Droid]; st != StatusInstalled {
		t.Fatalf("status %s", st)
	}
	mustOK(t, Uninstall(e.o, []string{Droid}))
	if fileExists(path) {
		t.Fatal("hooks.json togo created was left behind")
	}
}

// With the restore path unavailable (the user edited the file after
// install), uninstall removes only Togo's handlers.
func TestDroidUninstallKeepsForeignFlatEntries(t *testing.T) {
	e := optionalEnv(t)
	path := filepath.Join(e.o.FactoryDir, "hooks.json")
	orig := `{
  "PreToolUse": [
    {
      "matcher": "Execute",
      "hooks": [
        {
          "type": "command",
          "command": "audit.sh"
        }
      ]
    }
  ],
  "Stop": [
    {
      "hooks": [
        {
          "type": "command",
          "command": "say done"
        }
      ]
    }
  ]
}
`
	write(t, path, []byte(orig))
	mustOK(t, Install(e.o, []string{Droid}))
	doc := droidDoc(t, read(t, path))
	if gs := droidGroups(t, doc, "PreToolUse"); len(gs) != 2 || gs[0].Matcher != "Execute" || gs[0].Hooks[0].Command != "audit.sh" {
		t.Fatalf("PreToolUse %+v", gs)
	}
	if !strings.Contains(string(doc["Stop"]), "say done") || !strings.Contains(string(doc["Stop"]), "hook droid Stop") {
		t.Fatalf("Stop %s", doc["Stop"])
	}
	// The user touches the file, so uninstall cannot restore the backup.
	edited := strings.Replace(read(t, path), "say done", "say finished", 1)
	write(t, path, []byte(edited))
	mustOK(t, Uninstall(e.o, []string{Droid}))
	got := read(t, path)
	if want := strings.Replace(orig, "say done", "say finished", 1); got != want {
		t.Fatalf("uninstall left\n%s\nwant\n%s", got, want)
	}
}

// legacyDroid is a hooks.json an earlier Togo build wrote: everything under
// a nested "hooks" object, with a foreign entry the user kept there and one
// flat foreign entry.
func legacyDroid(binary string) string {
	nested := map[string]any{}
	for _, ev := range droidEvents {
		nested[ev] = []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": hookCommand(binary, Droid, ev)}}}}
	}
	nested["Stop"] = append(nested["Stop"].([]any), map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "say done"}}})
	b, _ := json.MarshalIndent(map[string]any{
		"hooks":      nested,
		"SessionEnd": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "cleanup.sh"}}}},
	}, "", "  ")
	return string(b) + "\n"
}

func TestDroidMigratesLegacyNestedFile(t *testing.T) {
	e := optionalEnv(t)
	path := filepath.Join(e.o.FactoryDir, "hooks.json")
	write(t, path, []byte(legacyDroid(e.o.Binary)))
	if st := Status(e.o)[Droid]; st != StatusOutdated {
		t.Fatalf("legacy status %s, want outdated", st)
	}
	mustOK(t, Install(e.o, []string{Droid}))
	got := read(t, path)
	doc := droidDoc(t, got)
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(doc["hooks"], &nested); err != nil {
		t.Fatalf("nested: %v in %s", err, got)
	}
	// Only the foreign nested entry stays nested; Togo's moved out.
	if strings.Contains(string(doc["hooks"]), "hook droid") || len(nested) != 1 || !strings.Contains(string(nested["Stop"]), "say done") {
		t.Fatalf("nested left %s", doc["hooks"])
	}
	for _, ev := range droidEvents {
		gs := droidGroups(t, doc, ev)
		n := slices.IndexFunc(gs, func(g droidGroup) bool {
			return len(g.Hooks) == 1 && g.Hooks[0].Command == hookCommand(e.o.Binary, Droid, ev)
		})
		if n < 0 || strings.Count(string(doc[ev]), "hook droid") != 1 {
			t.Fatalf("%s: %s", ev, doc[ev])
		}
	}
	if !strings.Contains(string(doc["SessionEnd"]), "cleanup.sh") {
		t.Fatalf("flat foreign entry lost: %s", doc["SessionEnd"])
	}
	if st := Status(e.o)[Droid]; st != StatusInstalled {
		t.Fatalf("status %s", st)
	}
	mustOK(t, Install(e.o, []string{Droid}))
	if read(t, path) != got {
		t.Fatal("a second install changed the file")
	}
}

// A nested object holding only Togo's entries is dropped entirely.
func TestDroidMigrationDropsEmptiedNestedObject(t *testing.T) {
	nested := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"'/opt/togo/bin/togo' hook droid Stop"}]}]}}`
	s := spec{agent: Droid, binary: "/opt/togo/bin/togo", add: true, want: droidEvents, all: droidEvents}
	out, err := editFlatHooksJSON([]byte(nested), s)
	if err != nil {
		t.Fatal(err)
	}
	doc := droidDoc(t, string(out))
	if _, ok := doc["hooks"]; ok || len(doc) != len(droidEvents) {
		t.Fatalf("got %s", out)
	}
}

// Uninstall on a legacy file never reinstalled also removes Togo's nested
// entries and leaves the rest.
func TestDroidUninstallCleansLegacyNestedFile(t *testing.T) {
	e := optionalEnv(t)
	path := filepath.Join(e.o.FactoryDir, "hooks.json")
	write(t, path, []byte(legacyDroid(e.o.Binary)))
	mustOK(t, Uninstall(e.o, []string{Droid}))
	got := read(t, path)
	if strings.Contains(got, "hook droid") || !strings.Contains(got, "say done") || !strings.Contains(got, "cleanup.sh") {
		t.Fatalf("uninstall left %s", got)
	}
	if st := Status(e.o)[Droid]; st != StatusAbsent {
		t.Fatalf("status %s", st)
	}
}
