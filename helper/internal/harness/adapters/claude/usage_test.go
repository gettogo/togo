package claude

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func TestContextFromUsage(t *testing.T) {
	for _, c := range []struct {
		model string
		in    int
		want  float64
	}{
		{"claude-opus-5-5", 66500, 6.7},
		{"claude-opus-4-5-20251101", 50000, 25},
		{"claude-sonnet-4-5", 300000, 30},
	} {
		ops := contextOps(message{Model: c.model, Usage: &usage{Input: c.in}})
		if len(ops) != 1 || ops[0].Op != harness.OpStatus || *ops[0].Status.ContextPercent != c.want {
			t.Errorf("%s %d: %+v", c.model, c.in, ops)
		}
	}
	if contextOps(message{Model: "claude-opus-5-5"}) != nil {
		t.Error("a message without usage reported a context")
	}
}

func TestTranscriptReportsContextWithoutStatusLine(t *testing.T) {
	m := newTranscript(env())
	_, ops := m.Map(rec(`{"type":"assistant","uuid":"a1","timestamp":"2026-11-07T10:00:00Z","message":{"id":"m1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":10,"cache_read_input_tokens":99990}}}`))
	if len(ops) != 1 || *ops[0].Status.ContextPercent != 10 {
		t.Fatalf("ops %+v", ops)
	}
}

func TestManualModeLineReadsAsDefault(t *testing.T) {
	if got := ReadMode("❯ \n───\n  ⏸ manual mode on\n")["mode"]; got != "default" {
		t.Fatalf("mode %q", got)
	}
}
