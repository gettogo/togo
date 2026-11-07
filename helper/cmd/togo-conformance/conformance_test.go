package main

import (
	"os"
	"path/filepath"
	"regexp"
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
