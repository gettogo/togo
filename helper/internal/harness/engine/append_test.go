package engine

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func textNotes(sub *fakeSub, id string) []harness.TextBody {
	var out []harness.TextBody
	for _, n := range sub.notes() {
		for _, it := range n.Items {
			if it.ID == id && it.Assistant != nil {
				out = append(out, *it.Assistant)
			}
		}
	}
	return out
}

// A streamed reply was resent whole every 250 ms (up to 64 KB). Updates
// now carry only the new tail and where it continues; the final text and
// anything that does not continue what was sent go whole.
func TestStreamedTextSendsTails(t *testing.T) {
	const id = "s_appendtxt1"
	var status atomic.Value
	status.Store("idle")
	e := statusEngine(t, id, &seqPane{mode: "a"}, &status)
	sub := &fakeSub{done: make(chan struct{})}
	defer close(sub.done)
	if _, err := e.Open(context.Background(), sub, id, 0, 50); err != nil {
		t.Fatal(err)
	}
	e.Release(sub, id)
	send := func(text string, partial bool) {
		deliver(id, map[string]any{"items": []harness.Item{{ID: "a1", Kind: harness.KindAssistant,
			Assistant: &harness.TextBody{Text: text, Partial: partial}}}})
		time.Sleep(300 * time.Millisecond) // past the partial throttle
	}
	send("Hello", true)
	send("Hello wörld", true)
	send("Bye", true) // does not continue: whole
	send("Bye now.", false)
	waitFor(t, func() bool { return len(textNotes(sub, "a1")) == 4 })
	got := textNotes(sub, "a1")
	if got[0].Text != "Hello" || got[0].AppendFrom != nil {
		t.Errorf("first %+v", got[0])
	}
	if got[1].Text != " wörld" || got[1].AppendFrom == nil || *got[1].AppendFrom != len("Hello") {
		t.Errorf("tail %+v", got[1])
	}
	if got[2].Text != "Bye" || got[2].AppendFrom != nil {
		t.Errorf("replacement %+v", got[2])
	}
	if got[3].Text != "Bye now." || got[3].AppendFrom != nil || got[3].Partial {
		t.Errorf("final %+v", got[3])
	}
	// The log, which harness.open serves, always holds whole text.
	if it, _ := e.log.Get(id, "a1"); it.Assistant.Text != "Bye now." || it.Assistant.AppendFrom != nil {
		t.Errorf("log %+v", it.Assistant)
	}
	_ = strings.Contains
}
