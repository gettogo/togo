package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/daemon/daemontest"
	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

// fakeAgent draws a permission menu twice, recording the single key typed
// at each one, the way an agent's arrow menu takes a digit without Enter.
const fakeAgent = `#!/bin/sh
out="$1"; prompt="$2"
for round in 1 2; do
  clear
  cat "$prompt"
  stty -icanon -echo min 1 time 0
  key=$(dd bs=1 count=1 2>/dev/null)
  stty sane
  printf '%s' "$key" >> "$out"
  clear
  echo "answered $round"
  sleep 3
done
sleep 600
`

// e2eEnv is a daemon environment on a private tmux label and a TOGO_HOME
// short enough for the unix socket path limit.
func e2eEnv(t *testing.T) (env []string, home, label string) {
	t.Helper()
	env, _, label = isolatedEnv(t)
	home, err := os.MkdirTemp("", "tg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		daemontest.Stop(t, home)
		os.RemoveAll(home)
	})
	// Later entries win in exec.Cmd.Env, so these override the inherited ones.
	env = append(env, "TOGO_HOME="+home, "TOGO_IDLE_SECONDS=1", "TOGO_TEST_RELAY_INSECURE=1", "SHELL=/bin/sh")
	return env, home, label
}

func tmuxCmd(env []string, label string, args ...string) *exec.Cmd {
	cmd := exec.Command("tmux", append([]string{"-L", label}, args...)...)
	cmd.Env = env
	return cmd
}

