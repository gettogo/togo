package tape

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The pilot: a long omp session of 35 turns, a helper restart, and an
// assistant reply on the final page.
func TestPilotLongChatRestart(t *testing.T) {
	sc, ok := Lookup("long-chat-restart")
	if !ok {
		t.Fatal("no long-chat-restart")
	}
	if why := sc.Applies("omp"); why != "" {
		t.Fatal(why)
	}
	w, err := sc.Run(t, "omp")
	if err != nil {
		t.Fatal(err)
	}
	if w.Layout.Truth.Prompts != 35 {
		t.Errorf("%d turns, want 35", w.Layout.Truth.Prompts)
	}
	c, err := HostCleaner(w.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	lines, _, err := c.Clean(w.Rec.Lines())
	if err != nil {
		t.Fatal(err)
	}
	var methods []string
	var finals []harness.OpenResult
	var last harness.OpenResult
	for _, l := range lines {
		var f struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(l, &f); err != nil {
			t.Fatal(err)
		}
		if f.Method != "" {
			methods = append(methods, f.Method)
		}
		if f.Result != nil && bytes.Contains(f.Result, []byte(`"cursor"`)) {
			var r harness.OpenResult
			if err := json.Unmarshal(f.Result, &r); err != nil {
				t.Fatal(err)
			}
			finals = append(finals, r)
			last = r
		}
	}
	want := []string{"harness.describe", "harness.open", "replay.eof", "harness.open", "harness.open"}
	if len(methods) != len(want) {
		t.Fatalf("calls %v, want %v", methods, want)
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("calls %v, want %v", methods, want)
		}
	}
	if len(finals) != 3 || finals[0].Epoch == finals[1].Epoch || finals[1].Epoch != finals[2].Epoch {
		t.Errorf("epochs across the three opens: %+v", finals)
	}
	if !hasReply(last.Items) {
		t.Error("no assistant reply on the final page")
	}
	if len(last.Items) < 80 {
		t.Errorf("only %d items on the final page", len(last.Items))
	}
}
