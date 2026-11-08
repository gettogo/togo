package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Two Pi sessions in one folder: A has its transcript, B starts and
// writes its own. A must not join B's file, and B gets it.
func TestExclusiveGlobKeepsNeighboursApart(t *testing.T) {
	envA, h := testEnv(t)
	envA.SessionID = "s_exclaaaa01"
	envA.Vars["created"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	envB := envA
	envB.Vars = map[string]string{}
	for k, v := range envA.Vars {
		envB.Vars[k] = v
	}
	envB.SessionID = "s_exclbbbb01"
	_ = os.WriteFile(filepath.Join(h, "a.jsonl"), []byte(`{"f":"a1"}`+"\n"), 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	spec := sspec("t", map[string]any{"glob": "~/*.jsonl", "follow": true, "exclusive": true})
	a := &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, spec, envA, a.emit) }()
	if got := a.wait(t, 1); got[0] != `{"f":"a1"}` {
		t.Fatalf("A first = %v", got)
	}
	time.Sleep(50 * time.Millisecond)
	envB.Vars["created"] = time.Now().UTC().Format(time.RFC3339)
	b := &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, spec, envB, b.emit) }()
	time.Sleep(50 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(h, "b.jsonl"), []byte(`{"f":"b1"}`+"\n"), 0o600)
	if got := b.wait(t, 1); got[0] != `{"f":"b1"}` {
		t.Fatalf("B first = %v", got)
	}
	time.Sleep(200 * time.Millisecond)
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.recs) != 1 {
		t.Fatalf("A joined its neighbour's file: %v", a.recs)
	}
}
