package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	var req struct {
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(bodies[0], &req); err != nil {
		t.Fatal(err)
	}
	keys, err := push.LoadOrCreateHostKeys(env["TOGO_HOME"])
	if err != nil {
		t.Fatal(err)
	}
	plain, err := seal.Open(privA, req.Ciphertext, seal.LabelPush, []byte(keys.HostID+"|dev_a"))
	if err != nil {
		t.Fatal(err)
	}
	var p wire.PushPlaintext
	if err := json.Unmarshal(plain, &p); err != nil || p.Kind != wire.PushNotify {
		t.Fatalf("plaintext %s, %v", plain, err)
	}
}
