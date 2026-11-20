package copilot

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// testdata/subagent-reply.events.jsonl and subagent-reply-hooks.jsonl are
// a real Copilot CLI 1.0.114 run, scrubbed, with the records the reader
// never reads (system prompt, usage) left out: the main agent starts one
// subagent that answers "ok", then replies "finished". The hooks are the
// hook.start inputs of the same log, in its order.
const replyMain = "35aadc17-38dd-4a56-974e-3c43e4f69ede"

// subagentTurnStart is when the subagent's turn starts in that log, after
// its prompt and before its "ok".
const subagentTurnStart = 1795184073090

// The log is read in pieces, wherever it has got to when a hook comes in.
// Wherever a piece ends, the subagent's "ok" is not the main agent's
// reply: only "finished" is.
func TestSubagentAnswerIsNotTheReplyWhereverAReadEnds(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".copilot", "session-state", replyMain)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lines := hookLines(t, "subagent-reply.events.jsonl")
	log := append(bytes.Join(lines, []byte("\n")), '\n')
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), log, 0o600); err != nil {
		t.Fatal(err)
	}
	env := harness.Env{Vars: map[string]string{source.VarHome: home}}
	var cut int64
	for i := range lines {
		cut += int64(len(lines[i]) + 1)
		first, next := repliesUpTo(env, replyMain, 0, cut)
		rest, _ := repliesUpTo(env, replyMain, next, -1)
		var said []string
		for _, r := range append(first, rest...) {
			if r.Text != "" {
				said = append(said, r.Text)
			}
		}
		if !slices.Equal(said, []string{"finished"}) {
			t.Errorf("read cut after line %d: main agent said %q, want just finished", i+1, said)
		}
	}
}

// The live run: the main agent's task call was read when the log already
// held the subagent's prompt and turn start, so the next read began at its
// "ok". The turn's reply is "finished", "ok" stays the subagent's answer,
// and a replay after a helper restart draws the same.
func TestSubagentAnswerReadLateIsNotTheReply(t *testing.T) {
	c := capture{main: replyMain, hooks: "subagent-reply-hooks.jsonl", events: "subagent-reply.events.jsonl",
		readAt: func(event string, _ int64) int64 {
			if event == "preToolUse" {
				return subagentTurnStart
			}
			return 0
		}}
	all, last, _, replay := capturedRun(t, c)
	var said []string
	for _, x := range all {
		if x.Kind == harness.KindAssistant && x.Parent == "" {
			said = append(said, x.Assistant.Text)
		}
	}
	if !slices.Equal(said, []string{"finished"}) {
		t.Errorf("main agent said %q, want just finished", said)
	}
	turns := 0
	for _, x := range last {
		switch x.Kind {
		case harness.KindTurn:
			turns++
			if x.Turn.State != harness.StateOK {
				t.Errorf("turn ended %s", x.Turn.State)
			}
		case harness.KindAgent:
			if x.Agent.Summary != "ok" || x.Agent.State != harness.StateOK {
				t.Errorf("agent %+v", *x.Agent)
			}
		}
	}
	if turns != 1 {
		t.Errorf("%d turns, want 1", turns)
	}

	if again := replay(); viewItems(again) != viewItems(all) {
		t.Errorf("replayed:\n%s\nfirst mapped:\n%s", viewItems(again), viewItems(all))
	}
}
