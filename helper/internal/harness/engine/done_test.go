package engine

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// omp, OpenCode and other adapters without a Stop hook got only the pane
// watcher's "Done" with no body. A turn the feed ends is reported with
// the reply's text; adapters with hooks leave it to their Stop hook.
func TestTurnEndSendsDoneWithTheReply(t *testing.T) {
	for _, c := range []struct {
		adapter string
		want    bool
	}{{"omp", true}, {"claude", false}} {
		a, err := compileAdapter(harness.AdapterFiles()[c.adapter], "test")
		if err != nil {
			t.Fatal(err)
		}
		got := make(chan [3]string, 1)
		e := &Engine{cfg: Config{Now: time.Now, Done: func(id, title, body string) { got <- [3]string{id, title, body} }},
			log: &Log{NewEpoch: func() string { return "e" }}}
		e.log.Append("s_doneabc001", []harness.Item{
			{ID: "u1", Kind: harness.KindUser, User: &harness.UserBody{Text: "hi"}},
			{ID: "a1", Kind: harness.KindAssistant, Assistant: &harness.TextBody{Text: " ok \n"}},
			{ID: "t1", Kind: harness.KindTurn, Turn: &harness.TurnBody{State: harness.StateOK}},
		})
		s := &sess{e: e, id: "s_doneabc001", ad: a}
		s.doneLocked()
		select {
		case g := <-got:
			if !c.want || g[1] != a.Name+" finished" || g[2] != "ok" {
				t.Errorf("%s: done %q", c.adapter, g)
			}
		case <-time.After(200 * time.Millisecond):
			if c.want {
				t.Errorf("%s: no done", c.adapter)
			}
		}
	}
}
