package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/session"
)

// A session removed while the helper was down leaves its follow files:
// the next start removes them. A live session's are kept, and so are
// ones written just now for a session the list does not name yet.
func TestSweepFollowsRemovesUnknownSessions(t *testing.T) {
	home := t.TempDir()
	save := func(id string, age time.Duration) {
		env := harness.Env{SessionID: id, Vars: map[string]string{source.VarTogoHome: home}}
		source.SaveFollowed(env, "claude", "/p.jsonl")
		p := filepath.Join(home, "run", "follow-"+id+".claude.json")
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	save("s_livelive01", time.Hour)
	save("s_gonegone01", time.Hour)
	save("s_newnewn01", 0)
	e := &Engine{cfg: Config{TogoHome: home, Now: time.Now,
		Sessions: fakeSessions{"s_livelive01": session.Session{ID: "s_livelive01"}}}}
	e.sweepFollows(context.Background())
	got := source.FollowSessions(home)
	if _, ok := got["s_gonegone01"]; ok || len(got) != 2 {
		t.Fatalf("after the sweep: %v", got)
	}
}
