package main

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/detect"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]|\x1b\\][^\x1b\a]*(\x1b\\\\|\a)")

// The PermissionRequest hook of Claude Code's question tool and the
// dialog on screen are one request (a real 2.1.329 run): the prompts the
// hook waits on are the questions the dialog's steps show, so the dialog
// is paired with the waiting hook and opens no request of its own.
func TestQuestionHookAndDialogAreOneRequest(t *testing.T) {
	const dir = "../../internal/harness/adapters/claude/testdata/"
	f, err := os.Open(dir + "ask-two-questions-hooks.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var prompts []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var c struct {
			Event   string          `json:"event"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		if c.Event != "PermissionRequest" {
			continue
		}
		if _, p, ok := claudeAsk(c.Payload); ok {
			prompts = p
		}
	}
	var shown []string
	for _, step := range []string{"ask-step1.ansi", "ask-step2.ansi"} {
		b, err := os.ReadFile(dir + "screens/" + step)
		if err != nil {
			t.Fatal(err)
		}
		p, ok := detect.Detect(ansi.ReplaceAllString(string(b), ""))
		if !ok || !p.Ask {
			t.Fatalf("%s: %v %+v", step, ok, p)
		}
		shown = append(shown, p.Title)
	}
	if len(prompts) != 2 || !slices.Equal(prompts, shown) {
		t.Fatalf("hook waits on %q, dialog shows %q", prompts, shown)
	}
}
