package session_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// recorder puts on PATH, and on s's login PATH, a stand-in named name that
// appends its arguments to runs in dir each time it runs and then waits at
// its prompt. It returns the stand-in's path.
func recorder(t *testing.T, s *session.Service, dir, name string) string {
	t.Helper()
	bin := realpath(t, t.TempDir())
	p := filepath.Join(bin, name)
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + filepath.Join(dir, "runs") + "'\nread -r line\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(filepath.ListSeparator)+os.Getenv("PATH"))
	s.Path = bin
	return p
}

func killServer(t *testing.T, s *session.Service) {
	t.Helper()
	if err := exec.Command(s.Tmux.Bin, append(append([]string{}, s.Tmux.Socket...), "kill-server")...).Run(); err != nil {
		t.Fatal(err)
	}
}

func mustRestorable(t *testing.T, s *session.Service) []wire.RestorableSession {
	t.Helper()
	got, err := s.Restorable(context.Background())
	if err != nil {
		t.Fatalf("Restorable: %v", err)
	}
	return got
}

func ids(list []wire.RestorableSession) []string {
	out := []string{}
	for _, r := range list {
		out = append(out, r.ID)
	}
	return out
}

func TestRestorableAfterServerRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	recorder(t, s, dir, "claude")
	a := mustCreate(t, s, session.CreateOpts{Agent: "claude", Args: []string{"--model", "opus"}, Cwd: dir})
	b := mustCreate(t, s, session.CreateOpts{Agent: "shell", Cwd: dir})
	if got := mustRestorable(t, s); len(got) != 0 {
		t.Fatalf("restorable while running: %q", ids(got))
	}
	killServer(t, s)
	got := mustRestorable(t, s)
	want := []wire.RestorableSession{
		{ID: a.ID, Bib: "01", Name: "Claude Code", Agent: "claude", Command: []string{"claude", "--model", "opus"},
			Cwd: dir, CreatedAt: fixedNow, EndedAt: fixedNow, Resumable: true},
		{ID: b.ID, Bib: "02", Name: "Shell", Agent: "shell", Cwd: dir, CreatedAt: fixedNow, EndedAt: fixedNow},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restorable\n%+v\nwant\n%+v", got, want)
	}
	// The time it was first found gone stays.
	s.Now = func() time.Time { return fixedNow.Add(time.Hour) }
	if got := mustRestorable(t, s); len(got) != 2 || !got[0].EndedAt.Equal(fixedNow) {
		t.Fatalf("restorable an hour later: %+v", got)
	}
}

func TestKilledAtDeskNotRestorable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	s.Now = time.Now
	ended := mustCreate(t, s, session.CreateOpts{})
	kept := mustCreate(t, s, session.CreateOpts{})
	tmuxOut(t, s.Tmux, "kill-session", "-t", "="+ended.ID)
	if got := mustRestorable(t, s); len(got) != 0 {
		t.Fatalf("restorable %q, want none", ids(got))
	}
	all, err := s.Manifest.All()
	if err != nil || len(all) != 1 || all[0].ID != kept.ID {
		t.Fatalf("record %+v %v, want only %s", all, err, kept.ID)
	}
	// The server dies later: only the session still running then is offered.
	killServer(t, s)
	if got := ids(mustRestorable(t, s)); !slices.Equal(got, []string{kept.ID}) {
		t.Fatalf("restorable %q, want %s", got, kept.ID)
	}
}

func TestRestoreKeepsIDBibNameArgv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	recorder(t, s, dir, "claude")
	args := []string{"--model", "opus", "a;b $(touch x) #{session_name}"}
	created := mustCreate(t, s, session.CreateOpts{Agent: "claude", Args: args, Name: "work", Cwd: dir})
	waitRuns(t, dir, 1)
	killServer(t, s)
	got, err := s.Restore(context.Background(), created.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	runs := waitRuns(t, dir, 2)
	if want := strings.Join(args, " "); runs[1] != want {
		t.Fatalf("restored run got %q, want %q", runs[1], want)
	}
	if got.ID != created.ID || *got.Bib != *created.Bib || got.Name != "work" || got.Agent != "claude" ||
		got.Cwd != dir || !slices.Equal(got.Command, created.Command) || got.Exit != nil || !got.Managed {
		t.Fatalf("restored %+v, created %+v", got, created)
	}
	if env := tmuxOut(t, s.Tmux, "show-environment", "-t", "="+created.ID, "TOGO_SESSION"); env != "TOGO_SESSION="+created.ID {
		t.Errorf("environment %q", env)
	}
	if env := tmuxOut(t, s.Tmux, "show-environment", "-t", "="+created.ID, "TOGO_AGENT"); env != "TOGO_AGENT=claude" {
		t.Errorf("agent environment %q", env)
	}
	if left := mustRestorable(t, s); len(left) != 0 {
		t.Fatalf("still restorable: %q", ids(left))
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); err == nil {
		t.Fatal("an argument ran as shell text")
	}
	// A restored session is recorded again, so a second restart offers it.
	killServer(t, s)
	if got := ids(mustRestorable(t, s)); !slices.Equal(got, []string{created.ID}) {
		t.Fatalf("after a second restart %q", got)
	}
}

