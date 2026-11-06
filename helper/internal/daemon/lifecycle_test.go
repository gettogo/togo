package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

// lifecycleClaude is the stand-in claude for each fixture: list-ended
// exits 0 at once; relaunch exits 1 the first time and then waits;
// relaunch-running only waits.
var lifecycleClaude = map[string]string{
	"session.list-ended":       "#!/bin/sh\nexit 0\n",
	"session.relaunch":         "#!/bin/sh\nif [ ! -e \"$HOME/ran\" ]; then : > \"$HOME/ran\"; exit 1; fi\nread -r line\n",
	"session.relaunch-running": "#!/bin/sh\nread -r line\n",
}

// fixtureShell is a login shell stand-in that runs the -lic script without
// reading startup files, so the pane sees only the test's PATH.
func fixtureShell(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n[ \"$1\" = -lic ] || exit 64\nshift\nexec /bin/sh -c \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

type exchange struct {
	Setup    []json.RawMessage `json:"setup"`
	Request  json.RawMessage   `json:"request"`
	Response json.RawMessage   `json:"response"`
}

// serve runs frames through the composed handler and returns the last
// response.
func serve(t *testing.T, h rpc.Handler, frames ...json.RawMessage) json.RawMessage {
	t.Helper()
	var in, out bytes.Buffer
	for _, f := range frames {
		if err := rpc.WriteFrame(&in, f); err != nil {
			t.Fatal(err)
		}
	}
	if err := rpc.Serve(context.Background(), &in, &out, h, rpc.HelperInfo{Version: "0.0.0-test", Capabilities: Capabilities}); err != nil {
		t.Fatal(err)
	}
	var last json.RawMessage
	for out.Len() > 0 {
		var err error
		if last, err = rpc.ReadFrame(&out); err != nil {
			t.Fatal(err)
		}
	}
	return last
}

// TestLifecycleFixtures runs the ended and relaunch exchanges against the
// composed daemon on a private tmux server. Where a fixture says the agent
// exited, the test sends the exit report the launch wrapper sends once the
// pane has fallen back to its shell.
func TestLifecycleFixtures(t *testing.T) {
	for _, name := range []string{"session.list-ended", "session.relaunch", "session.relaunch-running"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "..", "protocol", "v1", "fixtures", "pending", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var fx exchange
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(home, "bin")
			for _, d := range []string{filepath.Join(home, "proj"), bin} {
				if err := os.Mkdir(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(lifecycleClaude[name]), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("SHELL", "/bin/sh")
			tm := tmuxtest.New(t)
			if p, err := exec.LookPath(tm.Bin); err == nil {
				tm.Bin = p
			}
			t.Setenv("PATH", bin+":/usr/bin:/bin")
			togoHome := t.TempDir()
			recent := &state.Recent{File: filepath.Join(togoHome, "recent.json")}
			svc := &session.Service{
				Tmux: tm, Recent: recent, Shell: fixtureShell(t), Path: bin,
				LockFile: filepath.Join(togoHome, "run", "bib.lock"),
				NewID:    func() string { return "s_abcdefghij" },
				Now:      func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) },
			}
			c, err := Compose(&api.API{Sessions: svc, Recent: recent, Home: togoHome}, ComposeOptions{Home: togoHome})
			if err != nil {
				t.Fatal(err)
			}
			h := c.Handler()
			serve(t, h, fx.Setup...)

			if name == "session.relaunch-running" {
				// The fixture's agent waits at its prompt; a relaunch
				// sent before it starts would find the pane empty.
				waitAgentRunning(t, svc, "s_abcdefghij")
			} else {
				code := 0
				if name == "session.relaunch" {
					code = 1
				}
				reportExit(t, c, "s_abcdefghij", code)
				waitAgentGone(t, svc, "s_abcdefghij")
			}
			last := serve(t, h, fx.Setup[0], fx.Request)
			var got, want any
			_ = json.Unmarshal(last, &got)
			_ = json.Unmarshal(bytes.ReplaceAll(fx.Response, []byte("<home>"), []byte(home)), &want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("response %s\nwant %s", last, fx.Response)
			}
			if name == "session.relaunch" {
				if c.Watch.Ended("s_abcdefghij") {
					t.Fatal("still ended after the relaunch")
				}
				lateEndIgnored(t, c, "s_abcdefghij")
			}
		})
	}
}

// lateEndIgnored sends the SessionEnd hook of the run a relaunch killed,
// which may arrive after the relaunch: it must not end the new run. Once
// the new run's SessionStart arrives, its own SessionEnd counts.
func lateEndIgnored(t *testing.T, c *Components, id string) {
	t.Helper()
	hook := c.Local.Methods()[wire.MethodLocalHook]
	send := func(event string) {
		raw, _ := json.Marshal(wire.HookEvent{Agent: "claude", Event: event, SessionID: &id, Cwd: "/"})
		if _, err := hook(context.Background(), raw); err != nil {
			t.Fatal(err)
		}
	}
	send(local.EventEnd)
	if c.Watch.Ended(id) {
		t.Fatal("a SessionEnd from the killed run ended the new one")
	}
	send(local.EventStart)
	send(local.EventEnd)
	if !c.Watch.Ended(id) {
		t.Fatal("the new run's SessionEnd did not count")
	}
}

