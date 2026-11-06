package session_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// replyService is newService with a login shell that has job control, as
// zsh -lic and bash -lic do, so the pane reports the agent while it runs
// and the login shell once the wrapper falls back to it. The shell is
// named bash and SHELL is /bin/bash, with HOME empty of startup files.
func replyService(t *testing.T) *session.Service {
	t.Helper()
	s := newService(t)
	p := filepath.Join(t.TempDir(), "bash")
	script := "#!/bin/sh\n[ \"$1\" = -lic ] || exit 64\nshift\nexec /bin/sh -i -c \"$@\"\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s.Shell = p
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ENV", "")
	return s
}

// teeSession starts an agent that copies what it reads to a file and
// returns the session and the file.
func teeSession(t *testing.T, s *session.Service) (session.Session, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "in")
	sess := mustCreate(t, s, session.CreateOpts{Agent: wire.AgentCustom, Program: "/usr/bin/tee", Args: []string{out}})
	waitPane(t, s, sess.ID, "tee")
	return sess, out
}

func waitPane(t *testing.T, s *session.Service, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := s.Tmux.PaneCommand(context.Background(), "="+id+":")
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane command is %q, want %q", got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func fileText(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(b)
}

func screen(t *testing.T, s *session.Service, id string) string {
	t.Helper()
	text, _, err := s.Capture(context.Background(), id, 100, true)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return text
}

func refusal(t *testing.T, err error) string {
	t.Helper()
	var r *session.ReplyRefusedError
	if !errors.As(err, &r) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	return r.Reason
}

func TestSendTextPastesLiterally(t *testing.T) {
	s := replyService(t)
	sess, out := teeSession(t, s)
	text := "first $(touch x); `id` && echo y\n'q' \"z\" #{session_name} \\;\nlast;"
	if err := s.SendText(context.Background(), sess.ID, text, true, "done", nil); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	eventually(t, "the reply", func() bool { return fileText(t, out) == text+"\n" })
}

func TestSendTextSubmitPressesEnter(t *testing.T) {
	s := replyService(t)
	sess, out := teeSession(t, s)
	ctx := context.Background()
	if err := s.SendText(ctx, sess.ID, "draft", false, "done", nil); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	eventually(t, "the draft on screen", func() bool { return strings.Contains(screen(t, s, sess.ID), "draft") })
	time.Sleep(200 * time.Millisecond)
	if got := fileText(t, out); got != "" {
		t.Fatalf("without submit the agent read %q", got)
	}
	if err := s.SendText(ctx, sess.ID, " sent", true, "done", nil); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	eventually(t, "the submitted line", func() bool { return fileText(t, out) == "draft sent\n" })
}

func TestSendTextRefusesShellForeground(t *testing.T) {
	s := replyService(t)
	sess := mustCreate(t, s, session.CreateOpts{Agent: wire.AgentCustom, Program: "/usr/bin/true"})
	waitPane(t, s, sess.ID, "bash")
	err := s.SendText(context.Background(), sess.ID, "rm -rf ~", true, "done", nil)
	if got := refusal(t, err); got != wire.ConflictEnded {
		t.Fatalf("reason = %q, want %q", got, wire.ConflictEnded)
	}
	time.Sleep(200 * time.Millisecond)
	if strings.Contains(screen(t, s, sess.ID), "rm -rf") {
		t.Fatal("refused text reached the pane")
	}
}

func TestSendTextRefusesNeedsYou(t *testing.T) {
	s := replyService(t)
	sess, out := teeSession(t, s)
	err := s.SendText(context.Background(), sess.ID, "yes", true, "needs_you", nil)
	if got := refusal(t, err); got != wire.ConflictNeedsYou {
		t.Fatalf("reason = %q, want %q", got, wire.ConflictNeedsYou)
	}
	time.Sleep(200 * time.Millisecond)
	if fileText(t, out) != "" || strings.Contains(screen(t, s, sess.ID), "yes") {
		t.Fatal("refused text reached the pane")
	}
}

func TestSendTextRefusesShellAgent(t *testing.T) {
	s := replyService(t)
	sess := mustCreate(t, s, session.CreateOpts{Agent: "shell"})
	err := s.SendText(context.Background(), sess.ID, "ls", true, "idle", nil)
	if got := refusal(t, err); got != wire.ConflictShell {
		t.Fatalf("reason = %q, want %q", got, wire.ConflictShell)
	}
	if err := s.SendText(context.Background(), "u_6a6f62", "ls", true, "idle", nil); !errors.Is(err, session.ErrNotManaged) {
		t.Fatalf("unmanaged session: err = %v, want ErrNotManaged", err)
	}
}

