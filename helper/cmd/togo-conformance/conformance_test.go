package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCheapestOrder(t *testing.T) {
	got := cheapest([]pickerItem{
		{ID: "opus", Title: "Opus"}, {ID: "sonnet", Title: "Sonnet"}, {ID: "haiku", Title: "Haiku"},
		{ID: "opus[1m]", Title: "Opus"}, {ID: "gpt-x", Group: "Not set up"}, {ID: "dis", Disabled: true},
	})
	if len(got) < 2 || got[0].ID != "haiku" || got[1].ID != "sonnet" {
		t.Fatalf("order = %+v", got)
	}
	for _, it := range got {
		if it.ID == "gpt-x" || it.ID == "dis" {
			t.Fatalf("kept unusable %s", it.ID)
		}
	}
	if r := cheapRank(pickerItem{ID: "qwen3-max-mini"}); r < 5 {
		t.Fatalf("a max model ranked cheap: %d", r)
	}
}

func TestSameModel(t *testing.T) {
	cases := []struct {
		picked, recorded string
		want             bool
	}{
		{"haiku", "claude-haiku-4-5-20251001", true},
		{"sonnet", "claude-haiku-4-5-20251001", false},
		{"anthropic/claude-haiku-4.5", "claude-haiku-4-5", true},
		{"openrouter/qwen/qwen3-coder:free", "qwen3-coder", true},
		{"gpt-5-mini", "gpt-5", true}, // alias containment: accepted
		{"", "x", false},
	}
	for _, c := range cases {
		if got := sameModel(c.picked, c.recorded); got != c.want {
			t.Errorf("sameModel(%q, %q) = %v", c.picked, c.recorded, got)
		}
	}
}

func TestModelSincePrefersReplies(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	head := `{"type":"user","cwd":"/x/run-w-claude"}` + "\n" + `{"type":"assistant","message":{"model":"claude-haiku-4-5","usage":{}}}` + "\n"
	if err := os.WriteFile(p, []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
	before := map[string]int64{p: int64(len(head))}
	tail := `{"type":"model_change","model":"sonnet"}` + "\n" + `{"type":"assistant","message":{"model":"claude-haiku-4-5","usage":{}}}` + "\n"
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(tail)
	f.Close()
	got, strong := modelSince(map[string]int64{p: 0}, before)
	if got != "claude-haiku-4-5" || !strong {
		t.Fatalf("got %q strong=%v; a model-change record must not count as the reply's model", got, strong)
	}
	q := filepath.Join(dir, "q.jsonl")
	_ = os.WriteFile(q, []byte(`{"type":"model_change","model":"sonnet"}`+"\n"), 0o600)
	got, strong = modelSince(map[string]int64{q: 0}, nil)
	if strong || got != "sonnet" {
		t.Fatalf("model-change only: got %q strong=%v", got, strong)
	}
}

func TestScrubJSONKeepsOrder(t *testing.T) {
	in := "{\n  \"b\": 1,\n  \"projects\": {\n    \"/a\": {\"x\": true},\n    \"/tmp/tcrabc-w-claude\": {\"hasTrustDialogAccepted\": true}\n  },\n  \"a\": [\"keep\", \"/tmp/tcrabc-w\"],\n  \"n\": 1.50\n}\n"
	out, n, err := scrubJSON([]byte(in), "tcrabc")
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	want := "{\n  \"b\": 1,\n  \"projects\": {\n    \"/a\": {\n      \"x\": true\n    }\n  },\n  \"a\": [\n    \"keep\"\n  ],\n  \"n\": 1.50\n}\n"
	if string(out) != want {
		t.Fatalf("got\n%s", out)
	}
}

func TestInputLineAndPane(t *testing.T) {
	r := &runner{inputRE: regexp.MustCompile(`^(?:│ )?❯(?: (.*?))?\s*│?$`), emptyRE: []*regexp.Regexp{regexp.MustCompile(`^Try ".*"$`)}}
	if line, ok := r.inputLine("> /cost\n  cost: $0\n────\n❯ /cost\n────"); !ok || line != "/cost" {
		t.Fatalf("line=%q ok=%v", line, ok)
	}
	if line, ok := r.inputLine("❯ /cost\nout\n────\n❯ Try \"fix lint\"\n────"); !ok || line != "" {
		t.Fatalf("empty placeholder read as %q", line)
	}
	if paneReply("> Reply with only the word ok.\n\n● ok\n", okPrompt) != "ok" {
		t.Fatal("pane reply not found")
	}
	if !updatePrompt("Update available 1.2 -> 1.3\n  Skip  Install") || updatePrompt("ready\n❯ ") {
		t.Fatal("update prompt detection")
	}
}

// A command that draws its own view instead of echoing counts as run; one
// whose name shows only where it already was does not.
func TestOpenedView(t *testing.T) {
	prior := "ok\n π > Haiku\n╰─\n"
	view := "╭─ Context · Claude Haiku 4.5 ──╮\n│ Used 21K of 200K │\n"
	if !opened(prior, view, "/context") {
		t.Fatal("the context view was not seen")
	}
	if opened("talk about context\n", "talk about context\n╰─\n", "/context") {
		t.Fatal("an old line counted")
	}
	if !opened(prior, "   Help  General   Commands\n", "/help") {
		t.Fatal("Claude's help view was not seen")
	}
	// A lost /context must not pass on Claude's own status line.
	if opened(prior, "  Context left until auto-compact: 5%\n", "/context") {
		t.Fatal("a sentence that starts with the name counted")
	}
}

// Copilot CLI records side calls (a title, a summary) on a nano model after
// the reply; the assistant's own message names the model the turn ran on.
func TestModelSincePrefersTheAssistantsMessage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	lines := `{"type":"assistant.message","data":{"model":"claude-haiku-4.5"}}` + "\n" +
		`{"type":"model.turn_ended","data":{"model":"gpt-5.4-nano"}}` + "\n" +
		`{"type":"session.usage_record","data":{"usage":{"model":"gpt-5.4-nano"}}}` + "\n"
	if err := os.WriteFile(p, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, strong := modelSince(map[string]int64{p: 0}, nil); got != "claude-haiku-4.5" || !strong {
		t.Fatalf("got %q strong=%v", got, strong)
	}
}

func TestParseArgs(t *testing.T) {
	got, err := parseArgs("cline=--auto-approve false; droid = --x ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got["cline"], "|") != "--auto-approve|false" || strings.Join(got["droid"], "|") != "--x" || len(got) != 2 {
		t.Fatalf("parseArgs = %v", got)
	}
	for _, bad := range []string{"cline", "=x", "cline="} {
		if _, err := parseArgs(bad); err == nil {
			t.Errorf("parseArgs(%q) took it", bad)
		}
	}
}
