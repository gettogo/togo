package gitx_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gettogo/togo/helper/internal/wire"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/rpc"
)

func TestFilesStayInsideOneFrame(t *testing.T) {
	dir := newRepo(t)
	long := strings.Repeat("d", 200)
	for i := 0; i < 1500; i++ {
		write(t, dir, filepath.Join(long, long, long, strings.Repeat("f", 190)+fmt.Sprintf("%05d", i)), "")
	}
	var res gitx.Files
	if err := call(t, service(dir), "git.files", `{`+sid+`}`, &res); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": res})
	if len(b) > rpc.MaxFrame {
		t.Errorf("response is %d bytes, frame limit %d", len(b), rpc.MaxFrame)
	}
	if !res.Truncated || len(res.Paths) == 0 || len(res.Paths) >= 1500 {
		t.Errorf("truncated=%v with %d paths", res.Truncated, len(res.Paths))
	}
}

func TestDiffStaysInsideOneFrame(t *testing.T) {
	dir := seeded(t)
	write(t, dir, "README.md", strings.Repeat("<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<\n", 20000))
	var d gitx.Diff
	if err := call(t, service(dir), "git.diff", `{`+sid+`}`, &d); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": d})
	if len(b) > rpc.MaxFrame {
		t.Errorf("response is %d bytes, frame limit %d", len(b), rpc.MaxFrame)
	}
	if !d.Truncated || !strings.HasPrefix(d.Diff, "diff --git") {
		t.Errorf("truncated=%v, diff starts %.20q", d.Truncated, d.Diff)
	}
}

// A write that runs out of time must be asked to stop, not killed: git
// removes its lock files when it gets SIGTERM and cannot when it is killed.
func TestTimedOutWriteIsTerminatedNotKilled(t *testing.T) {
	dir := seeded(t)
	write(t, dir, "new.txt", "x\n")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}
	marker := filepath.Join(t.TempDir(), "term")
	bin := fakeGit(t, `case " $* " in *" add "*)
  trap 'kill $p 2>/dev/null; echo term > `+marker+`; exit 143' TERM
  sleep 30 & p=$!
  wait $p;;
*) exec `+real+` "$@";;
esac`)
	g := &gitx.Git{Bin: bin, Timeout: 500 * time.Millisecond}
	s := &gitx.Service{Sessions: writeSessions{cwd: dir}, Git: *g}
	_, err = writeCall(t, s, "git.stage", []string{"new.txt"}, seenAll(t, s, "new.txt"))
	if !errors.Is(err, gitx.ErrTimeout) {
		t.Fatalf("err %v", err)
	}
	if !exists(marker) {
		t.Error("git was killed instead of terminated")
	}
}

func TestCommitBlockedByRefLockIsLocked(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "README.md", "changed\n")
	git(t, dir, "add", "README.md")
	write(t, dir, ".git/refs/heads/main.lock", "")
	err := call(t, s, "git.commit", commitParams("m", false, ""), nil)
	if conflictReason(err) != wire.ConflictLocked {
		t.Errorf("err %v", err)
	}
}