func TestRestoreBibTakenGetsLowestFree(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	first := mustCreate(t, s, session.CreateOpts{})
	second := mustCreate(t, s, session.CreateOpts{})
	killServer(t, s)
	s.NewID = func() string { return "s_zzzzzzzzzz" }
	if late := mustCreate(t, s, session.CreateOpts{}); *late.Bib != "01" {
		t.Fatalf("new session got %s, want 01", *late.Bib)
	}
	got, err := s.Restore(context.Background(), first.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if *got.Bib != "02" {
		t.Fatalf("restored with 01 taken got %s, want 02", *got.Bib)
	}
	got, err = s.Restore(context.Background(), second.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if *got.Bib != "03" {
		t.Fatalf("restored with 01 and 02 taken got %s, want 03", *got.Bib)
	}
}

func TestRestoreResumeAppendsContinue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	recorder(t, s, dir, "claude")
	created := mustCreate(t, s, session.CreateOpts{Agent: "claude", Args: []string{"--model", "opus"}, Cwd: dir})
	waitRuns(t, dir, 1)
	killServer(t, s)
	got, err := s.Restore(context.Background(), created.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if runs := waitRuns(t, dir, 2); runs[1] != "--model opus --continue" {
		t.Fatalf("resumed run got %q, want it to end --continue", runs[1])
	}
	if !slices.Equal(got.Command, []string{"claude", "--model", "opus"}) {
		t.Fatalf("command %q, want the one the user gave", got.Command)
	}
}

func TestRestoreResumeSkippedWithPositional(t *testing.T) {
	for _, args := range [][]string{{"fix it"}, {"--model", "opus", "fix it"}, {"--", "fix it"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			s := newService(t)
			dir := realpath(t, t.TempDir())
			recorder(t, s, dir, "claude")
			created := mustCreate(t, s, session.CreateOpts{Agent: "claude", Args: args, Cwd: dir})
			waitRuns(t, dir, 1)
			killServer(t, s)
			if got := mustRestorable(t, s); len(got) != 1 || got[0].Resumable {
				t.Fatalf("restorable %+v, want one not resumable", got)
			}
			if _, err := s.Restore(context.Background(), created.ID, true); err != nil {
				t.Fatal(err)
			}
			if runs := waitRuns(t, dir, 2); runs[1] != strings.Join(args, " ") {
				t.Fatalf("restored run got %q, want the saved args unchanged", runs[1])
			}
		})
	}
}

// TestLastSessionClosedNotRestorable ends the only session from inside it:
// the server stays up, so the session reads as ended at the desk.
func TestLastSessionClosedNotRestorable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	s.Now = time.Now
	conf := filepath.Join(t.TempDir(), "tmux.conf")
	if err := os.WriteFile(conf, []byte(session.TmuxConf), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Tmux.Config = conf
	created := mustCreate(t, s, session.CreateOpts{Agent: wire.AgentCustom, Program: "/bin/sh", Args: []string{"-c", "exit 0"}})
	// The wrapper falls back to a login shell; end it too.
	for deadline := time.Now().Add(10 * time.Second); ; {
		_ = exec.Command(s.Tmux.Bin, append(append([]string{}, s.Tmux.Socket...), "send-keys", "-t", "="+created.ID+":", "exit", "Enter")...).Run()
		if len(mustList(t, s)) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the session never ended")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, up, err := s.Tmux.ServerStarted(context.Background()); err != nil || !up {
		t.Fatalf("server up %v (%v), want it kept with no sessions", up, err)
	}
	if got := mustRestorable(t, s); len(got) != 0 {
		t.Fatalf("restorable %q, want none", ids(got))
	}
}

// TestSettlePrunesDeskKillBeforeRestart kills a session outside togo while
// the server is up; Settle, which the watcher runs on reconcile, drops it,
// so a later server death offers only the session that was running.
func TestSettlePrunesDeskKillBeforeRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	s.Now = time.Now
	ended := mustCreate(t, s, session.CreateOpts{})
	kept := mustCreate(t, s, session.CreateOpts{})
	tmuxOut(t, s.Tmux, "kill-session", "-t", "="+ended.ID)
	if err := s.Settle(context.Background()); err != nil {
		t.Fatal(err)
	}
	killServer(t, s)
	if got := ids(mustRestorable(t, s)); !slices.Equal(got, []string{kept.ID}) {
		t.Fatalf("restorable %q, want only %s", got, kept.ID)
	}
}

func TestRestoreCustomNotResumable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	program := recorder(t, s, dir, "my-agent")
	created := mustCreate(t, s, session.CreateOpts{Agent: wire.AgentCustom, Program: program, Args: []string{"-x"}, Cwd: dir})
	waitRuns(t, dir, 1)
	killServer(t, s)
	if got := mustRestorable(t, s); len(got) != 1 || got[0].Resumable {
		t.Fatalf("restorable %+v, want one not resumable", got)
	}
	if _, err := s.Restore(context.Background(), created.ID, true); err != nil {
		t.Fatal(err)
	}
	if runs := waitRuns(t, dir, 2); runs[1] != "-x" {
		t.Fatalf("restored run got %q, want -x alone", runs[1])
	}
}

