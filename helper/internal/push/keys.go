// Package push pairs phones with this host and sends them sealed
// notifications through the relay. It owns the host key file and the device
// list under $TOGO_HOME/keys, the push.pair and push.unpair methods, and the
// Sender that turns hub events into relay pushes. Plaintexts are sealed per
// device before they leave the process; the relay sees ciphertext, tokens and
// Live Activity metadata only.
package push

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/state"
)

const (
	keysDir      = "keys"
	hostKeysFile = "host.json"
	devicesFile  = "devices.json"
	keyFileV     = 1
)

var b64 = base64.RawURLEncoding

// HostKeys are this host's long-lived secrets. MailboxSecret authorises the
// relay mailbox and HostID is derived from it; X25519Priv opens answers the
// phone seals for the host. None of these may be logged.
type HostKeys struct {
	HostID        string
	MailboxSecret [32]byte
	X25519Priv    [32]byte
	X25519Pub     [32]byte
}

// HostKey returns the public X25519 key in its wire form.
func (k HostKeys) HostKey() string { return b64.EncodeToString(k.X25519Pub[:]) }

// String keeps the secrets out of any accidental %v.
func (k HostKeys) String() string { return "HostKeys{" + k.HostID + "}" }

// GoString keeps the secrets out of %#v as well.
func (k HostKeys) GoString() string { return k.String() }

type hostKeysJSON struct {
	V             int    `json:"v"`
	MailboxSecret string `json:"mailboxSecret"`
	X25519        string `json:"x25519"`
}

// LoadOrCreateHostKeys reads $home/keys/host.json, creating it from
// crypto/rand on first use.
func LoadOrCreateHostKeys(home string) (HostKeys, error) {
	return loadOrCreateHostKeys(home, rand.Reader)
}

// loadOrCreateHostKeys draws the mailbox secret and then the X25519 private
// key from r, in that order, so a fixed reader gives fixed keys in tests.
func loadOrCreateHostKeys(home string, r io.Reader) (HostKeys, error) {
	dir, err := keysPath(home)
	if err != nil {
		return HostKeys{}, err
	}
	path := filepath.Join(dir, hostKeysFile)
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		return parseHostKeys(raw)
	case !errors.Is(err, os.ErrNotExist):
		return HostKeys{}, fmt.Errorf("host keys: %w", err)
	}
	var k HostKeys
	if _, err := io.ReadFull(r, k.MailboxSecret[:]); err != nil {
		return HostKeys{}, fmt.Errorf("host keys: random: %w", err)
	}
	if _, err := io.ReadFull(r, k.X25519Priv[:]); err != nil {
		return HostKeys{}, fmt.Errorf("host keys: random: %w", err)
	}
	if err := k.derive(); err != nil {
		return HostKeys{}, err
	}
	data, err := json.Marshal(hostKeysJSON{
		V:             keyFileV,
		MailboxSecret: b64.EncodeToString(k.MailboxSecret[:]),
		X25519:        b64.EncodeToString(k.X25519Priv[:]),
	})
	if err != nil {
		return HostKeys{}, err
	}
	if err := writeExclusive(path, data); err != nil {
		if errors.Is(err, os.ErrExist) {
			// Another process created the file first; its keys win so the
			// host id never changes under a paired phone.
			raw, err := os.ReadFile(path)
			if err != nil {
				return HostKeys{}, fmt.Errorf("host keys: %w", err)
			}
			return parseHostKeys(raw)
		}
		return HostKeys{}, err
	}
	return k, nil
}

func parseHostKeys(raw []byte) (HostKeys, error) {
	var j hostKeysJSON
	if err := json.Unmarshal(raw, &j); err != nil || j.V != keyFileV {
		return HostKeys{}, errors.New("host keys: unreadable keys/host.json")
	}
	var k HostKeys
	if !decode32(j.MailboxSecret, &k.MailboxSecret) || !decode32(j.X25519, &k.X25519Priv) {
		return HostKeys{}, errors.New("host keys: unreadable keys/host.json")
	}
	if err := k.derive(); err != nil {
		return HostKeys{}, err
	}
	return k, nil
}

func (k *HostKeys) derive() error {
	pub, err := seal.PublicKey(k.X25519Priv)
	if err != nil {
		return errors.New("host keys: unusable X25519 key")
	}
	k.X25519Pub = pub
	k.HostID = seal.HostID(k.MailboxSecret[:])
	return nil
}

func decode32(s string, out *[32]byte) bool {
	b, err := b64.Strict().DecodeString(s)
	if err != nil || len(b) != 32 {
		return false
	}
	copy(out[:], b)
	return true
}

// keysPath returns $home/keys, created 0700 and tightened if it exists.
func keysPath(home string) (string, error) {
	if home == "" {
		return "", errors.New("push: TOGO_HOME is not set")
	}
	dir := filepath.Join(home, keysDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("keys dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("keys dir: %w", err)
	}
	return dir, nil
}

// writeExclusive writes data to a temporary file and links it into place,
// which fails with ErrExist rather than replacing a key file another process
// created in the meantime. Readers never see a partial file.
func writeExclusive(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".new*")
	if err != nil {
		return fmt.Errorf("host keys: %w", err)
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)
	if err := state.WriteFile(tmp, data); err != nil {
		return err
	}
	if err := os.Link(tmp, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return err
		}
		return fmt.Errorf("host keys: %w", err)
	}
	return nil
}
