package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/push"
	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

const testToken = "abababababababababababababababababababababababababababababababab"

func pairTestDevice(t *testing.T, env map[string]string, id, relay string) [32]byte {
	t.Helper()
	svc, err := pushService(env)
	if err != nil {
		t.Fatal(err)
	}
	priv, pub, err := seal.GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(wire.PairParams{
		DeviceID: id, Name: "Desk iPhone", X25519: b64(pub[:]),
		ApprovalKey: "ERERERERERERERERERERERERERERERERERERERERERE",
		APNsToken:   testToken, Env: wire.EnvSandbox, RelayURL: relay,
	})
	if _, err := svc.Methods()[wire.MethodPushPair](context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	return priv
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func TestDevicesLsHidesToken(t *testing.T) {
	env := map[string]string{"TOGO_HOME": t.TempDir()}
	pairTestDevice(t, env, "dev_test-1", "https://relay.example.test")
	var out, errOut bytes.Buffer
	if code := run([]string{"devices", "ls"}, env, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"Desk iPhone", "dev_test-1", "sandbox", "active"} {
		if !strings.Contains(got, want) {
			t.Fatalf("devices ls missing %q:\n%s", want, got)
		}
	}
	for _, secret := range []string{testToken[:16], "ERERERER"} {
		if strings.Contains(got, secret) {
			t.Fatalf("devices ls prints a secret:\n%s", got)
		}
	}
}

func TestDevicesLsShowsPlatformAndLiveActivity(t *testing.T) {
	env := map[string]string{"TOGO_HOME": t.TempDir()}
	svc, err := pushService(env)
	if err != nil {
		t.Fatal(err)
	}
	_, pub, _ := seal.GenerateX25519()
	start := strings.Repeat("ef", 32)
	for _, p := range []wire.PairParams{
		{DeviceID: "d_phone", Name: "Desk iPhone", LiveActivityStartToken: start, LiveActivityCommand: true},
		{DeviceID: "w_watch", Name: "Wrist", Platform: wire.PlatformWatch},
	} {
		p.X25519, p.ApprovalKey = b64(pub[:]), "ERERERERERERERERERERERERERERERERERERERERERE"
		p.APNsToken, p.Env, p.RelayURL = testToken, wire.EnvSandbox, "https://relay.example.test"
		raw, _ := json.Marshal(p)
		if _, err := svc.Methods()[wire.MethodPushPair](context.Background(), raw); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"devices", "ls"}, env, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "PLATFORM") || !strings.Contains(lines[0], "LIVE") || !strings.Contains(lines[0], "COMMAND") {
		t.Fatalf("devices ls:\n%s", out.String())
	}
	if f := strings.Fields(lines[1]); !strings.Contains(lines[1], "d_phone") || !strings.Contains(lines[1], " ios ") ||
		f[len(f)-4] != "on" || f[len(f)-3] != "on" {
		t.Fatalf("phone row %q", lines[1])
	}
	if f := strings.Fields(lines[2]); !strings.Contains(lines[2], "watchos") || f[len(f)-4] != "-" || f[len(f)-3] != "-" {
		t.Fatalf("watch row %q", lines[2])
	}
	if strings.Contains(out.String(), start[:16]) {
		t.Fatalf("devices ls prints a Live Activity token:\n%s", out.String())
	}
}

func TestDevicesRevokeUnpairs(t *testing.T) {
	env := map[string]string{"TOGO_HOME": t.TempDir()}
	pairTestDevice(t, env, "dev_test-1", "https://relay.example.test")
	var out, errOut bytes.Buffer
	if code := run([]string{"devices", "revoke", "dev_test-1"}, env, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	out.Reset()
	run([]string{"devices", "ls"}, env, &out, &errOut)
	if !strings.Contains(out.String(), "no paired devices") {
		t.Fatalf("after revoke: %s", out.String())
	}
	if code := run([]string{"devices", "revoke", "dev_test-1"}, env, &out, &errOut); code != exitError {
		t.Fatalf("revoking an unknown device exited %d", code)
	}
}

func TestPushTestSealsNotifyToEveryDevice(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, b)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer relay.Close()
	env := map[string]string{"TOGO_HOME": t.TempDir(), push.InsecureRelayEnv: "1"}
	privA := pairTestDevice(t, env, "dev_a", relay.URL)
	pairTestDevice(t, env, "dev_b", relay.URL)

	var out, errOut bytes.Buffer
	if code := run([]string{"push", "test"}, env, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s %s", code, out.String(), errOut.String())
	}
	if len(bodies) != 2 {
		t.Fatalf("relay got %d pushes, want 2", len(bodies))
	}
	keys, err := push.LoadOrCreateHostKeys(env["TOGO_HOME"])
	if err != nil {
		t.Fatal(err)
	}
	// Devices are sent to side by side: dev_a's push is whichever opens.
	var plain []byte
	for _, b := range bodies {
		var req struct {
			Ciphertext string `json:"ciphertext"`
		}
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		if p, err := seal.Open(privA, req.Ciphertext, seal.LabelPush, []byte(keys.HostID+"|dev_a")); err == nil {
			plain = p
		}
	}
	if plain == nil {
		t.Fatal("no push opens for dev_a")
	}
	var p wire.PushPlaintext
	if err := json.Unmarshal(plain, &p); err != nil || p.Kind != wire.PushNotify {
		t.Fatalf("plaintext %s, %v", plain, err)
	}
}

const revokeKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBERERERERERERERERERERERERERERERERERERERERER"

// revokeEnv is a temp TOGO_HOME and a temp HOME whose ~/.ssh/authorized_keys
// holds keys, or no ~/.ssh at all when keys is empty.
func revokeEnv(t *testing.T, keys string) (map[string]string, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, ".ssh", "authorized_keys")
	if keys != "" {
		if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(keys), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return map[string]string{"TOGO_HOME": t.TempDir(), "HOME": home}, path
}

func pairWithKey(t *testing.T, env map[string]string, id, name, key string) {
	t.Helper()
	svc, err := pushService(env)
	if err != nil {
		t.Fatal(err)
	}
	_, pub, _ := seal.GenerateX25519()
	raw, _ := json.Marshal(wire.PairParams{
		DeviceID: id, Name: name, X25519: b64(pub[:]),
		ApprovalKey: "ERERERERERERERERERERERERERERERERERERERERERE",
		APNsToken:   testToken, Env: wire.EnvSandbox, RelayURL: "https://relay.example.test",
		SSHKey: key,
	})
	if _, err := svc.Methods()[wire.MethodPushPair](context.Background(), raw); err != nil {
		t.Fatal(err)
	}
}

func assertUnpaired(t *testing.T, env map[string]string) {
	t.Helper()
	svc, _ := pushService(env)
	list, err := svc.Devices().List()
	if err != nil || len(list) != 0 {
		t.Fatalf("after revoke: %d devices, %v", len(list), err)
	}
}

func TestRevokeRemovesKey(t *testing.T) {
	other := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIi me@laptop\n"
	env, path := revokeEnv(t, other+revokeKey+" togo@Desk iPhone\n")
	pairWithKey(t, env, "dev_a", "Desk iPhone", revokeKey)
	var out, errOut bytes.Buffer
	if code := run([]string{"devices", "revoke", "dev_a"}, env, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	assertUnpaired(t, env)
	if got, _ := os.ReadFile(path); string(got) != other {
		t.Errorf("authorized_keys %q, want %q", got, other)
	}
	if !strings.Contains(out.String(), "Removed this device's key from ~/.ssh/authorized_keys.\n") {
		t.Errorf("output %q", out.String())
	}

	// A second phone whose key is not in the file.
	pairWithKey(t, env, "dev_b", "Spare", revokeKey)
	out.Reset()
	if code := run([]string{"devices", "revoke", "dev_b"}, env, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "No key for this device was found in ~/.ssh/authorized_keys.\n") {
		t.Errorf("output %q", out.String())
	}
}

func TestRevokeWithoutKeyStillUnpairs(t *testing.T) {
	line := revokeKey + " togo@Desk iPhone\n"
	env, path := revokeEnv(t, line)
	pairWithKey(t, env, "dev_a", "Desk iPhone", "")
	var out, errOut bytes.Buffer
	if code := run([]string{"devices", "revoke", "dev_a"}, env, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	assertUnpaired(t, env)
	if got, _ := os.ReadFile(path); string(got) != line {
		t.Errorf("authorized_keys changed: %q", got)
	}
	if strings.Contains(out.String(), "authorized_keys") {
		t.Errorf("output mentions authorized_keys for a device without a key: %q", out.String())
	}
}

func TestRevokeConflictStillUnpairs(t *testing.T) {
	line := revokeKey + " togo@Desk\x1b[31m iPhone\n"
	target, _ := revokeEnv(t, line)
	env, path := revokeEnv(t, "")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(target["HOME"], ".ssh", "authorized_keys")
	if err := os.Symlink(targetPath, path); err != nil {
		t.Fatal(err)
	}
	pairWithKey(t, env, "dev_a", "Desk\x1b[31m iPhone", revokeKey)
	var out, errOut bytes.Buffer
	if code := run([]string{"devices", "revoke", "dev_a"}, env, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	assertUnpaired(t, env)
	if got, _ := os.ReadFile(targetPath); string(got) != line {
		t.Errorf("link target changed: %q", got)
	}
	want := "~/.ssh/authorized_keys is a link; remove the line containing ssh-ed25519 …ERERERERERERERER (comment starts with togo@) by hand.\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("output %q, want %q", out.String(), want)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Errorf("output carries the device name's escape: %q", out.String())
	}

	// No ~/.ssh at all.
	env, _ = revokeEnv(t, "")
	pairWithKey(t, env, "dev_a", "Desk iPhone", revokeKey)
	out.Reset()
	if code := run([]string{"devices", "revoke", "dev_a"}, env, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	assertUnpaired(t, env)
	if !strings.Contains(out.String(), "~/.ssh/authorized_keys does not exist") {
		t.Errorf("output %q", out.String())
	}
}