func TestRestoreFolderGone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := filepath.Join(realpath(t, t.TempDir()), "proj")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	created := mustCreate(t, s, session.CreateOpts{Cwd: dir})
	killServer(t, s)
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	_, err := s.Restore(context.Background(), created.ID, false)
	if !errors.Is(err, fsx.ErrInvalidPath) {
		t.Fatalf("restore into a removed folder: %v, want ErrInvalidPath", err)
	}
	a := &api.API{Sessions: s, Ext: map[string]api.Method{wire.MethodSessionRestore: api.Restore(s, nil)}}
	_, rerr := a.Handle(context.Background(), wire.MethodSessionRestore, json.RawMessage(`{"id":"`+created.ID+`","resume":false}`))
	if rerr == nil || rerr.Code != rpc.CodeInvalidPath {
		t.Fatalf("rpc error %+v, want -32003", rerr)
	}
	if got := ids(mustRestorable(t, s)); !slices.Equal(got, []string{created.ID}) {
		t.Fatalf("restorable %q, want it still offered", got)
	}
}

func TestRestoreNotInstalled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	recorder(t, s, dir, "claude")
	created := mustCreate(t, s, session.CreateOpts{Agent: "claude", Cwd: dir})
	killServer(t, s)
	s.Path = t.TempDir()
	_, err := s.Restore(context.Background(), created.ID, false)
	if r := reason(t, err); r != wire.ReasonNotInstalled {
		t.Fatalf("reason %q, want %s", r, wire.ReasonNotInstalled)
	}
}

func TestDismissRemoves(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	created := mustCreate(t, s, session.CreateOpts{})
	ctx := context.Background()
	if err := s.Dismiss(ctx, created.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("dismiss a running session: %v, want ErrNotFound", err)
	}
	killServer(t, s)
	if err := s.Dismiss(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if got := mustRestorable(t, s); len(got) != 0 {
		t.Fatalf("restorable after dismiss: %q", ids(got))
	}
	if all, _ := s.Manifest.All(); len(all) != 0 {
		t.Fatalf("record after dismiss: %+v", all)
	}
	if err := s.Dismiss(ctx, created.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("second dismiss: %v, want ErrNotFound", err)
	}
	if _, err := s.Restore(ctx, created.ID, false); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("restore after dismiss: %v, want ErrNotFound", err)
	}
}

func TestPruneAfterSevenDays(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	created := mustCreate(t, s, session.CreateOpts{})
	killServer(t, s)
	if got := ids(mustRestorable(t, s)); !slices.Equal(got, []string{created.ID}) {
		t.Fatalf("restorable %q", got)
	}
	s.Now = func() time.Time { return fixedNow.Add(7*24*time.Hour - time.Minute) }
	if got := mustRestorable(t, s); len(got) != 1 {
		t.Fatalf("restorable just under seven days: %q", ids(got))
	}
	s.Now = func() time.Time { return fixedNow.Add(7*24*time.Hour + time.Minute) }
	if got := mustRestorable(t, s); len(got) != 0 {
		t.Fatalf("restorable after seven days: %q", ids(got))
	}
	if all, _ := s.Manifest.All(); len(all) != 0 {
		t.Fatalf("record after seven days: %+v", all)
	}
}

// seedRestorable records the Claude Code session the restore fixtures
// describe as ended by a restart.
func seedRestorable(t *testing.T, svc *session.Service, cwd string) {
	t.Helper()
	err := svc.Manifest.Put(session.ManifestEntry{
		ID: "s_abcdefghij", Bib: "01", Name: "Claude Code", Agent: "claude", Cwd: cwd,
		Command:   []string{"claude", "--model", "opus"},
		CreatedAt: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC),
		EndedAt:   time.Date(2026, 11, 3, 11, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// runFixture plays the fixture name against a and compares the
// last response, with <home> standing for home.
func runFixture(t *testing.T, a *api.API, home, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "protocol", "v1", "fixtures", name+".json"))
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
	var in bytes.Buffer
	for _, req := range append(append([]json.RawMessage{}, fx.Setup...), fx.Request) {
		if err := rpc.WriteFrame(&in, req); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := rpc.Serve(context.Background(), &in, &out, a, rpc.HelperInfo{Version: "0.0.0-test"}); err != nil {
		t.Fatal(err)
	}
	var last json.RawMessage
	for out.Len() > 0 {
		if last, err = rpc.ReadFrame(&out); err != nil {
			t.Fatal(err)
		}
	}
	var got, want any
	_ = json.Unmarshal(last, &got)
	_ = json.Unmarshal(bytes.ReplaceAll(fx.Response, []byte("<home>"), []byte(home)), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("response %s\nwant %s", last, fx.Response)
	}
}
