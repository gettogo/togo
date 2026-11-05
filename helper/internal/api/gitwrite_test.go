package api_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// gitFixtures are the fixtures whose ~/proj is a git repository, each with
// the change its description names on top of the shared start: branch main,
// an identity in the repository config, and one commit adding README.md.
var gitFixtures = map[string]func(t *testing.T, proj string){
	"git.commit.json": func(t *testing.T, proj string) {
		writeFile(t, proj, "README.md", "hello again\n")
		runGit(t, proj, "add", "README.md")
	},
	"git.commit-nothing.json": func(*testing.T, string) {},
	"git.diff-commit.json": func(t *testing.T, proj string) {
		writeFile(t, proj, "README.md", "hello again\n")
		runGit(t, proj, "commit", "-qam", "again")
	},
	"git.discard-changed.json": func(t *testing.T, proj string) {
		writeFile(t, proj, "README.md", "hello again\n")
	},
	"git.discard-folder.json": func(t *testing.T, proj string) {
		writeFile(t, proj, "newpkg/a.txt", "hello\n")
	},
	"git.discard-stale.json": func(*testing.T, string) {},
	"git.discard.json": func(t *testing.T, proj string) {
		writeFile(t, proj, "README.md", "hello again\n")
	},
	"git.stage-untracked.json": func(t *testing.T, proj string) {
		writeFile(t, proj, "newpkg/a.txt", "hello\n")
	},
	"git.stage.json": func(t *testing.T, proj string) {
		writeFile(t, proj, "README.md", "hello again\n")
	},
	"git.unstage.json": func(t *testing.T, proj string) {
		writeFile(t, proj, "README.md", "hello again\n")
		runGit(t, proj, "add", "README.md")
	},
}

func init() {
	for name := range gitFixtures {
		fixtureNeedsFake[name] = true
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = gitx.Env()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGitWriteFixtures(t *testing.T) {
	for name, setup := range gitFixtures {
		t.Run(strings.TrimSuffix(name, ".json"), func(t *testing.T) {
			a, home := harness(t, tmuxtest.New(t))
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			proj := filepath.Join(home, "proj")
			runGit(t, proj, "init", "-q", "-b", "main")
			runGit(t, proj, "config", "user.name", "t")
			runGit(t, proj, "config", "user.email", "t@example.com")
			writeFile(t, proj, "README.md", "hello\n")
			runGit(t, proj, "add", "README.md")
			runGit(t, proj, "commit", "-qm", "init")
			setup(t, proj)
			runFixture(t, a, home, filepath.Join(fixturesDir, name))
		})
	}
}
