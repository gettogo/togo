package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// Live (e2e group B): changing Gemini's default model and back moved
// "model", "ide" and "security" to the end of ~/.gemini/settings.json in
// sorted order. A write changes one value and keeps the user's layout:
// key order, numbers as written, "<" and "&" unescaped.
func TestSetJSONKeyKeepsTheFilesLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	orig := `{
  "security": {
    "auth": {
      "selectedType": "oauth-personal"
    }
  },
  "model": {
    "name": "gemini-2.5-flash",
    "compressionThreshold": 0.50
  },
  "general": {
    "big": 1e6,
    "note": "a <b> & c",
    "list": [],
    "empty": {}
  }
}
`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := setJSONKey(path, "model.name", "gemini-2.5-pro"); err != nil {
		t.Fatal(err)
	}
	if err := setJSONKey(path, "model.name", "gemini-2.5-flash"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != orig {
		t.Fatalf("layout changed:\n%s", got)
	}
	if err := setJSONKey(path, "ui.theme", "Dark"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	want := orig[:len(orig)-3] + ",\n  \"ui\": {\n    \"theme\": \"Dark\"\n  }\n}\n"
	if string(got) != want {
		t.Fatalf("new key:\n%s", got)
	}
}
