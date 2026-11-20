package source

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A torn or foreign follow file is ignored and removed, and the sessions
// with something remembered are listed by id.
func TestFollowFilesGarbageAndListing(t *testing.T) {
	home := t.TempDir()
	env := harness.Env{SessionID: "s_garbage01", Vars: map[string]string{VarTogoHome: home}}
	SaveFollowed(env, "claude", "/x.jsonl")
	saveFollow(env, "/y.jsonl")
	SaveFollowed(harness.Env{SessionID: "s_other0001", Vars: env.Vars}, "codex", "t1")
	got := FollowSessions(home)
	if len(got) != 2 || got["s_garbage01"].IsZero() || got["s_other0001"].IsZero() {
		t.Fatalf("listed %v", got)
	}
	run := filepath.Join(home, "run")
	for name, body := range map[string]string{
		"follow-s_garbage01.claude.json": `{"id":"/x.js`, // torn
		"follow-s_garbage01.json":        "garbage",
	} {
		if err := os.WriteFile(filepath.Join(run, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if LoadFollowed(env, "claude") != "" || loadFollow(env) != "" {
		t.Fatal("a torn file was read")
	}
	for _, name := range []string{"follow-s_garbage01.claude.json", "follow-s_garbage01.json"} {
		if _, err := os.Stat(filepath.Join(run, name)); !os.IsNotExist(err) {
			t.Errorf("%s was kept: %v", name, err)
		}
	}
}