func waitFor(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func openApproval(t *testing.T, c *daemonClient, sessionID, notID string) approvalInfo {
	t.Helper()
	var a approvalInfo
	c.waitEvent("approval.open with options", 30*time.Second, func(ev event) bool {
		if ev.Kind != wire.EventApprovalOpen || ev.SessionID == nil || *ev.SessionID != sessionID {
			return false
		}
		if json.Unmarshal(ev.Approval, &a) != nil || a.RequestID == notID {
			return false
		}
		return len(a.Options) > 0
	})
	return a
}

// rig is a running daemon with a paired phone, a background subscriber and
// a session whose fake agent is showing its first permission menu.
type rig struct {
	bin     string
	env     []string
	label   string
	daemon  *exec.Cmd
	c       *daemonClient
	ph      *phone
	sess    sessionInfo
	keysOut string
}

func newRig(t *testing.T, relayURL string) *rig {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	bin := buildTogo(t)
	env, home, label := e2eEnv(t)
	daemonCmd := startDaemon(t, bin, env)
	c := dialDaemon(t, filepath.Join(home, "run", "togo.sock"))

	for _, want := range []string{"events.v1", "approvals.v1", "mute.v1", "push.v1", "git.v1", "ports.v1", "upload.v1"} {
		if !slices.Contains(c.caps, want) {
			t.Fatalf("hello capabilities %q lack %s", c.caps, want)
		}
	}

	ph := newPhone(t, relayURL)
	ph.pair(t, c)
	c.call("events.subscribe", map[string]any{"foreground": false}, nil)

	work := t.TempDir()
	var sess sessionInfo
	c.call("session.create", map[string]any{"agent": "shell", "cwd": work}, &sess)
	c.waitEvent("session event for the new session", 10*time.Second, func(ev event) bool {
		return ev.Kind == wire.EventSession && ev.SessionID != nil && *ev.SessionID == sess.ID
	})

	script := filepath.Join(work, "agent.sh")
	keysOut := filepath.Join(work, "keys.out")
	prompt, err := filepath.Abs("../internal/detect/testdata/claude/bash-permission.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(fakeAgent), 0o700); err != nil {
		t.Fatal(err)
	}
	target := "=" + sess.ID + ":"
	// Output drawn before the tap is attached is never seen, so the agent
	// starts once the pane is piped.
	waitFor(t, "the tap on the pane", 15*time.Second, func() bool {
		out, _ := tmuxCmd(env, label, "display-message", "-p", "-t", target, "#{pane_pipe}").Output()
		return strings.TrimSpace(string(out)) == "1"
	})
	if out, err := tmuxCmd(env, label, "send-keys", "-t", target, "sh "+script+" "+keysOut+" "+prompt, "Enter").CombinedOutput(); err != nil {
		t.Fatalf("send-keys: %v %s", err, out)
	}
	waitFor(t, "the menu on screen", 15*time.Second, func() bool {
		out, _ := tmuxCmd(env, label, "capture-pane", "-p", "-t", target).Output()
		return bytes.Contains(out, []byte("Do you want to proceed?"))
	})

	return &rig{bin: bin, env: env, label: label, daemon: daemonCmd, c: c, ph: ph, sess: sess, keysOut: keysOut}
}

// TestE2EPromptToPhoneAndMailbox drives the built daemon end to end: a
// screen prompt plus a Claude PermissionRequest hook open one approval, the
// phone is pushed a sealed needs_you, an answer over the socket types the
// approve key once and a repeat conflicts, then a signed answer through the
// relay mailbox types the deny key once even when delivered twice.
func TestE2EPromptToPhoneAndMailbox(t *testing.T) {
	relay := newFakeRelay(t)
	r := newRig(t, relay.srv.URL)
	bin, env, c, ph, sess, keysOut := r.bin, r.env, r.c, r.ph, r.sess, r.keysOut

	payload, err := os.ReadFile("../internal/adapters/claude/testdata/permission_request.json")
	if err != nil {
		t.Fatal(err)
	}
	hook := exec.Command(bin, "hook", "claude", "PermissionRequest")
	hook.Env = append(env, "TOGO_SESSION="+sess.ID)
	hook.Stdin = bytes.NewReader(payload)
	if out, err := hook.CombinedOutput(); err != nil {
		t.Fatalf("togo hook: %v %s", err, out)
	}
	first := openApproval(t, c, sess.ID, "")
	c.waitEvent("needs_you session status", 10*time.Second, func(ev event) bool {
		return ev.Kind == wire.EventSession && ev.SessionID != nil && *ev.SessionID == sess.ID && ev.Session.Status == "needs_you"
	})

	pushed := ph.waitPush(t, relay, first.RequestID)
	if pushed.Kind != wire.PushNeedsYou {
		t.Fatalf("push kind %q, want needs_you", pushed.Kind)
	}

	var ans struct {
		State string `json:"state"`
	}
	c.call("approval.answer", map[string]any{"requestId": first.RequestID, "decision": "approve"}, &ans)
	if ans.State != "answered" {
		t.Fatalf("answer state %q", ans.State)
	}
	waitClosed(t, c, first.RequestID)
	waitFor(t, "the approve key", 10*time.Second, func() bool {
		b, _ := os.ReadFile(keysOut)
		return string(b) == "1"
	})
	if _, e := c.try("approval.answer", map[string]any{"requestId": first.RequestID, "decision": "approve"}); e == nil || e.Code != -32006 {
		t.Fatalf("second answer error = %+v, want -32006", e)
	}

	// The first menu stays drawn for a moment after its key is typed, and a
	// watcher that reads that stale screen opens an approval for it, which a
	// phone answer then finds gone. Take the second approval only once the
	// agent has cleared the first menu and drawn the second.
	waitFor(t, "the first menu to clear", 15*time.Second, func() bool {
		out, _ := tmuxCmd(env, r.label, "capture-pane", "-p", "-t", "="+sess.ID+":").Output()
		return bytes.Contains(out, []byte("answered 1"))
	})
	waitFor(t, "the second menu on screen", 20*time.Second, func() bool {
		out, _ := tmuxCmd(env, r.label, "capture-pane", "-p", "-t", "="+sess.ID+":").Output()
		return bytes.Contains(out, []byte("Do you want to proceed?"))
	})
	var second approvalInfo
	waitFor(t, "an open approval for the second menu", 20*time.Second, func() bool {
		var res struct {
			Approvals []approvalInfo `json:"approvals"`
		}
		c.call("approval.list", map[string]any{"sessionId": sess.ID}, &res)
		for _, a := range res.Approvals {
			if a.RequestID != first.RequestID && len(a.Options) > 0 {
				second = a
				return true
			}
		}
		return false
	})
	frame := ph.answerFrame(t, second.RequestID, "deny")
	relay.frames <- strings.Replace(frame, `"id":"m"`, `"id":"m1"`, 1)
	relay.frames <- strings.Replace(frame, `"id":"m"`, `"id":"m2"`, 1)
	for _, want := range []string{"m1", "m2"} {
		select {
		case got := <-relay.acks:
			if got != want {
				t.Fatalf("ack %q, want %q", got, want)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("no ack for %s", want)
		}
	}
	waitClosed(t, c, second.RequestID)
	waitFor(t, "the deny key", 10*time.Second, func() bool {
		b, _ := os.ReadFile(keysOut)
		return string(b) == "1\x1b"
	})
	time.Sleep(time.Second)
	if b, _ := os.ReadFile(keysOut); string(b) != "1\x1b" {
		t.Fatalf("keys typed %q, want one approve and one deny", b)
	}

	assertNoNetworkListener(t, r.daemon.Process.Pid)

	c.call("session.kill", map[string]any{"id": sess.ID}, nil)
	c.waitEvent("session.removed", 10*time.Second, func(ev event) bool {
		return ev.Kind == wire.EventSessionRemoved && ev.SessionID != nil && *ev.SessionID == sess.ID
	})
}

func waitClosed(t *testing.T, c *daemonClient, requestID string) {
	t.Helper()
	ev := c.waitEvent("approval.closed", 15*time.Second, func(ev event) bool {
		return ev.Kind == wire.EventApprovalClosed && ev.RequestID == requestID
	})
	if ev.Resolution != wire.ResolutionPhone {
		t.Fatalf("resolution %q, want phone", ev.Resolution)
	}
}

// waitPush returns the first push for requestID, opened with the phone key.
func (p *phone) waitPush(t *testing.T, relay *fakeRelay, requestID string) wire.PushPlaintext {
	t.Helper()
	timeout := time.After(20 * time.Second)
	var priv [32]byte
	copy(priv[:], p.x25519.Bytes())
	for {
		select {
		case body := <-relay.pushes:
			ct, _ := body["ciphertext"].(string)
			if ct == "" {
				continue
			}
			pt, err := seal.Open(priv, ct, seal.LabelPush, []byte(p.hostID+"|"+p.id))
			if err != nil {
				t.Fatalf("push does not open with the device key: %v", err)
			}
			var m wire.PushPlaintext
			if err := json.Unmarshal(pt, &m); err != nil {
				t.Fatal(err)
			}
			if m.RequestID == requestID && m.Kind != wire.PushClear {
				return m
			}
		case <-timeout:
			t.Fatalf("no push for %s", requestID)
		}
	}
}

// answerFrame is a relay mailbox frame carrying a sealed, signed answer.
// Its id is "m" for the caller to replace.
func (p *phone) answerFrame(t *testing.T, requestID, decision string) string {
	t.Helper()
	ct, ts := p.sealedAnswer(t, requestID, decision)
	return fmt.Sprintf(`{"type":"msg","id":"m","ciphertext":%q,"receivedAt":%d}`, ct, ts)
}

// sealedAnswer is the ciphertext a phone posts to the host's mailbox, and
// the timestamp it signed.
func (p *phone) sealedAnswer(t *testing.T, requestID, decision string) (string, int64) {
	t.Helper()
	ts := time.Now().UnixMilli()
	sig := seal.Sign(p.sign, seal.SignMessage(p.hostID, requestID, decision, ts))
	pt, err := json.Marshal(wire.AnswerPlaintext{
		RequestID: requestID, Decision: decision, TS: ts, DeviceID: p.id,
		Sig: base64URL(sig),
	})
	if err != nil {
		t.Fatal(err)
	}
	ct, err := seal.Seal(p.hostKey, pt, seal.LabelAnswer, []byte(p.hostID))
	if err != nil {
		t.Fatal(err)
	}
	return ct, ts
}

// assertNoNetworkListener fails if pid holds a listening TCP socket or any
// UDP socket. Outbound relay connections are allowed.
func assertNoNetworkListener(t *testing.T, pid int) {
	t.Helper()
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Log("lsof not installed; listener check skipped")
		return
	}
	out, _ := exec.Command("lsof", "-nP", "-a", "-p", strconv.Itoa(pid), "-i").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "(LISTEN)") || strings.Contains(line, " UDP ") {
			t.Fatalf("daemon has a network listener: %s", line)
		}
	}
}

// TestE2EMailboxThroughRelayWorker runs against a local relay worker
// (`npx wrangler dev` in relay/) named by TOGO_E2E_RELAY_URL: the daemon
// pushes the approval to the worker, the phone posts a sealed answer to the
// worker's mailbox, and the daemon's mailbox reader types the key once.
func TestE2EMailboxThroughRelayWorker(t *testing.T) {
	relayURL := os.Getenv("TOGO_E2E_RELAY_URL")
	if relayURL == "" {
		t.Skip("TOGO_E2E_RELAY_URL not set")
	}
	r := newRig(t, relayURL)
	a := openApproval(t, r.c, r.sess.ID, "")

	ct, _ := r.ph.sealedAnswer(t, a.RequestID, "approve")
	body, err := json.Marshal(map[string]string{"ciphertext": ct})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(relayURL+"/v1/mailbox/"+r.ph.hostID, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("mailbox post status %d, want 202", resp.StatusCode)
	}
	waitClosed(t, r.c, a.RequestID)
	waitFor(t, "the approve key", 15*time.Second, func() bool {
		b, _ := os.ReadFile(r.keysOut)
		return string(b) == "1"
	})
}
