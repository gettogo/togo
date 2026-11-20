package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Seen in review: a hook store that spans a /new (written by a helper that
// kept both conversations, or cut by a crash before the move was saved)
// was replayed whole after a restart, the old conversation included, and
// the chat then named no conversation, so the next move went unseen. Now
// the replay starts the chat over where the store moves, keeps only the
// conversation it moved to, and a later move still starts over.
func TestHookStoreSpanningAMoveRestoresOnlyTheLastConversation(t *testing.T) {
	const id = "s_hookmove01"
	home, togoHome := t.TempDir(), t.TempDir()
	ctx := context.Background()
	rec := func(conv string, i int) []byte {
		r := turnRecord(i, harness.StateOK)
		r["state"].(map[string]string)[harness.StateConversation] = conv
		b, _ := json.Marshal(r)
		return b
	}
	h := &hookStore{path: hookStorePath(togoHome, id)}
	at := time.Now().Add(-time.Minute)
	for i, conv := range []string{"A", "A", "B", "B"} {
		if err := h.add(hookLine{Feed: "events", At: at.Add(time.Duration(i) * time.Second), Data: rec(conv, i)}); err != nil {
			t.Fatal(err)
		}
	}
	var notes atomic.Int32
	e := storeEngine(t, home, togoHome, &notes, id)
	// The hook bus is the process's: its backlog for this session would
	// hand this run's records to the next run of the test (-count), whose
	// feed would then move to C before its own records came.
	t.Cleanup(func() { e.Forget(id) })
	res, err := e.Open(ctx, &fakeSub{done: make(chan struct{})}, id, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(itemView(res.Items), " ")
	if strings.Contains(got, "t000") || strings.Contains(got, "t001") || !strings.Contains(got, "t002") || !strings.Contains(got, "t003") {
		t.Fatalf("after the restart the chat holds %s, want only the second conversation (t002, t003)", got)
	}
	e.mu.Lock()
	s := e.sessions[id]
	e.mu.Unlock()
	s.mu.Lock()
	conv := s.conv
	s.mu.Unlock()
	if conv != "B" {
		t.Fatalf("the chat names conversation %q, want B", conv)
	}
	kept := (&hookStore{path: h.path}).load(time.Now())
	if len(kept) != 2 || !strings.Contains(string(kept[0].Data), "t002") {
		t.Fatalf("the store keeps %d records, want the second conversation's 2", len(kept))
	}
	// The same conversation again changes nothing; another one starts over.
	epoch := e.log.Epoch(id)
	deliver(id, json.RawMessage(rec("B", 4)))
	waitFor(t, func() bool { _, ok := e.log.Get(id, fmt.Sprintf("t%03d", 4)); return ok })
	if e.log.Epoch(id) != epoch {
		t.Fatal("the same conversation started the chat over")
	}
	deliver(id, json.RawMessage(rec("C", 5)))
	waitFor(t, func() bool { _, ok := e.log.Get(id, "t005"); return ok && e.log.Epoch(id) != epoch })
	if _, ok := e.log.Get(id, "t003"); ok {
		t.Fatal("a later move kept the conversation left")
	}
}
