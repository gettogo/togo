package enginetest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// A rig that has read a session and been closed leaves no goroutine of the
// engine and no child process behind, and the folder its session lives in
// can be removed. The test is serial: nothing else runs engines while it
// counts.
func TestRigCloseLeavesNothingRunning(t *testing.T) {
	cases := []struct {
		agent   string
		profile func() sessiongen.Profile
		sqlite  bool
	}{
		{"omp", func() sessiongen.Profile { return &sessiongen.OMP{TTY: TTY} }, false},
		{"goose", func() sessiongen.Profile { return &sessiongen.Goose{} }, true},
	}
	for _, c := range cases {
		t.Run(c.agent, func(t *testing.T) {
			if c.sqlite && sessiongen.SQLiteBin() == "" {
				t.Skip("no sqlite3 program")
			}
			root, err := os.MkdirTemp("", "togo-rig-close-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			home, _ := filepath.EvalSymlinks(root)
			cwd := filepath.Join(home, "proj")
			start := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Second)
			if _, err := sessiongen.Write(home, c.profile(), sessiongen.Script{Seed: 3, Start: start, Cwd: cwd,
				Turns: []sessiongen.Turn{{Say: 50, Tools: 1, Output: 100}, {Say: 30}}}); err != nil {
				t.Fatal(err)
			}
			goroutines, children := len(Goroutines()), len(Children())
			t.Run("rig", func(t *testing.T) {
				r := New(t, Options{Agent: c.agent, Home: home, Cwd: cwd, Created: start.Add(-30 * time.Second)})
				st := r.SettleUntil(10*time.Second, func(st State) bool { return countKind(st, harness.KindTurn) == 2 })
				if countKind(st, harness.KindTurn) != 2 {
					t.Fatalf("the rig did not read the session: %d turns", countKind(st, harness.KindTurn))
				}
				if len(Goroutines()) <= goroutines {
					t.Fatal("the count sees no engine goroutine while the rig runs: it would never find a leak")
				}
				r.Restart() // a second engine, which the cleanup must also stop
				r.SettleUntil(10*time.Second, func(st State) bool { return countKind(st, harness.KindTurn) == 2 })
			})
			// The rig's cleanup has run: no waiting here.
			if g := Goroutines(); len(g) > goroutines {
				t.Errorf("%d engine goroutines before the rig, %d after it was closed:\n%v", goroutines, len(g), g)
			}
			if kids := Children(); len(kids) > children {
				t.Errorf("%d child processes before the rig, %d after it was closed: %v", children, len(kids), kids)
			}
			if err := os.RemoveAll(root); err != nil {
				t.Errorf("the session's folder cannot be removed: %v", err)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Errorf("the session's folder is still there: %v", err)
			}
		})
	}
}
