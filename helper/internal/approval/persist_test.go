package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

func keptPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "run", "approvals.json")
}

// An approval open before a restart is open after it under the same id,
// options and expiry, and its answer is typed into the pane.
func TestApprovalSurvivesRestart(t *testing.T) {
	path := keptPath(t)
	f := &fakeTmux{screen: menu}
	st := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path})
	pr, _ := detect.Detect(menu)
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	before := st.Pending("")[0]
	st.Flush()
	if before.Snippet == "" {
		t.Fatal("no snippet to leave out")
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("kept file: %v %v", fi, err)
	}
	if di, _ := os.Stat(filepath.Dir(path)); di.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode %v", di.Mode().Perm())
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), before.Snippet) {
		t.Fatal("the screen snippet was written to disk")
	}

	pub := &fakePub{}
	st2 := kept(t, Config{Tmux: f, Pub: pub, Path: path})
	after := st2.Pending("")
	if len(after) != 1 {
		t.Fatalf("%d open after the restart", len(after))
	}
	want := before
	want.Snippet = ""
	if !reflect.DeepEqual(after[0], want) {
		t.Fatalf("after the restart:\n%+v\nbefore:\n%+v", after[0], want)
	}
	if evs := pub.snapshot(); len(evs) == 0 || evs[0].kind != wire.EventApprovalOpen {
		t.Fatalf("the restored approval was not announced: %+v", pub.events)
	}
	// The watcher reads the same prompt again: no new request, and the
	// snippet comes back from the screen.
	st2.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	if got := st2.Pending(""); len(got) != 1 || got[0].RequestID != before.RequestID || got[0].Snippet != before.Snippet {
		t.Fatalf("after the prompt was seen again: %+v", got)
	}
	if err := st2.Answer(context.Background(), before.RequestID, wire.DecisionApprove, wire.ByPhone); err != nil {
		t.Fatalf("answer after the restart: %v", err)
	}
	if len(f.sent) == 0 {
		t.Fatal("nothing was typed")
	}
	st2.Flush()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the answered approval is still kept: %v", err)
	}
}

// A prompt that left the screen while the helper was down was answered at
// the desk: its approval closes once the store is back.
func TestRestoredApprovalClosesWhenItsPromptIsGone(t *testing.T) {
	path := keptPath(t)
	f := &fakeTmux{screen: menu}
	st := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path})
	pr, _ := detect.Detect(menu)
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	st.Flush()
	f.mu.Lock()
	f.screen = "$ "
	f.mu.Unlock()
	pub := &fakePub{}
	st2 := kept(t, Config{Tmux: f, Pub: pub, Path: path})
	eventually(t, "the restored approval closes", func() bool { return len(pub.closed()) == 1 })
	if got := pub.closed(); got[0] != wire.ResolutionDesk || len(st2.Pending("")) != 0 {
		t.Fatalf("closed %v, open %d", got, len(st2.Pending("")))
	}
	st2.Flush()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a closed approval is still kept: %v", err)
	}
}

// Expired approvals, asks and sign-in steps are not kept; a file of a
// newer format or not ours is ignored and replaced.
func TestKeptApprovalsBounds(t *testing.T) {
	path := keptPath(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	f := &fakeTmux{screen: menu}
	st := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path, Clock: clock, TTL: time.Hour})
	st.OpenAsk(context.Background(), wire.AskParams{Title: "Ship?", Options: []string{"Yes", "No"}})
	st.Flush()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("an ask was kept: %v", err)
	}
	pr, _ := detect.Detect(menu)
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	st.Flush()
	mu.Lock()
	now = now.Add(2 * time.Hour)
	mu.Unlock()
	expired := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path, Clock: clock})
	expired.Flush()
	if got := expired.Pending(""); len(got) != 0 {
		t.Fatalf("an expired approval came back: %+v", got)
	}

	for name, body := range map[string]string{"newer": `{"v":2,"approvals":[]}`, "garbage": `{"v":`, "huge": `{"v":1,"approvals":[` + strings.Repeat(" ", persistBytes) + `]}`} {
		_ = os.WriteFile(path, []byte(body), 0o600)
		st := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path})
		if len(st.Pending("")) != 0 {
			t.Fatalf("%s: approvals read", name)
		}
		st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
		st.Flush()
		var in persisted
		b, _ := os.ReadFile(path)
		if json.Unmarshal(b, &in) != nil || in.V != persistVersion || len(in.Approvals) != 1 {
			t.Fatalf("%s: not replaced: %.80s", name, b)
		}
	}
}

// Many sessions opening, answering and closing at once: what is kept is
// exactly what is open.
func TestKeptApprovalsUnderConcurrency(t *testing.T) {
	path := keptPath(t)
	f := &fakeTmux{screen: menu}
	st := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path})
	pr, _ := detect.Detect(menu)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("s_conc%06d", i)
			st.PromptSeen(context.Background(), id, pr)
			if i%3 == 0 {
				st.PromptGone(context.Background(), id)
			}
			if i%5 == 0 {
				st.SessionGone(context.Background(), id)
			}
		}()
	}
	wg.Wait()
	open := st.Pending("")
	st.Flush()
	reloaded := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path})
	again := reloaded.Pending("")
	t.Cleanup(reloaded.Flush)
	if len(again) != len(open) || len(open) == 0 {
		t.Fatalf("%d open, %d kept", len(open), len(again))
	}
	for i := range open {
		if open[i].RequestID != again[i].RequestID {
			t.Fatalf("kept %s, open %s", again[i].RequestID, open[i].RequestID)
		}
	}
}
