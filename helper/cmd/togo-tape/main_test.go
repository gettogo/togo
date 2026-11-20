package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
	"github.com/gettogo/togo/helper/internal/testtmp"
)

func TestMain(m *testing.M) {
	enginetest.StandIn()
	os.Exit(testtmp.Main(m))
}

func TestPickExpandsAllAndNames(t *testing.T) {
	all, err := pick("all", "all")
	if err != nil {
		t.Fatal(err)
	}
	if want := len(ids()) * len(sessiongen.Names()); len(all) != want {
		t.Errorf("%d pairs, want %d", len(all), want)
	}
	one, err := pick("interrupt", "omp")
	if err != nil || len(one) != 1 || one[0].agent != "omp" || one[0].sc.ID != "interrupt" {
		t.Errorf("one pair: %+v, %v", one, err)
	}
	for _, bad := range [][2]string{{"nope", "all"}, {"all", "nope"}} {
		if _, err := pick(bad[0], bad[1]); err == nil {
			t.Errorf("pick(%v) did not fail", bad)
		}
	}
}

// Without a revision nothing is written and the command says why.
func TestRefusesWithoutRevision(t *testing.T) {
	t.Setenv("TOGO_TAPE_REVISION", "")
	out := t.TempDir()
	if code := run([]string{"-scenario", "interrupt", "-agent", "omp", "-out", out}); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if files, _ := filepath.Glob(filepath.Join(out, "*", "*")); len(files) != 0 {
		t.Errorf("wrote %v", files)
	}
	if code := run([]string{"-scenario", "interrupt"}); code != 2 {
		t.Errorf("no -out: exit %d, want 2", code)
	}
}

// -pilot writes the omp restart tape under the names the app's test
// expects, with the commit it was made from in the manifest.
func TestPilotNames(t *testing.T) {
	head, err := exec.Command("git", "-C", coverage.HelperDir(), "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skip("not a git checkout")
	}
	t.Setenv("TOGO_TAPE_REVISION", strings.TrimSpace(string(head)))
	t.Setenv("TOGO_TAPE_ALLOW_DIRTY", "1")
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	out := t.TempDir()
	if code := run([]string{"-pilot", "-out", out}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, name := range []string{"engine-restart.jsonl", "engine-restart.json"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Error(err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(out, "engine-restart.json"))
	if !strings.Contains(string(raw), strings.TrimSpace(string(head))) {
		t.Errorf("manifest %s does not name the revision", raw)
	}
}
