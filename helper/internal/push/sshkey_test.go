package push

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

const testSSHKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBERERERERERERERERERERERERERERERERERERERERER"

func TestPairStoresSSHKey(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.SSHKey = testSSHKey })
	e.pairWith(t, "dev_b", nil)

	raw, err := os.ReadFile(filepath.Join(e.svc.Home, keysDir, devicesFile))
	if err != nil {
		t.Fatal(err)
	}
	var stored devicesJSON
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Devices) != 2 {
		t.Fatalf("stored %d devices", len(stored.Devices))
	}
	if got := stored.Devices[0].SSHKey; got != testSSHKey {
		t.Errorf("dev_a sshKey %q, want %q", got, testSSHKey)
	}
	if got := stored.Devices[1].SSHKey; got != "" {
		t.Errorf("dev_b sshKey %q, want none", got)
	}
	var fields []map[string]any
	var outer struct {
		Devices *[]map[string]any `json:"devices"`
	}
	outer.Devices = &fields
	if err := json.Unmarshal(raw, &outer); err != nil {
		t.Fatal(err)
	}
	if fields[0]["sshKey"] != testSSHKey {
		t.Errorf("devices.json lacks sshKey: %s", raw)
	}
	if _, ok := fields[1]["sshKey"]; ok {
		t.Errorf("a device without a key stores sshKey: %s", raw)
	}

	list, err := e.svc.Devices().List()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].SSHKey != testSSHKey {
		t.Errorf("List sshKey %q", list[0].SSHKey)
	}
}

func TestPairRejectsBadSSHKey(t *testing.T) {
	e := newEnv(t, 0)
	_, pub, _ := seal.GenerateX25519()
	for _, key := range []string{
		"ssh-ed25519",
		testSSHKey + " togo@phone",
		"restrict " + testSSHKey,
		"ssh-dss AAAAB3NzaC1kc3MAAACBAP",
		"ssh-ed25519 AAAA\nssh-ed25519 AAAA",
	} {
		p := pairParams("dev_a", b64.EncodeToString(pub[:]), "https://relay.example.test")
		p.SSHKey = key
		_, err := e.call(wire.MethodPushPair, p)
		var inv *session.InvalidParamsError
		if !errors.As(err, &inv) {
			t.Errorf("%q: err %v, want invalid params", key, err)
		}
	}
}
