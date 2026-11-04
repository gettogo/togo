package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/push"
	"github.com/gettogo/togo/helper/internal/wire"
)

type recordPub struct{ kinds []string }

func (r *recordPub) Publish(kind string, _ *string, payload any) {
	if n, ok := payload.(wire.NotifyEvent); ok {
		kind += ":" + n.Level
	}
	r.kinds = append(r.kinds, kind)
}

func TestDoneNotifyDedupedPerTurn(t *testing.T) {
	rec := &recordPub{}
	now := time.Unix(1000, 0)
	d := newDoneDedupe(rec, func() time.Time { return now })
	a, b := "s_aaaaaaaaaa", "s_bbbbbbbbbb"
	done := wire.NotifyEvent{Title: "Claude finished", Level: wire.LevelDone}

	d.Publish(wire.EventNotify, &a, done)
	d.Publish(wire.EventNotify, &a, done) // idle timer after the Stop hook
	d.Publish(wire.EventNotify, &b, done)
	d.Publish(wire.EventNotify, &a, wire.NotifyEvent{Level: wire.LevelNeedsYou})
	now = now.Add(doneWindow)
	d.Publish(wire.EventNotify, &a, done)

	want := "notify:done notify:done notify:needs_you notify:done"
	if got := strings.Join(rec.kinds, " "); got != want {
		t.Fatalf("published %q, want %q", got, want)
	}
}

func TestMailboxDevicesMarkDeadAndDropUnpaired(t *testing.T) {
	home := t.TempDir()
	c := &Components{Push: &push.Service{Home: home}, logf: t.Logf}
	key := func() string {
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(pub)
	}
	devs := c.Push.Devices()
	for _, id := range []string{"live", "dead", "gone"} {
		err := devs.Put(wire.Device{PairParams: wire.PairParams{
			DeviceID: id, ApprovalKey: key(), APNsToken: strings.Repeat("ab", 32), RelayURL: "https://relay.example",
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := devs.MarkDead("dead", strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	if err := devs.Remove("gone"); err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, d := range c.mailboxDevices() {
		if len(d.ApprovalKey) != ed25519.PublicKeySize {
			t.Fatalf("device %s approval key not decoded", d.DeviceID)
		}
		got[d.DeviceID] = d.Dead
	}
	if len(got) != 2 || got["live"] || !got["dead"] {
		t.Fatalf("mailbox devices (id -> dead) = %v, want live and dead only", got)
	}
}
