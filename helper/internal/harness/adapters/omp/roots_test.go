package omp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

func TestAgentDirFollowsOmpsOwnRules(t *testing.T) {
	const home = "/Users/u"
	cases := []struct {
		name    string
		argv    []string
		environ []string
		want    string
	}{
		{"default", []string{"omp"}, []string{}, "/Users/u/.omp/agent"},
		{"config dir name", []string{"omp"}, []string{"PI_CONFIG_DIR=.omp-work"}, "/Users/u/.omp-work/agent"},
		{"agent dir", []string{"omp"}, []string{"PI_CODING_AGENT_DIR=/srv/omp/agent"}, "/srv/omp/agent"},
		{"relative agent dir is not followed", []string{"omp"}, []string{"PI_CODING_AGENT_DIR=agent"}, "/Users/u/.omp/agent"},
		{"profile from the environment", []string{"omp"}, []string{"OMP_PROFILE=work"}, "/Users/u/.omp/profiles/work/agent"},
		{"legacy profile variable", []string{"omp"}, []string{"PI_PROFILE=old"}, "/Users/u/.omp/profiles/old/agent"},
		{"empty OMP_PROFILE selects the default", []string{"omp"}, []string{"OMP_PROFILE=", "PI_PROFILE=old"}, "/Users/u/.omp/agent"},
		{"profile wins over the agent dir", []string{"omp"}, []string{"OMP_PROFILE=work", "PI_CODING_AGENT_DIR=/srv/a"}, "/Users/u/.omp/profiles/work/agent"},
		{"profile flag", []string{"omp", "--profile", "side"}, []string{"OMP_PROFILE=work"}, "/Users/u/.omp/profiles/side/agent"},
		{"profile flag with equals", []string{"omp", "--profile=side"}, []string{}, "/Users/u/.omp/profiles/side/agent"},
		{"a flag after -- is prompt text", []string{"omp", "--", "--profile", "x"}, []string{}, "/Users/u/.omp/agent"},
		{"default profile name", []string{"omp"}, []string{"OMP_PROFILE=default"}, "/Users/u/.omp/agent"},
		{"a path is not a profile", []string{"omp"}, []string{"OMP_PROFILE=../x"}, "/Users/u/.omp/agent"},
		{"profile under a renamed config dir", []string{"omp"}, []string{"PI_CONFIG_DIR=.cfg", "OMP_PROFILE=work"}, "/Users/u/.cfg/profiles/work/agent"},
	}
	for _, c := range cases {
		env := harness.Env{Argv: c.argv, Environ: c.environ, Vars: map[string]string{source.VarHome: home}}
		if got := agentDir(env); got != c.want {
			t.Errorf("%s: agentDir = %q, want %q", c.name, got, c.want)
		}
	}
}

// Seen live on omp 18.20 with PI_CODING_AGENT_DIR set: the reply was saved
// under the custom folder and the chat showed only the sent prompt, because
// the feeds and the terminal record were read from ~/.omp/agent.
func TestTerminalRecordIsReadFromAMovedAgentFolder(t *testing.T) {
	home, moved, cwd := t.TempDir(), t.TempDir(), t.TempDir()
	file := filepath.Join(moved, "sessions", "-proj", "2026_abc.jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(moved, "terminal-sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moved, "terminal-sessions", "ttys011"), []byte(cwd+"\n"+file+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := harness.Env{Cwd: cwd, Argv: []string{"omp"}, Environ: []string{"PI_CODING_AGENT_DIR=" + moved},
		Vars: map[string]string{source.VarHome: home, source.VarTTY: "/dev/ttys011"}}
	harness.ApplyRoots("omp", &env)
	if env.Vars[VarAgent] != moved {
		t.Fatalf("omp_agent = %q, want %q", env.Vars[VarAgent], moved)
	}
	if got := terminalSession(env); got != "-proj/2026_abc.jsonl" {
		t.Errorf("terminalSession = %q, want the record under the moved folder", got)
	}
	// A source may read under the moved folder though it is outside home.
	if _, err := source.Confine(file, env); err != nil {
		t.Errorf("Confine(%s) = %v, want it allowed", file, err)
	}
	if _, err := source.Confine(filepath.Join(t.TempDir(), "x"), env); err == nil {
		t.Error("Confine allowed a path outside every root")
	}
}

// omp moves its sessions and terminal records to the XDG folders once the
// user has migrated (`omp config init-xdg` creates $XDG_DATA_HOME/omp): the
// folder is used only when it exists and no agent folder override is set.
func TestSessionsFollowOmpsXDGLayout(t *testing.T) {
	home, data, state := t.TempDir(), t.TempDir(), t.TempDir()
	env := func(environ ...string) harness.Env {
		e := harness.Env{Argv: []string{"omp"}, Environ: environ, Vars: map[string]string{source.VarHome: home}}
		harness.ApplyRoots("omp", &e)
		return e
	}
	xdg := []string{"XDG_DATA_HOME=" + data, "XDG_STATE_HOME=" + state}
	// Not migrated: the XDG folders hold no omp folder.
	if e := env(xdg...); e.Vars[VarSessions] != filepath.Join(home, ".omp", "agent", "sessions") {
		t.Errorf("before migration: sessions = %q", e.Vars[VarSessions])
	}
	for _, d := range []string{filepath.Join(data, "omp"), filepath.Join(state, "omp"), filepath.Join(data, "omp", "profiles", "work")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	e := env(xdg...)
	if e.Vars[VarSessions] != filepath.Join(data, "omp", "sessions") || e.Vars[VarTerminal] != filepath.Join(state, "omp", "terminal-sessions") {
		t.Errorf("migrated: sessions %q, terminal %q", e.Vars[VarSessions], e.Vars[VarTerminal])
	}
	if e.Vars[VarAgent] != filepath.Join(home, ".omp", "agent") {
		t.Errorf("config stays in the agent folder: %q", e.Vars[VarAgent])
	}
	// A source may read the moved sessions folder though it is outside home.
	found := false
	for _, r := range e.Roots {
		found = found || r == filepath.Join(data, "omp", "sessions")
	}
	if !found {
		t.Errorf("the sessions folder is not a readable root: %q", e.Roots)
	}
	// A profile uses its own XDG folder, and only where it exists.
	p := env(append(xdg, "OMP_PROFILE=work")...)
	if p.Vars[VarSessions] != filepath.Join(data, "omp", "profiles", "work", "sessions") {
		t.Errorf("profile sessions = %q", p.Vars[VarSessions])
	}
	if p.Vars[VarTerminal] != filepath.Join(home, ".omp", "profiles", "work", "agent", "terminal-sessions") {
		t.Errorf("profile terminal records (state not migrated for it) = %q", p.Vars[VarTerminal])
	}
	// An agent folder override keeps everything under it.
	o := env(append(xdg, "PI_CODING_AGENT_DIR=/srv/agent")...)
	if o.Vars[VarSessions] != "/srv/agent/sessions" {
		t.Errorf("override sessions = %q", o.Vars[VarSessions])
	}
}
