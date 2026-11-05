package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/daemon/daemontest"
	"github.com/gettogo/togo/helper/internal/push"
	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

const question = "Run make deploy?"

// vectorDeviceKey is the device key of protocol/fixtures/crypto/seal.json,
// bytes 0x40..0x5f, which the app's tests open envelopes with too.
func vectorDeviceKey() (priv, pub [32]byte) {
	for i := range priv {
		priv[i] = byte(0x40 + i)
	}
	pub, _ = seal.PublicKey(priv)
	return priv, pub
}

func TestE2ELocalSealOpensInSwiftFormat(t *testing.T) {
	priv, pub := vectorDeviceKey()
	if got := hex.EncodeToString(pub[:]); got != "79a631eede1bf9c98f12032cdeadd0e7a079398fc786b88cc846ec89af85a51a" {
		t.Fatalf("device key does not match the crypto vector: %s", got)
	}
	dir := t.TempDir()
	device := filepath.Join(dir, "e2e-pair.json")
	params := wire.PairParams{DeviceID: "d_vectorvectorvectorvectorve", X25519: base64.RawURLEncoding.EncodeToString(pub[:])}
	raw, _ := json.Marshal(params)
	if err := os.WriteFile(device, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "push.apns")
	hostID := "h_abcdefghijklmnopqrstuvwxyz"
	if err := runSeal([]string{"-device", device, "-host-id", hostID, "-out", out, "-request-id", "r_sealtest01"}); err != nil {
		t.Fatal(err)
	}

	var apns struct {
		APS struct {
			Alert struct {
				Title string `json:"title"`
				Body  string `json:"body"`
			} `json:"alert"`
			MutableContent int    `json:"mutable-content"`
			Sound          string `json:"sound"`
		} `json:"aps"`
		C string `json:"c"`
	}
	file, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(file, &apns); err != nil {
		t.Fatal(err)
	}
	// The relay's generic alert, which the notification service replaces.
	if apns.APS.MutableContent != 1 || apns.APS.Alert.Title != "Togo" || apns.APS.Alert.Body != "Needs you" || apns.C == "" {
		t.Fatalf("apns payload is not the relay's alert shape: %+v", apns.APS)
	}
	if bytes.Contains(file, []byte("deploy")) {
		t.Fatal("the .apns file carries the question in the clear")
	}
	if _, err := seal.Open(priv, apns.C, seal.LabelPush, []byte(hostID+"|other")); err == nil {
		t.Fatal("envelope opened with another device's aad")
	}
	plain, err := seal.Open(priv, apns.C, seal.LabelPush, []byte(hostID+"|"+params.DeviceID))
	if err != nil {
		t.Fatalf("envelope does not open with the vector key: %v", err)
	}
	// The keys PushPayload decodes on the phone.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(plain, &fields); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"v", "kind", "sessionId", "bib", "agent", "title", "body", "requestId", "options", "ts"} {
		if _, ok := fields[k]; !ok {
			t.Fatalf("plaintext lacks %q", k)
		}
	}
	var p wire.PushPlaintext
	if err := json.Unmarshal(plain, &p); err != nil {
		t.Fatal(err)
	}
	if p.V != 1 || p.Kind != wire.PushNeedsYou || p.Title != question || p.RequestID != "r_sealtest01" ||
		len(p.Options) != 2 || p.Options[0] != (wire.Option{N: 1, Label: "Approve", Role: wire.RoleApprove}) ||
		p.Options[1].Role != wire.RoleDeny {
		t.Fatalf("plaintext %+v", p)
	}
	if age := time.Since(time.UnixMilli(p.TS)); age < 0 || age > time.Minute {
		t.Fatalf("plaintext ts is %s old; the phone drops pushes older than 600 s", age)
	}
}

// phoneKeys are the test device's keys, generated for the run.
type phoneKeys struct {
	params wire.PairParams
	x25519 [32]byte
	signer ed25519.PrivateKey
}

