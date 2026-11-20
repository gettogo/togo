package filefed

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
)

var (
	piISO   = regexp.MustCompile(`"(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3})Z"`)
	piMilli = regexp.MustCompile(`"timestamp":(1\d{12})`)
	piHead  = regexp.MustCompile(`^\{"type":"session","version":3,"id":"([^"]+)","timestamp":"([^"]+)"`)
)

// piRecorded puts a recorded Pi session file (Pi 0.73.1) in the folder Pi
// keeps it in, moved in time so that its first line is at start, and in
// the folder of the session (the recording's /Users/dev/proj). It returns
// the file's path.
func piRecorded(t *testing.T, e support.Env, name string, start time.Time) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "adapters", "pisession", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	m := piHead.FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s: no session header", name)
	}
	was, err := time.Parse(time.RFC3339Nano, string(m[2]))
	if err != nil {
		t.Fatal(err)
	}
	by := start.Sub(was)
	out := piISO.ReplaceAllFunc(b, func(s []byte) []byte {
		at, err := time.Parse("2006-01-02T15:04:05.000Z", strings.Trim(string(s), `"`))
		if err != nil {
			t.Fatal(err)
		}
		return []byte(`"` + at.Add(by).UTC().Format("2006-01-02T15:04:05.000Z") + `"`)
	})
	out = piMilli.ReplaceAllFunc(out, func(s []byte) []byte {
		ms, _ := strconv.ParseInt(string(s[len(`"timestamp":`):]), 10, 64)
		return []byte(`"timestamp":` + strconv.FormatInt(ms+by.Milliseconds(), 10))
	})
	out = []byte(strings.ReplaceAll(string(out), "/Users/dev/proj", e.Cwd))
	dir := filepath.Join(e.Home, ".pi", "agent", "sessions", "-"+strings.ReplaceAll(e.Cwd, "/", "-")+"--")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, strings.ReplaceAll(start.UTC().Format("2006-01-02T15-04-05.000Z"), ".", "-")+"_"+string(m[1])+".jsonl")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// /fork in Pi (a live 0.73.1 run): Pi copies the conversation up to the
// message chosen into a new file, and writes it when the first message is
// sent in it. Until then the chat shows the parent. Once the fork's file
// is written the chat starts over on it, and what it shows is the fork's
// own message and reply, not the parent's.
func TestPiForkStartsTheChatOverOnTheNewFile(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "tape", "pi/lifecycle/fork")
	e := support.NewEnv(t, 10*time.Minute)
	// The conversation's file is there when the session begins, as Pi's is.
	begun := time.Now()
	piRecorded(t, e, "fork-parent.jsonl", begun)
	r := e.RigMod(t, "pi", func(o *enginetest.Options) {
		o.PanePIDs = support.ProcsOf(t)
		o.Created = begun.Add(-time.Second)
	})
	const ask = "Reply with the single word ok. Do not use any tools."
	st := r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		return len(support.UserPrompts(st)) == 1 && support.CountKind(st, harness.KindAssistant) == 1 && !st.Mission.Summary.Working
	})
	if p := support.UserPrompts(st)[0]; p.User.Text != ask || p.ID != "7f163569" {
		t.Fatalf("the parent's prompt %q (%s)", p.User.Text, p.ID)
	}
	// The fork's file appears, written since the engine began to watch.
	piRecorded(t, e, "fork-forked.jsonl", time.Now())
	after := r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		for _, it := range st.Items {
			if it.User != nil && it.ID == "b4197d45" {
				return true
			}
		}
		return false
	})
	if after.Epoch == st.Epoch {
		t.Error("the chat did not start over on the fork's file")
	}
	for _, it := range after.Items {
		if strings.Contains(it.ID, "7f163569") || strings.Contains(it.TurnID, "7f163569") {
			t.Errorf("item %s of the parent is still shown after the fork", it.ID)
		}
	}
	if n := len(support.UserPrompts(after)); n != 1 {
		t.Errorf("%d prompts after the fork, want the fork's one", n)
	}
	if n := support.CountKind(after, harness.KindAssistant); n != 1 {
		t.Errorf("%d replies after the fork, want 1", n)
	}
}
