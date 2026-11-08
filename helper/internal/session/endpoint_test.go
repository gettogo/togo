package session_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/session"
)

// The endpoint arguments are asked for on every start of the agent and
// never stored with the session's argv.
func TestEndpointArgsOnCreateRelaunchRestore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	recorder(t, s, dir, "claude")
	var asked []string
	prev := session.EndpointArgs
	session.EndpointArgs = func(agent, id, _ string) []string {
		asked = append(asked, agent+" "+id)
		return []string{"--port", "4242"}
	}
	t.Cleanup(func() { session.EndpointArgs = prev })

	created := mustCreate(t, s, session.CreateOpts{Agent: "claude", Args: []string{"--model", "opus"}, Cwd: dir})
	if runs := waitRuns(t, dir, 1); runs[0] != "--model opus --port 4242" {
		t.Fatalf("create ran %q", runs[0])
	}
	if !slices.Equal(created.Command[1:], []string{"--model", "opus"}) {
		t.Fatalf("stored argv %q", created.Command)
	}
	if err := s.RelaunchWith(context.Background(), created.ID, []string{"claude", "--model", "sonnet"}); err != nil {
		t.Fatal(err)
	}
	if runs := waitRuns(t, dir, 2); runs[1] != "--model sonnet --port 4242" {
		t.Fatalf("relaunch with ran %q", runs[1])
	}
	killServer(t, s)
	if _, err := s.Restore(context.Background(), created.ID, false); err != nil {
		t.Fatal(err)
	}
	if runs := waitRuns(t, dir, 3); runs[2] != "--model opus --port 4242" {
		t.Fatalf("restore ran %q", runs[2])
	}
	if len(asked) != 3 || asked[0] != "claude "+created.ID {
		t.Fatalf("asked %q", asked)
	}
}

func TestRelaunchWithRefusesOtherPrograms(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	recorder(t, s, dir, "claude")
	created := mustCreate(t, s, session.CreateOpts{Agent: "claude", Cwd: dir})
	waitRuns(t, dir, 1)
	for _, argv := range [][]string{nil, {"sh", "-c", "true"}, {"/tmp/claude"}, {"claude", "bad\x00arg"}} {
		var ip *session.InvalidParamsError
		if err := s.RelaunchWith(context.Background(), created.ID, argv); !errors.As(err, &ip) {
			t.Errorf("RelaunchWith(%q) = %v, want invalid params", argv, err)
		}
	}
}

func TestCrushJoinsClientServerMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	recorder(t, s, dir, "crush")
	created := mustCreate(t, s, session.CreateOpts{Agent: "crush", Cwd: dir})
	if env := tmuxOut(t, s.Tmux, "show-environment", "-t", "="+created.ID, "CRUSH_CLIENT_SERVER"); env != "CRUSH_CLIENT_SERVER=1" {
		t.Errorf("crush environment %q", env)
	}
	if env := tmuxOut(t, s.Tmux, "show-environment", "-t", "="+created.ID, "TOGO_AGENT"); env != "TOGO_AGENT=crush" {
		t.Errorf("crush agent %q", env)
	}
}

// An endpoint's password reaches the session's environment on create and
// is replaced on a relaunch; it is never stored with the argv.
func TestEndpointEnvOnCreateAndRelaunch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	recorder(t, s, dir, "claude")
	pw := "first"
	prevA, prevE := session.EndpointArgs, session.EndpointEnv
	session.EndpointArgs = func(string, string, string) []string { return []string{"--port", "1"} }
	session.EndpointEnv = func(string) map[string]string { return map[string]string{"FAKE_SERVER_PASSWORD": pw} }
	t.Cleanup(func() { session.EndpointArgs, session.EndpointEnv = prevA, prevE })
	created := mustCreate(t, s, session.CreateOpts{Agent: "claude", Cwd: dir})
	waitRuns(t, dir, 1)
	if env := tmuxOut(t, s.Tmux, "show-environment", "-t", "="+created.ID, "FAKE_SERVER_PASSWORD"); env != "FAKE_SERVER_PASSWORD=first" {
		t.Fatalf("create env %q", env)
	}
	pw = "second"
	if err := s.RelaunchWith(context.Background(), created.ID, []string{"claude"}); err != nil {
		t.Fatal(err)
	}
	if env := tmuxOut(t, s.Tmux, "show-environment", "-t", "="+created.ID, "FAKE_SERVER_PASSWORD"); env != "FAKE_SERVER_PASSWORD=second" {
		t.Fatalf("relaunch env %q", env)
	}
	if slices.Contains(created.Command, "first") {
		t.Fatal("password stored with the argv")
	}
}