func newPhone(t *testing.T, relay string) phoneKeys {
	t.Helper()
	xPriv, xPub, err := seal.GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	token := make([]byte, 32)
	rand.Read(token)
	b64 := base64.RawURLEncoding.EncodeToString
	return phoneKeys{
		params: wire.PairParams{
			DeviceID: "d_e2etestphone", Name: "E2E iPhone", X25519: b64(xPub[:]), ApprovalKey: b64(edPub),
			APNsToken: hex.EncodeToString(token), Env: wire.EnvSandbox, RelayURL: relay, Platform: wire.PlatformIOS,
		},
		x25519: xPriv,
		signer: edPriv,
	}
}

// answer is what the phone posts to the mailbox for a lock-screen action.
func (k phoneKeys) answer(t *testing.T, res wire.PairResult, requestID, decision string) []byte {
	t.Helper()
	ts := time.Now().UnixMilli()
	sig := ed25519.Sign(k.signer, wire.ApprovalSignedBytes(res.HostID, requestID, decision, ts))
	plain, _ := json.Marshal(wire.AnswerPlaintext{
		RequestID: requestID, Decision: decision, TS: ts, DeviceID: k.params.DeviceID,
		Sig: base64.RawURLEncoding.EncodeToString(sig),
	})
	var hostKey [32]byte
	key, err := base64.RawURLEncoding.DecodeString(res.HostKey)
	if err != nil || len(key) != 32 {
		t.Fatalf("host key unreadable")
	}
	copy(hostKey[:], key)
	ct, err := seal.Seal(hostKey, plain, seal.LabelAnswer, []byte(res.HostID))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"ciphertext": ct})
	return body
}

// isolated is a helper environment of its own: TOGO_HOME, a private tmux
// label and socket directory, plain http to the local relay, and the tap.
func isolated(t *testing.T) (env []string, home, tap string) {
	t.Helper()
	tmuxDir, err := os.MkdirTemp("/tmp", "togo-pe2e-")
	if err != nil {
		t.Fatal(err)
	}
	home, err = os.MkdirTemp("", "tg")
	if err != nil {
		t.Fatal(err)
	}
	label := fmt.Sprintf("togo-pe2e-%d", os.Getpid())
	tap = filepath.Join(tmuxDir, "tap.jsonl")
	env = append(os.Environ(),
		"TOGO_HOME="+home, "TOGO_TMUX_LABEL="+label, "TMUX=", "TMUX_TMPDIR="+tmuxDir,
		push.InsecureRelayEnv+"=1", push.TapEnv+"="+tap,
	)
	t.Cleanup(func() {
		daemontest.Stop(t, home)
		kill := exec.Command("tmux", "-L", label, "kill-server")
		kill.Env = env
		kill.Run()
		os.RemoveAll(tmuxDir)
		os.RemoveAll(home)
	})
	return env, home, tap
}

