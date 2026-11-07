package itemtext

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func TestCategory(t *testing.T) {
	for name, want := range map[string]string{
		"Bash": harness.ToolShell, "Read": harness.ToolRead, "ReadMediaFile": harness.ToolRead,
		"Edit": harness.ToolEdit, "str-replace-editor": harness.ToolEdit, "Write": harness.ToolWrite,
		"Grep": harness.ToolSearch, "Glob": harness.ToolSearch, "WebSearch": harness.ToolWeb,
		"FetchURL": harness.ToolWeb, "Agent": harness.ToolAgent, "mcp__shadcn__list": harness.ToolMCP,
		"TodoList": harness.ToolOther, "CreateGoal": harness.ToolOther, "TaskOutput": harness.ToolOther,
	} {
		if got := Category(name); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
}

func TestTextBounds(t *testing.T) {
	if got := Title("first line\nsecond"); got != "first line" {
		t.Fatal(got)
	}
	if got := Truncate("héllo wörld", 5); got != "héll…" {
		t.Fatal(got)
	}
	if got := Tail("añb", 2); got != "b" {
		t.Fatalf("%q", got)
	}
	if got := Clip(strings.Repeat("é", harness.MaxItemText)); len(got) > harness.MaxItemText {
		t.Fatal(len(got))
	}
	if Rel("/a/b", "/a/b/c.go") != "c.go" || Rel("/a/b", "/x/y") != "/x/y" {
		t.Fatal("rel")
	}
}
