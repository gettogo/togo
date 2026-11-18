package surface

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
)

func pruneFixture(t *testing.T, c *Companions) CompanionConfig {
	t.Helper()
	launch, err := c.PrepareLaunch(context.Background(), "s_abcdefghij", "omp", session.Launch{Program: "omp"})
	if err != nil {
		t.Fatal(err)
	}
	return preparedCompanion(t, launch)
}

func pruneState(t *testing.T, config CompanionConfig, pid int) {
	t.Helper()
	if err := writeJSON(filepath.Join(filepath.Dir(config.Path), "state.json"), CompanionState{
		Version: 1, Generation: config.Generation, PID: pid, Pane: "%1", Conversation: "one", Available: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPruneRetainsLiveGenerationAndReclaimsADeadProcess(t *testing.T) {
	c := testCompanions(t)
	live, dead := pruneFixture(t, c), pruneFixture(t, c)
	pruneState(t, live, os.Getpid())
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	pruneState(t, dead, cmd.Process.Pid)
	if err := c.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(live.Path); err != nil {
		t.Fatal("live generation removed", err)
	}
	if _, err := os.Stat(filepath.Dir(dead.Path)); !os.IsNotExist(err) {
		t.Fatal("dead generation retained", err)
	}
}

func TestPrunePreservesFreshPendingAndMalformedState(t *testing.T) {
	c := testCompanions(t)
	fresh, expired, malformed := pruneFixture(t, c), pruneFixture(t, c), pruneFixture(t, c)
	old := time.Now().Add(-2 * pendingCompanionGrace)
	if err := os.Chtimes(expired.Path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(malformed.Path), "state.json"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, config := range []CompanionConfig{fresh, malformed} {
		if _, err := os.Stat(config.Path); err != nil {
			t.Fatal("recoverable generation removed", err)
		}
	}
	if _, err := os.Stat(expired.Path); !os.IsNotExist(err) {
		t.Fatal("expired unstarted generation retained", err)
	}
}

func TestPruneEpochsKeepsCurrentFutureAndSymlinkTargets(t *testing.T) {
	c := testCompanions(t)
	config := pruneFixture(t, c)
	root := filepath.Dir(config.Path)
	for _, name := range []string{"cache-0", "cache-2", "cache-3", "cache-01", "other"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "keep")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "cache-1")); err != nil {
		t.Fatal(err)
	}
	if err := c.pruneEpochs(context.Background(), config, 2); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cache-0"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatal("retired epoch retained", name, err)
		}
	}
	for _, name := range []string{"cache-1", "cache-2", "cache-3", "cache-01", "other"} {
		if _, err := os.Lstat(filepath.Join(root, name)); err != nil {
			t.Fatal("protected entry removed", name, err)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("symlink target touched", err)
	}
}

func TestPruneCancellationAndForeignSymlinkDoNotRemoveData(t *testing.T) {
	c := testCompanions(t)
	config := pruneFixture(t, c)
	outside := t.TempDir()
	foreign := filepath.Join(outside, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(foreign, "config.json")
	if err := writeJSON(configPath, CompanionConfig{Version: 1, ID: "s_zzzzzzzzzz", Generation: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Token: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * pendingCompanionGrace)
	if err := os.Chtimes(configPath, old, old); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(c.Home, "run", "surfaces", "s_zzzzzzzzzz")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Prune(ctx); err == nil {
		t.Fatal("cancelled cleanup continued")
	}
	if _, err := os.Stat(config.Path); err != nil {
		t.Fatal("cancelled cleanup removed data", err)
	}
	if err := c.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatal("foreign symlink removed", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatal("cleanup traversed foreign symlink", err)
	}
}