func buildTogo(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "togo")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/togo").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func TestE2EDeskFirstThenLockScreenApprove(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	togo := buildTogo(t)
	env, home, tap := isolated(t)
	relay := newLocalRelay("", []string{question, "make deploy", "Approve"})
	srv := httptest.NewServer(relay)
	defer srv.Close()
	phone := newPhone(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := pairDevice(ctx, togo, env, phone.params)
	if err != nil {
		t.Fatal(err)
	}
	watcher, err := dial(ctx, togo, env)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.close()
	if err := watcher.subscribe(); err != nil {
		t.Fatal(err)
	}
	desk, err := dial(ctx, togo, env)
	if err != nil {
		t.Fatal(err)
	}
	defer desk.close()
	if err := desk.subscribe(); err != nil {
		t.Fatal(err)
	}

	ask, err := startAsk(ctx, togo, env, []string{"ask", question, "--options", "Approve,Deny", "--timeout", "60", "--require-remote"})
	if err != nil {
		t.Fatal(err)
	}
	open, err := watcher.next(wire.EventApprovalOpen, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	id := open.Approval.RequestID

	// The phone gets the request sealed for it, as the notification service opens it.
	var alert pushed
	if !relay.waitFor(15*time.Second, func() bool {
		a := relay.pushesOf("alert")
		if len(a) > 0 {
			alert = a[0]
		}
		return len(a) > 0
	}) {
		t.Fatal("no alert push reached the relay")
	}
	p := openPush(t, phone, res, alert.Ciphertext)
	if p.Kind != wire.PushNeedsYou || p.RequestID != id || p.Title != question || len(p.Options) != 2 {
		t.Fatalf("needs_you plaintext %+v", p)
	}
	// The roles the lock screen offers Approve and Deny for.
	if p.Options[0].Role != wire.RoleApprove || p.Options[1].Role != wire.RoleDeny {
		t.Fatalf("ask options carry roles %q and %q", p.Options[0].Role, p.Options[1].Role)
	}

	// The desk answers first.
	var answered map[string]string
	if err := desk.call(wire.MethodApprovalAnswer, map[string]string{"requestId": id, "decision": "approve"}, &answered); err != nil {
		t.Fatal(err)
	}
	code, out := ask.wait()
	if code != 0 || out != "Approve" {
		t.Fatalf("ask exited %d printing %q, want 0 and Approve", code, out)
	}
	var clear pushed
	if !relay.waitFor(15*time.Second, func() bool {
		c := relay.pushesOf("background")
		if len(c) > 0 {
			clear = c[0]
		}
		return len(c) > 0
	}) {
		t.Fatal("no clear push after the desk answered")
	}
	if c := openPush(t, phone, res, clear.Ciphertext); c.Kind != wire.PushClear || c.RequestID != id {
		t.Fatalf("clear plaintext %+v", c)
	}

	// The lock-screen Deny arrives through the mailbox after the desk answer: the helper
	// takes it off the mailbox and applies nothing.
	resp, err := http.Post(srv.URL+"/v1/mailbox/"+res.HostID, "application/json", bytes.NewReader(phone.answer(t, res, id, "deny")))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("mailbox post: %d", resp.StatusCode)
	}
	if !relay.waitFor(20*time.Second, func() bool { return relay.acked == 1 }) {
		t.Fatal("the helper never took the late answer off the mailbox")
	}
	var list struct {
		Approvals []wire.Approval `json:"approvals"`
	}
	if err := desk.call(wire.MethodApprovalList, map[string]any{}, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Approvals) != 0 {
		t.Fatalf("approvals still open: %d", len(list.Approvals))
	}
	log, _ := os.ReadFile(filepath.Join(home, "log", "daemon.log"))
	handled := false
	for _, line := range strings.Split(string(log), "\n") {
		if strings.Contains(line, "mailbox: answer") && strings.HasSuffix(line, " applied") && !strings.HasSuffix(line, "not applied") {
			t.Fatalf("the late lock-screen answer was applied: %s", line)
		}
		handled = handled || strings.Contains(line, "mailbox: answer") || strings.Contains(line, "mailbox: dropped")
	}
	if !handled {
		t.Fatal("daemon log does not show the late answer handled")
	}
	relay.mu.Lock()
	n, leaked := len(relay.pushes), relay.plaintext
	relay.mu.Unlock()
	if len(leaked) > 0 {
		t.Fatalf("relay bodies carried %q", leaked)
	}
	if n != 2 {
		t.Fatalf("relay got %d pushes, want the request and its clear", n)
	}
	assertTapSealed(t, tap, "deploy", "make", "approve")
}

func openPush(t *testing.T, k phoneKeys, res wire.PairResult, ct string) wire.PushPlaintext {
	t.Helper()
	plain, err := seal.Open(k.x25519, ct, seal.LabelPush, []byte(res.HostID+"|"+k.params.DeviceID))
	if err != nil {
		t.Fatalf("push does not open for the device: %v", err)
	}
	var p wire.PushPlaintext
	if err := json.Unmarshal(plain, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// assertTapSealed checks the helper's relay tap: every request it made has
// a record, and none carries the words in the clear.
func assertTapSealed(t *testing.T, tap string, words ...string) {
	t.Helper()
	raw, err := os.ReadFile(tap)
	if err != nil {
		t.Fatalf("no relay tap: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for _, line := range lines {
		var r push.TapRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		if r.Status != http.StatusAccepted {
			t.Fatalf("tap record status %d", r.Status)
		}
		for _, w := range words {
			if slices.Contains(r.Words, push.WordHash(w)) {
				t.Fatalf("a %s request body carried %q", r.Kind, w)
			}
		}
	}
}
