package push

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

// APNs said 410 for an old token, then the phone registered a new one
// through the mailbox: the device lives again (it used to stay dead until
// paired again, with pushes and relay answers dropped silently).
func TestNewTokenRevivesADeadDevice(t *testing.T) {
	d := &Devices{Home: t.TempDir()}
	old, fresh := strings.Repeat("ab", 32), strings.Repeat("cd", 32)
	if err := d.Put(wire.Device{PairParams: wire.PairParams{DeviceID: "dev_a", APNsToken: old}}); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkDead("dev_a", old); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateTokens("dev_a", old, "", "", false, 1); err != nil {
		t.Fatal(err)
	}
	if l, _ := d.List(); !l[0].Dead {
		t.Fatal("the same dead token revived the device")
	}
	if err := d.UpdateTokens("dev_a", fresh, "", "", false, 2); err != nil {
		t.Fatal(err)
	}
	if l, _ := d.List(); l[0].Dead || l[0].APNsToken != fresh {
		t.Fatalf("device %+v, want alive with the new token", l[0])
	}
}
