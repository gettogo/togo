package generic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The composer's / and @ menus, from the phone: the agent's commands, and
// the files of the session's folder, one created by the test among them.
func TestCompletion(t *testing.T) {
	for _, agent := range profiles(t) {
		t.Run(agent, func(t *testing.T) {
			t.Parallel()
			t.Run("commands", func(t *testing.T) {
				t.Parallel()
				if !declare(t, agent, "capability/commands") {
					return
				}
				lv := start(t, agent)
				res, err := lv.r.Complete(harness.CompleteCommand, "")
				if err != nil {
					t.Fatalf("complete: %v", err)
				}
				if len(res.Items) == 0 {
					t.Fatal("no command offered")
				}
				for _, c := range res.Items {
					if !strings.HasPrefix(c.Insert, "/") {
						t.Errorf("command %q does not insert a / command", c.Insert)
					}
				}
			})
			t.Run("files", func(t *testing.T) {
				t.Parallel()
				if !declare(t, agent, "capability/files") {
					return
				}
				lv := start(t, agent)
				name := "parser notes 100%.md"
				if err := os.WriteFile(filepath.Join(lv.r.Opt.Cwd, name), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
				res, err := lv.r.Complete(harness.CompleteFile, "parser")
				if err != nil {
					t.Fatalf("complete: %v", err)
				}
				found := false
				for _, c := range res.Items {
					found = found || strings.Contains(c.Insert, "parser notes 100%.md") || strings.Contains(c.Insert, `parser\ notes`)
				}
				if !found {
					t.Errorf("the folder's file is not offered: %+v", res.Items)
				}
			})
		})
	}
}
