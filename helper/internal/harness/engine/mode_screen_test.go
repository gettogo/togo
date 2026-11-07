package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestModeScreenRules(t *testing.T) {
	aider := newModeScreen(&spec.Modes{Screen: []spec.ScreenMode{
		{Line: `^architect( multi)?>( |$)`, ID: "architect"}, {Line: `^ask( multi)?>( |$)`, ID: "ask"},
		{Line: `^help( multi)?>( |$)`, ID: "help"}, {Line: `^([a-z-]+)?( multi)?>( |$)`, ID: "code"}}})
	crush := newModeScreen(&spec.Modes{Screen: []spec.ScreenMode{{Line: `YOLO MODE`, ID: "yolo"}}, ScreenDefault: "default"})
	cases := []struct {
		m      *modeScreen
		screen string
		want   string
	}{
		{aider, "Aider v0.86.2\nask> what is this\n\nIt is a file.\n\nask> ", "ask"},
		{aider, "ask> hi\nhello\n/code\n> ", "code"},
		{aider, "main.go\narchitect multi> ", "architect"},
		{aider, "diff> ", "code"},
		{crush, " ctrl+p commands   YOLO MODE  \n", "yolo"},
		{crush, " ctrl+p commands \n", "default"},
		{crush, "", ""},
	}
	for _, c := range cases {
		if got := c.m.ReadScreen(c.screen)["mode"]; got != c.want {
			t.Errorf("%q: mode %q, want %q", c.screen, got, c.want)
		}
	}
}

// A harness that never reports its mode (Goose) shows the one picked from
// Togo once it took the typed command.
func TestTypedModeIsKept(t *testing.T) {
	const id = "s_keepmode01"
	pane := &seqPane{}
	e := newSeq(t, id, pane, nil)
	s, _ := e.served(context.Background(), id)
	res := harness.ActionResult{Result: harness.ResultTyped}
	s.keepMode("set_mode", map[string]any{"id": "b"}, res)
	s.mu.Lock()
	got := s.stateLocked()["mode"]
	s.mu.Unlock()
	if got != "b" {
		t.Fatalf("mode %q after a typed pick", got)
	}
	s.keepMode("set_mode", map[string]any{"id": "nope"}, res)
	s.keepMode("set_mode", map[string]any{"id": "c"}, harness.ActionResult{Result: harness.ResultFailed})
	s.mu.Lock()
	got = s.stateLocked()["mode"]
	s.mu.Unlock()
	if got != "b" {
		t.Fatalf("mode %q; an unknown or failed pick must not stick", got)
	}
	_ = json.RawMessage(nil)
}
