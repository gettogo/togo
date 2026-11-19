package engine

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	// The scripted harness of e2e_test.go plus a command list that its
	// source re-reads every few milliseconds, so completions keep meeting a
	// freshly decoded value.
	ad := strings.NewReplacer(
		"adapter: fake\n", "adapter: hammer\n",
		"agents: [fake]", "agents: [hammer]",
		"sources:\n", "sources:\n  cmds:\n    cli: { argv: [fakeharness, cmds] }\n    cache: 2ms\n    stability: documented\n",
		"commands:\n", "commands:\n  source: cmds\n  select: '[.cmds[]]'\n",
	).Replace(fakeAdapter)
	harness.RegisterAdapterFile("hammer", []byte(ad))
}

// hammerMarker is fakeMarker with a lock: the hammer marks from many calls.
type hammerMarker struct {
	mu sync.Mutex
	n  int64
}

func (m *hammerMarker) Mark(context.Context, string) (harness.Anchor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	return harness.Anchor{Epoch: "e", Line: m.n}, nil
}

func (m *hammerMarker) Find(_ context.Context, _ string, a harness.Anchor) (harness.Location, error) {
	return harness.Location{Found: true, LinesFromBottom: 10 - a.Line}, nil
}

// hammerEngine serves the given sessions, all run by the hammer adapter.
func hammerEngine(t *testing.T, ids ...string) *Engine {
	t.Helper()
	bin, home := t.TempDir(), t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo 'fake 1.2.3';;\n" +
		"models) echo '{\"models\":[{\"id\":\"x\",\"name\":\"X\"},{\"id\":\"y\",\"name\":\"Y\"}]}';;\n" +
		"cmds) echo '{\"cmds\":[{\"name\":\"alpha\",\"n\":3,\"w\":[1,2.5]},{\"name\":\"beta\",\"n\":4,\"detail\":\"d\"}]}';;\n" +
		"*) exit 2;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "fakeharness"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(home, "work")
	for _, f := range []string{"a.txt", "b.go", "sub/c.md", "sub/deep/d.txt"} {
		p := filepath.Join(cwd, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sessions := fakeSessions{}
	for _, id := range ids {
		sessions[id] = session.Session{ID: id, Agent: "hammer", Managed: true, Cwd: cwd}
	}
	pane := &fakePane{mode: "a"}
	e := New(Config{
		Sessions: sessions, Typist: pane, Marker: &hammerMarker{}, Screen: pane.Screen, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.05,
	})
	t.Cleanup(func() {
		for _, id := range ids {
			e.Forget(id)
		}
	})
	return e
}

func hammerIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = "s_hammer" + string(rune(0x61+i)) + "222"
	}
	return ids
}

// callArgs builds the params for one call of a method; every method of the
// table needs an entry, so a new method cannot go unhammered.
type callArgs func(r *rand.Rand, id string) any

const hammerPool = 40 // distinct item ids the storm reuses

var hammerCalls = map[string]callArgs{
	harness.MethodDescribe: func(_ *rand.Rand, id string) any { return map[string]any{"id": id} },
	harness.MethodOpen: func(r *rand.Rand, id string) any {
		return map[string]any{"id": id, "after": r.Intn(30), "limit": 1 + r.Intn(60)}
	},
	harness.MethodClose: func(_ *rand.Rand, id string) any { return map[string]any{"id": id} },
	harness.MethodHistory: func(r *rand.Rand, id string) any {
		return map[string]any{"id": id, "before": 1 + r.Intn(400), "limit": 1 + r.Intn(15)}
	},
	harness.MethodScreen: func(r *rand.Rand, id string) any {
		return map[string]any{"id": id, "screen": []string{"model", "broken", "nope"}[r.Intn(3)]}
	},
	harness.MethodAction: func(r *rand.Rand, id string) any {
		a := []string{"set_model", "set_model", "interrupt", "nope", "set_mode"}[r.Intn(5)]
		args := map[string]any{"id": []string{"x", "y"}[r.Intn(2)]}
		if a == "set_mode" {
			args["id"] = []string{"a", "b", "c"}[r.Intn(3)]
		}
		return map[string]any{"id": id, "action": a, "args": args}
	},
	harness.MethodSend: func(r *rand.Rand, id string) any {
		return map[string]any{"id": id, "text": fmt.Sprintf("prompt %d", r.Intn(1000)), "mode": []string{"queue", "now"}[r.Intn(2)]}
	},
	harness.MethodInterrupt: func(_ *rand.Rand, id string) any { return map[string]any{"id": id} },
	harness.MethodComplete: func(r *rand.Rand, id string) any {
		kind, q := "command", []string{"", "a", "al", "be", "mo", "cl"}[r.Intn(6)]
		if r.Intn(2) == 0 {
			kind, q = "file", []string{"", "a", "sub/", "sub/d", "b.g"}[r.Intn(5)]
		}
		return map[string]any{"id": id, "kind": kind, "query": q}
	},
	harness.MethodLocate: func(r *rand.Rand, id string) any {
		return map[string]any{"id": id, "anchor": map[string]any{"epoch": "e", "line": r.Intn(20), "needle": "x"}}
	},
}

// mustRead are methods that have no reason to fail with valid arguments.
var mustRead = map[string]bool{harness.MethodDescribe: true, harness.MethodOpen: true, harness.MethodHistory: true,
	harness.MethodLocate: true, harness.MethodComplete: true}