func TestSendTextRefusesEnded(t *testing.T) {
	s := replyService(t)
	sess, out := teeSession(t, s)
	code := 1
	for _, tc := range []struct {
		status string
		exit   *wire.SessionExit
	}{
		{"ended", nil},
		{"exited", nil},
		{"failed", &wire.SessionExit{Reason: wire.ExitReasonExit, Code: &code}},
	} {
		err := s.SendText(context.Background(), sess.ID, "go on", true, tc.status, tc.exit)
		if got := refusal(t, err); got != wire.ConflictEnded {
			t.Errorf("status %s: reason = %q, want %q", tc.status, got, wire.ConflictEnded)
		}
	}
	// A failure the agent survived, such as a usage limit, leaves it at
	// its prompt, so a reply goes through.
	limit := &wire.SessionExit{Reason: wire.ExitReasonUsageLimit}
	if err := s.SendText(context.Background(), sess.ID, "go on", true, "failed", limit); err != nil {
		t.Fatalf("failed without an exit code: %v", err)
	}
	eventually(t, "the reply", func() bool { return fileText(t, out) == "go on\n" })
}

func TestSendTextValidates(t *testing.T) {
	ok := []string{"a", strings.Repeat("é", wire.MaxSendText), "two\nlines", "emoji \U0001F600"}
	for _, text := range ok {
		if err := session.ValidateSendText(text); err != nil {
			t.Errorf("ValidateSendText(%.20q) = %v, want nil", text, err)
		}
	}
	bad := []string{
		"", strings.Repeat("a", wire.MaxSendText+1), "esc \x1b[201~ out", "tab\there",
		"cr\r", "del\x7f", "c1 \u009b", "nul\x00", "bad utf8 \xff",
	}
	for _, text := range bad {
		err := session.ValidateSendText(text)
		var invalid *session.InvalidParamsError
		if !errors.As(err, &invalid) || invalid.Reason != wire.ReasonText {
			t.Errorf("ValidateSendText(%.20q) = %v, want reason %q", text, err, wire.ReasonText)
		}
	}
	// SendText validates before it looks at tmux.
	s := replyService(t)
	sess, out := teeSession(t, s)
	if err := s.SendText(context.Background(), sess.ID, "x\x1by", true, "done", nil); err == nil {
		t.Fatal("SendText accepted an escape")
	}
	time.Sleep(200 * time.Millisecond)
	if fileText(t, out) != "" {
		t.Fatal("rejected text reached the pane")
	}
}

// TestSendTextFixtures runs the reply exchanges over the protocol. HOME
// holds proj, and the login PATH holds a claude that waits at its prompt.
// Status comes from a stand-in for the hub that reports the agent done.
func TestSendTextFixtures(t *testing.T) {
	for _, name := range []string{"session.sendText", "session.sendText-shell"} {
		t.Run(name, func(t *testing.T) {
			svc := replyService(t)
			home := realpath(t, t.TempDir())
			t.Setenv("HOME", home)
			bin := filepath.Join(home, "bin")
			for _, dir := range []string{filepath.Join(home, "proj"), bin} {
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexec /bin/cat\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(filepath.ListSeparator)+os.Getenv("PATH"))
			svc.Path = bin
			svc.NewID = func() string { return "s_abcdefghij" }
			done := func(ss []session.Session) []session.Session {
				for i := range ss {
					ss[i].Status = "done"
				}
				return ss
			}
			a := &api.API{Sessions: svc, Ext: map[string]api.Method{wire.MethodSessionSendText: api.SendText(svc, done)}}

			raw, err := os.ReadFile(filepath.Join("..", "..", "..", "protocol", "v1", "fixtures", "pending", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var fx struct {
				Setup    []json.RawMessage `json:"setup"`
				Request  json.RawMessage   `json:"request"`
				Response json.RawMessage   `json:"response"`
			}
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			serve := func(reqs ...json.RawMessage) json.RawMessage {
				var in, out bytes.Buffer
				for _, req := range reqs {
					if err := rpc.WriteFrame(&in, req); err != nil {
						t.Fatal(err)
					}
				}
				if err := rpc.Serve(context.Background(), &in, &out, a, rpc.HelperInfo{Version: "0.0.0-test"}); err != nil {
					t.Fatal(err)
				}
				var last json.RawMessage
				for out.Len() > 0 {
					if last, err = rpc.ReadFrame(&out); err != nil {
						t.Fatal(err)
					}
				}
				return last
			}
			serve(fx.Setup...)
			if name == "session.sendText" {
				waitPane(t, svc, "s_abcdefghij", "cat")
			}
			last := serve(fx.Setup[0], fx.Request)
			var got, want any
			_ = json.Unmarshal(last, &got)
			_ = json.Unmarshal(fx.Response, &want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("response %s\nwant %s", last, fx.Response)
			}
		})
	}
}

func TestSendTextConcurrentRepliesSubmitSeparately(t *testing.T) {
	s := replyService(t)
	sess, out := teeSession(t, s)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, text := range []string{"first reply", "second reply"} {
		wg.Go(func() { errs[i] = s.SendText(context.Background(), sess.ID, text, true, "done", nil) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("SendText: %v", err)
		}
	}
	eventually(t, "both replies", func() bool { return strings.Count(fileText(t, out), "\n") >= 2 })
	got := fileText(t, out)
	if got != "first reply\nsecond reply\n" && got != "second reply\nfirst reply\n" {
		t.Fatalf("agent read %q, want two separate lines", got)
	}
}
