package source

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A session whose folder was deleted still gets its version command run:
// the process starts in the nearest existing folder instead of failing with
// an ENOENT reported against the program.
func TestRunStderrSurvivesADeletedSessionFolder(t *testing.T) {
	home := t.TempDir()
	cwd := home + "/project"
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cwd); err != nil {
		t.Fatal(err)
	}
	env := harness.Env{Cwd: cwd, Vars: map[string]string{VarHome: home, VarPath: "/usr/bin:/bin"}}
	out, err := RunStderr(context.Background(), []string{"pwd"}, env, false)
	if err != nil {
		t.Fatalf("version-style command in a deleted folder: %v", err)
	}
	if got := strings.TrimSpace(string(out)); !strings.HasSuffix(got, strings.TrimPrefix(home, "/private")) {
		t.Fatalf("ran in %q, want the nearest existing folder %q", got, home)
	}
}
