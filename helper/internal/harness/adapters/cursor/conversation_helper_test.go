package cursor

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// conversationAfter feeds hook lines ({"event", "payload"}) through this
// adapter's registered mapper, the way `togo hook cursor` and the engine
// do, and returns the conversation the mapper names after each line.
func conversationAfter(t *testing.T, lines [][]byte) []string {
	t.Helper()
	d, ok := hookstyle.For("cursor")
	if !ok {
		t.Fatal("no dialect")
	}
	f, ok := harness.LookupMapper(MapperHooks)
	if !ok {
		t.Fatal("no mapper")
	}
	// A home of its own: no reader looks at the machine's agent folders.
	m := f(harness.Env{SessionID: "conv-" + t.Name(), Vars: map[string]string{source.VarHome: t.TempDir()}})
	var out []string
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	for i, l := range lines {
		var c struct {
			Event   string          `json:"event"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(l, &c) != nil || c.Event == "" {
			continue
		}
		_, norm, err := hookstyle.Normalize(d, c.Event, c.Payload)
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		m.Map(harness.Record{Source: "hooks", At: at.Add(time.Duration(i) * time.Second), Data: norm})
		out = append(out, m.State()[harness.StateConversation])
	}
	return out
}

func hookLines(t *testing.T, name string) [][]byte {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	var out [][]byte
	for sc.Scan() {
		out = append(out, append([]byte(nil), sc.Bytes()...))
	}
	return out
}
