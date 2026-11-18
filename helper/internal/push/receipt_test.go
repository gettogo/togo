package push

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

func (e *env) openPush(t *testing.T, p relayPush) wire.PushPlaintext {
	t.Helper()
	keys, err := e.svc.HostKeys()
	if err != nil {
		t.Fatal(err)
	}
	for _, ph := range e.phones {
		pt, err := seal.Open(ph.priv, p.Ciphertext, seal.LabelPush, []byte(keys.HostID+"|"+ph.id))
		if err != nil {
			continue
		}
		var out wire.PushPlaintext
		if err := json.Unmarshal(pt, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Fatal("no phone opens the push")
	return wire.PushPlaintext{}
}

// The device that answered, and no other, learns what became of its answer.
func TestAnswerReceiptGoesToTheDeviceThatAnswered(t *testing.T) {
	e := newEnv(t, 1)
	e.pairWith(t, "dev_r", func(p *wire.PairParams) { p.AnswerReceipts = true })
	ctx := context.Background()
	e.sender.AnswerReceipt(ctx, "dev_a", "r_abcdefghij", wire.ConflictExpired)
	if n := len(e.relay.received()); n != 0 {
		t.Fatalf("%d pushes to a device that did not ask for receipts", n)
	}
	e.sender.AnswerReceipt(ctx, "dev_r", "r_abcdefghij", wire.ConflictExpired)
	e.sender.AnswerReceipt(ctx, "dev_r", "r_abcdefghij", wire.AnswerApplied)
	got := e.relay.received()
	if len(got) != 2 {
		t.Fatalf("got %d pushes, want 2", len(got))
	}
	if got[0].Kind != relayAlert || got[1].Kind != relayBackground {
		t.Fatalf("kinds = %s, %s; want an alert for expired and a background push for applied", got[0].Kind, got[1].Kind)
	}
	for i, want := range []string{wire.ConflictExpired, wire.AnswerApplied} {
		if who := e.opener(t, got[i].Ciphertext); who != "dev_r" {
			t.Fatalf("push %d opens for %s", i, who)
		}
		p := e.openPush(t, got[i])
		if p.Kind != wire.PushAnswer || p.RequestID != "r_abcdefghij" || p.Result != want {
			t.Fatalf("push %d = %+v, want answer %s", i, p, want)
		}
	}
}
