package engine

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

func TestScreenError(t *testing.T) {
	cases := []struct {
		screen, want string
	}{
		{"Login to OpenRouter or create a free account? (\nY)es/(N)o [Yes]: testing\nPlease answer with one of: yes, no, skip, all\nLogin to OpenRouter or create a free account? (\nY)es/(N)o [Yes]: \n", "Please answer with one of: yes, no, skip, all"},
		{"> hi\n\nlitellm.APIConnectionError: OpenAIException - Connection error.\nRetrying in 0.2 seconds...\n> \n", "litellm.APIConnectionError: OpenAIException - Connection error."},
		{"Error: 401 Unauthorized\n> ", "Error: 401 Unauthorized"},
		{"> fix the error handling in main.go\nI fixed the error: it was a nil map.\n> ", ""},
	}
	for _, c := range cases {
		got, ok := screenError(c.screen)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("screenError(%q) = %q, %v; want %q", c.screen, got, ok, c.want)
		}
	}
}

func TestExitNotice(t *testing.T) {
	one, zero, intr := 1, 0, 130
	cases := []struct {
		status string
		x      *wire.SessionExit
		code   string
		text   string
	}{
		{wire.StatusFailed, &wire.SessionExit{Code: &one, Reason: wire.ExitReasonExit}, harness.NoticeError, "Aider quit with exit status 1."},
		{wire.StatusEnded, &wire.SessionExit{Code: &zero, Reason: wire.ExitReasonExit}, harness.NoticeEnded, "Aider exited."},
		{wire.StatusEnded, &wire.SessionExit{Code: &intr, Reason: wire.ExitReasonInterrupted}, harness.NoticeEnded, "Aider was stopped."},
		{wire.StatusFailed, nil, harness.NoticeError, "Aider quit with an error."},
	}
	for _, c := range cases {
		n := exitNotice("Aider", c.status, c.x)
		if n.Code != c.code || n.Text != c.text {
			t.Errorf("exitNotice(%s, %+v) = %+v", c.status, c.x, n)
		}
		if n.Code == harness.NoticeError && n.Reason != harness.ErrCrashed {
			t.Errorf("crash reason = %q", n.Reason)
		}
	}
}

// A crash says so once in the chat, with its exit status.
func TestExitedAgentNoticeOnce(t *testing.T) {
	const id = "s_exitntc001"
	var status atomic.Value
	status.Store("idle")
	code := 1
	e := statusEngine(t, id, &seqPane{mode: "a"}, &status, func(c *Config) {
		c.Exit = func(string) *wire.SessionExit { return &wire.SessionExit{Code: &code, Reason: wire.ExitReasonExit} }
	})
	status.Store(wire.StatusFailed)
	sid := id
	for range 3 {
		e.Observe(wire.Event{Kind: wire.EventSession, SessionID: &sid, Payload: wire.SessionEvent{
			Session: session.Session{ID: id, Agent: "fake", Managed: true, Status: wire.StatusFailed},
		}})
	}
	waitFor(t, func() bool { return len(noticesIn(e, id)) >= 1 })
	ns := noticesIn(e, id)
	if len(ns) != 1 || !strings.Contains(ns[0].Notice.Text, "exit status 1") || ns[0].Notice.Reason != harness.ErrCrashed {
		t.Fatalf("notices = %+v", ns)
	}
}

// The owner's case after the fix: a turn ends with no reply and the pane
// shows the agent's error. The notice carries the error and its reason.
func TestTurnWithoutReplyNamesTheScreenError(t *testing.T) {
	const id = "s_noreplyscr"
	var status atomic.Value
	status.Store("idle")
	pane := &dialogPane{}
	e := statusEngine(t, id, pane, &status, func(c *Config) { c.Screen = pane.Screen })
	s, _ := e.served(context.Background(), id)
	if _, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: "hi", Mode: harness.SendNow}); err != nil {
		t.Fatal(err)
	}
	pane.set("> hi\nlitellm.AuthenticationError: OpenrouterException - 401 No auth credentials found\n> \n")
	deliver(id, map[string]any{"state": map[string]string{"working": "true"}})
	waitFor(t, func() bool { return s.sum.Load().Working })
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { return len(noticesIn(e, id)) == 1 })
	n := noticesIn(e, id)[0].Notice
	if n.Reason != harness.ErrAuth || !strings.HasPrefix(n.Text, "litellm.AuthenticationError") {
		t.Fatalf("notice = %+v", n)
	}
}
