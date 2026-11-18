package push

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func (e *env) alertsTo(t *testing.T) map[string]int {
	t.Helper()
	got := map[string]int{}
	for _, p := range e.relay.received() {
		if p.Kind == relayAlert {
			got[e.opener(t, p.Ciphertext)]++
		}
	}
	return got
}

// A tablet with the app open does not silence the phone.
func TestForegroundDeviceAloneIsSkipped(t *testing.T) {
	e := newEnv(t, 2)
	e.hub.setFront("dev_a")
	e.handle(approvalOpen("r_abcdefghij"))
	got := e.alertsTo(t)
	if got["dev_a"] != 0 || got["dev_b"] != 1 {
		t.Fatalf("alerts = %v, want one to dev_b only", got)
	}
}

// An app that did not name its device silences every device, as before.
func TestUnnamedForegroundAppSilencesAll(t *testing.T) {
	e := newEnv(t, 2)
	e.hub.fg.Store(1)
	e.handle(approvalOpen("r_abcdefghij"))
	if got := e.alertsTo(t); len(got) != 0 {
		t.Fatalf("alerts = %v, want none", got)
	}
}

// Every phone and tablet in front: nothing goes out, the Watch included.
func TestAllDevicesInFrontSilenceTheWatch(t *testing.T) {
	e := newEnv(t, 1)
	e.pairWith(t, "dev_w", func(p *wire.PairParams) { p.Platform = wire.PlatformWatch })
	e.hub.setFront("dev_a")
	e.handle(approvalOpen("r_abcdefghij"))
	if n := len(e.relay.received()); n != 0 {
		t.Fatalf("%d pushes went out, want none", n)
	}
}

// The skipped device gets the push once it has left the foreground.
func TestSkippedDeviceToldAfterItLeaves(t *testing.T) {
	e := newEnv(t, 2)
	e.hub.setFront("dev_a")
	ev := approvalOpen("r_abcdefghij")
	m, ok := e.sender.message(ev, "s_aaaaaaaaaa", SessionInfo{})
	if !ok || !m.skip["dev_a"] {
		t.Fatalf("message skip = %v, ok %v", m.skip, ok)
	}
	e.sender.Remind = &Reminders{Pending: func(string) bool { return true }}
	held := m
	held.skip, held.only = nil, map[string]bool{"dev_a": true}
	e.hub.setFront()
	e.sender.releaseHeld(held)
	got := e.alertsTo(t)
	if got["dev_a"] != 1 || got["dev_b"] != 0 {
		t.Fatalf("alerts = %v, want one to dev_a only", got)
	}
}

// An app in front on a device that never paired speaks only for itself.
func TestUnpairedDeviceInFrontSilencesNobody(t *testing.T) {
	e := newEnv(t, 2)
	e.hub.setFront("dev_unpaired")
	e.handle(approvalOpen("r_abcdefghij"))
	got := e.alertsTo(t)
	if got["dev_a"] != 1 || got["dev_b"] != 1 {
		t.Fatalf("alerts = %v, want one to each paired device", got)
	}
}
