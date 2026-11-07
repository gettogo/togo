package engine

import "testing"

func TestFolderFilters(t *testing.T) {
	data := map[string]any{"cwd": "/Users/jamie"}
	for src, want := range map[string]string{
		`-{{.cwd | replace "/" "-"}}--`: "--Users-jamie--",
		`{{slice (sha256 .cwd) 0 12}}`:  "79a25ac0eb0f",
		`{{replace "/" "-" "a/b c"}}`:   "a-b c",
	} {
		got, err := Render(src, data)
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", src, got, err, want)
		}
	}
	// An adapter checked with no data must still render.
	if _, err := Render(`{{slice (sha256 .cwd) 0 12}}`, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := Render(`[{{.cwd | replace "/" "-"}}]`, map[string]any{}); got != "[]" {
		t.Fatalf("missing value rendered %q", got)
	}
}

// A value printf could not format ("%!s(<nil>)") fails the render, so it
// never lands in a config file: an omp role once read "%!s(<nil>):low".
func TestRenderRefusesFormattingErrors(t *testing.T) {
	if _, err := Render(`{{printf "%s%s" .id ":low" | json}}`, map[string]any{}); err == nil {
		t.Fatal("rendered a missing id")
	}
	if got, err := Render(`{{printf "%s%s" .id ":low" | json}}`, map[string]any{"id": "apple/on-device"}); err != nil || got != `"apple/on-device:low"` {
		t.Fatalf("got %q %v", got, err)
	}
}
