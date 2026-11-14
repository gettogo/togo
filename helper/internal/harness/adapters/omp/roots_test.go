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
