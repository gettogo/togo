package termmenu

import (
	"os"
	"path/filepath"
	"testing"
)

// Screens captured from real programs (paths and local names replaced):
// Kimi 2.6.0, OpenCode 2, omp 18.20 in its ordinary terminal.
func read(t *testing.T, name string) (Menu, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".ansi"))
	if err != nil {
		t.Fatal(err)
	}
	return Read(string(raw))
}

func labels(m Menu) []string {
	var out []string
	for _, it := range m.Items {
		out = append(out, it.Label)
	}
	return out
}

func TestKimiPermissionChooser(t *testing.T) {
	m, ok := read(t, "kimi-permission")
	if !ok {
		t.Fatal("not read")
	}
	if m.Title != "Select permission mode" || m.Hint != "↑↓ navigate · Enter select · Esc cancel" {
		t.Errorf("title %q, hint %q", m.Title, m.Hint)
	}
	want := []Item{
		{Label: "Always Ask", Detail: "Auto-read only; everything else needs your approval first.", Cursor: true, Current: true},
		{Label: "Ask When Needed", Detail: "Routine edits and commands run automatically; risky actions, questions, and plans still ask."},
		{Label: "Never Ask", Detail: "Everything runs and is decided automatically; only plans in Plan mode wait for your review."},
	}
	if len(m.Items) != len(want) {
		t.Fatalf("items = %+v", m.Items)
	}
	for i := range want {
		if m.Items[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, m.Items[i], want[i])
		}
	}
}

func TestKimiCommandMenuShowsItsPosition(t *testing.T) {
	m, ok := read(t, "kimi-slash")
	if !ok {
		t.Fatal("not read")
	}
	if m.Position != 1 || m.Total != 96 {
		t.Errorf("counter = %d/%d", m.Position, m.Total)
	}
	got := labels(m)
	if len(got) != 5 || got[0] != "yolo" || got[1] != "model" || got[2] != "permission" || got[4] != "settings" {
		t.Fatalf("rows = %q", got)
	}
	if !m.Items[0].Cursor || m.Items[1].Detail != "Switch LLM model" || m.Items[4].Detail != "Open TUI settings" {
		t.Errorf("rows = %+v", m.Items)
	}
}

func TestOpenCodeCommandMenuIsFoundByItsHighlight(t *testing.T) {
	m, ok := read(t, "oc-slash")
	if !ok {
		t.Fatal("not read")
	}
	if len(m.Items) != 10 || m.Items[0].Label != "/agents" || m.Items[0].Detail != "Switch agent" || !m.Items[0].Cursor {
		t.Fatalf("rows = %+v", m.Items)
	}
	if m.Items[9].Label != "/clear" || m.Items[9].Detail != "Clear session" || m.Items[9].Cursor {
		t.Errorf("last row = %+v", m.Items[9])
	}
}

func TestOpenCodeModelChooserHasGroupsSearchAndCurrent(t *testing.T) {
	m, ok := read(t, "oc-models")
	if !ok {
		t.Fatal("not read")
	}
	if m.Title != "Select model" || !m.Search {
		t.Errorf("title %q, search %v", m.Title, m.Search)
	}
	var cursor, cur *Item
	groups := map[string]int{}
	for i := range m.Items {
		it := &m.Items[i]
		groups[it.Group]++
		if it.Cursor {
			cursor = it
		}
		if it.Current {
			cur = it
		}
	}
	if cursor == nil || cur == nil || cursor.Label != "Local Model B" || cur.Label != "Local Model B" {
		t.Errorf("cursor %+v, current %+v", cursor, cur)
	}
	if groups["Moonshot AI"] != 5 || groups["Local server"] != 2 {
		t.Errorf("groups = %v", groups)
	}
	if m.Items[0].Label != "Beam Free" || m.Items[0].Detail != "Free" {
		t.Errorf("first row = %+v", m.Items[0])
	}
}

func TestOmpTerminalLists(t *testing.T) {
	m, ok := read(t, "omp-slash")
	if !ok || len(m.Items) != 10 || m.Items[0].Label != "security" || !m.Items[0].Cursor ||
		m.Items[1].Label != "settings" || m.Items[1].Detail != "Open settings menu" {
		t.Errorf("command menu = %v %+v", ok, m.Items)
	}
	m, ok = read(t, "omp-login")
	if !ok || m.Title != "Select provider to login" || !m.Search || len(m.Items) != 10 ||
		m.Items[0].Label != "ChatGPT Plus/Pro (Codex Subscription)" || m.Items[0].Detail != "" || m.Items[5].Label != "OpenRouter" {
		t.Errorf("sign-in list = %v %q %+v", ok, m.Title, m.Items)
	}
	m, ok = read(t, "omp-switch")
	if !ok || m.Title != "Switch Model" || !m.Search || len(m.Items) != 1 || m.Items[0].Label != "apple/on-device" {
		t.Errorf("model switch = %v %q %+v", ok, m.Title, m.Items)
	}
}

// omp's settings page is two panes; the rows of the pane the cursor is in
// are read with their current values.
func TestOmpSettingsRowsCarryTheirValues(t *testing.T) {
	m, ok := read(t, "omp-settings")
	if !ok || m.Title != "Settings" {
		t.Fatalf("settings = %v %q", ok, m.Title)
	}
	values := map[string]string{}
	var cursor string
	for _, it := range m.Items {
		values[it.Label] = it.Detail
		if it.Cursor {
			cursor = it.Label
		}
	}
	if cursor != "Dark Theme" || values["Dark Theme"] != "titanium" || values["Light Theme"] != "light" ||
		values["Symbol Preset"] != "unicode" || values["Max Frames Per Second"] != "30" {
		t.Errorf("cursor %q, values %v", cursor, values)
	}
}

// A screen that is not a list is not read as one.
func TestOtherScreensAreNotLists(t *testing.T) {
	if m, ok := read(t, "omp-tree"); ok {
		t.Errorf("a tree was read as a list: %+v", m)
	}
	for _, screen := range []string{"", "plain output\nmore output\n$ ", "\x1b[1mBuild finished\x1b[0m\n"} {
		if m, ok := Read(screen); ok {
			t.Errorf("%q read as a list: %+v", screen, m)
		}
	}
}