// waitAgentRunning waits until the session's pane shows its agent running.
func waitAgentRunning(t *testing.T, svc *session.Service, id string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; {
		if gone, err := svc.AgentGone(context.Background(), id); err == nil && !gone {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the agent never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// reportExit sends the exit report until it counts: the pane's login
// shell may still be running its profile when the agent has gone.
func reportExit(t *testing.T, c *Components, id string, code int) {
	t.Helper()
	sig := wire.Signal{SessionID: id, Kind: wire.SignalExit, Code: &code}
	for deadline := time.Now().Add(10 * time.Second); !c.Watch.Ended(id); {
		if err := c.Watch.Signal(context.Background(), sig); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the exit never counted")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestComposeServesLifecycle(t *testing.T) {
	if !slices.Contains(Capabilities, wire.CapLifecycle) {
		t.Errorf("capabilities %q lack %s", Capabilities, wire.CapLifecycle)
	}
}

// standInEnv makes the test binary act as the helper's session-exit: it
// creates the file the variable names and then holds the pane's
// foreground for a while, as the real one does while it waits for the
// daemon. A copy named togo has the process name the real helper has.
const standInEnv = "TOGO_TEST_SESSION_EXIT_STAND_IN"

func TestMain(m *testing.M) {
	if p := os.Getenv(standInEnv); p != "" && len(os.Args) == 3 && os.Args[1] == "session-exit" {
		_ = os.WriteFile(p, nil, 0o600)
		time.Sleep(3 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestWrapperExitReportCounts runs the real launch wrapper under each
// login shell on this machine. A report sent while the agent runs is
// ignored; the report sent while the stand-in helper holds the pane's
// foreground, as session-exit does, counts.
func TestWrapperExitReportCounts(t *testing.T) {
	for _, shell := range []string{"/bin/sh", "/bin/bash", "/bin/zsh"} {
		if _, err := os.Stat(shell); err != nil {
			continue
		}
		t.Run(filepath.Base(shell), func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("SHELL", "/bin/sh")
			tm := tmuxtest.New(t)
			if p, err := exec.LookPath(tm.Bin); err == nil {
				tm.Bin = p
			}
			t.Setenv("PATH", "/usr/bin:/bin")
			flag, called := filepath.Join(home, "stop"), filepath.Join(home, "called")
			agent := filepath.Join(home, "agent")
			togo := filepath.Join(home, "togo")
			for p, body := range map[string]string{
				agent: "#!/bin/sh\nwhile [ ! -e " + flag + " ]; do sleep 0.05; done\nexit 3\n",
			} {
				if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			copyFile(t, os.Args[0], togo)
			t.Setenv(standInEnv, called)
			togoHome := t.TempDir()
			recent := &state.Recent{File: filepath.Join(togoHome, "recent.json")}
			svc := &session.Service{Tmux: tm, Recent: recent, Shell: shell, Path: "/usr/bin:/bin",
				LockFile: filepath.Join(togoHome, "run", "bib.lock")}
			c, err := Compose(&api.API{Sessions: svc, Recent: recent, Home: togoHome}, ComposeOptions{Home: togoHome, TogoPath: togo})
			if err != nil {
				t.Fatal(err)
			}
			sess, err := svc.Create(context.Background(), session.CreateOpts{Agent: wire.AgentCustom, Program: agent, Cwd: home})
			if err != nil {
				t.Fatal(err)
			}
			code := 3
			sig := wire.Signal{SessionID: sess.ID, Kind: wire.SignalExit, Code: &code}
			for deadline := time.Now().Add(10 * time.Second); ; {
				fg, err := tm.Foreground(context.Background(), sess.ID)
				if err == nil && slices.ContainsFunc(fg.Foreground, func(p tmux.Proc) bool { return p.PID != fg.PanePID }) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("the agent never ran: %+v %v", fg, err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err := c.Watch.Signal(context.Background(), sig); err != nil || c.Watch.Ended(sess.ID) {
				t.Fatalf("a report while the agent runs counted (err %v)", err)
			}
			if err := os.WriteFile(flag, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(10 * time.Second); ; {
				if _, err := os.Stat(called); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the wrapper never ran the helper")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err := c.Watch.Signal(context.Background(), sig); err != nil || !c.Watch.Ended(sess.ID) {
				t.Fatalf("the wrapper's report did not count (err %v)", err)
			}
			if status, exit := c.Hub.Exit(sess.ID); status != wire.StatusFailed || exit == nil || *exit.Code != 3 {
				t.Fatalf("exit %q %+v, want failed 3", status, exit)
			}
		})
	}
}
