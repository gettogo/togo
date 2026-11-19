package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// storeEngine is a fake-adapter engine (hooks only) with a run folder.
func storeEngine(t *testing.T, home, togoHome string, notes *atomic.Int32, ids ...string) *Engine {
	t.Helper()
	bin := fakeBin(t, "1.2.3")
	sessions := fakeSessions{}
	for _, id := range ids {
		sessions[id] = session.Session{ID: id, Agent: "fake", Managed: true, Cwd: home}
	}
	pane := &fakePane{mode: "a"}
	count := func(string, string, string) { notes.Add(1) }
	return New(Config{
		Sessions: sessions, Typist: pane, Screen: pane.Screen, Home: home, TogoHome: togoHome,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		NeedsYou:  count, Done: count, Scale: 0.2,
	})
}

func turnRecord(i int, state string) map[string]any {
	turn := fmt.Sprintf("t%03d", i)
	return map[string]any{"state": map[string]string{"working": fmt.Sprint(state == harness.StateRunning), "turn": turn},
		"items": []harness.Item{
			{ID: turn + ":user", TurnID: turn, Kind: harness.KindUser, User: &harness.UserBody{Text: "prompt " + turn}},
			{ID: turn, TurnID: turn, Kind: harness.KindTurn, Turn: &harness.TurnBody{State: state}},
		}}
}

func itemView(items []harness.Item) []string {
	var out []string
	for _, it := range items {
		state := ""
		if it.Turn != nil {
			state = it.Turn.State
		}
		out = append(out, it.ID+"|"+it.Kind+"|"+state)
	}
	return out
}

// A hook-only session's chat is the same after a restart: two sessions
// written at once, each restored from its own file, nothing announced
// again, and the file gone with its session.
func TestHookChatSurvivesRestart(t *testing.T) {
	ids := []string{"s_hookkeep01", "s_hookkeep02"}
	home, togoHome := t.TempDir(), t.TempDir()
	ctx := context.Background()
	var notes atomic.Int32
	e1 := storeEngine(t, home, togoHome, &notes, ids...)
	for _, id := range ids {
		if _, err := e1.served(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	const turns = 30
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range turns {
				deliver(id, turnRecord(i, harness.StateRunning))
				if i < turns-1 {
					deliver(id, turnRecord(i, harness.StateOK))
				}
			}
		}()
	}
	wg.Wait()
	before := map[string][]string{}
	for _, id := range ids {
		waitFor(t, func() bool {
			it, ok := e1.log.Get(id, fmt.Sprintf("t%03d", turns-1))
			return ok && it.Turn.State == harness.StateRunning
		})
		items, _, _ := e1.log.Since(id, 0, 0)
		before[id] = itemView(items)
		e1.Unload(id)
	}
	path := hookStorePath(togoHome, ids[0])
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("store file: %v %v", st, err)
	}
	if dir, _ := os.Stat(filepath.Dir(path)); dir.Mode().Perm() != 0o700 {
		t.Fatalf("run folder mode %v", dir.Mode().Perm())
	}

	notes.Store(0)
	e2 := storeEngine(t, home, togoHome, &notes, ids...)
	for _, id := range ids {
		res, err := e2.Open(ctx, &fakeSub{done: make(chan struct{})}, id, 0, 500)
		if err != nil {
			t.Fatal(err)
		}
		if got := itemView(res.Items); strings.Join(got, "\n") != strings.Join(before[id], "\n") {
			t.Fatalf("%s after the restart:\n%s\nbefore:\n%s", id, strings.Join(got, "\n"), strings.Join(before[id], "\n"))
		}
		if !res.Mission.Summary.Working {
			t.Errorf("%s: the running turn is not running after the restart", id)
		}
	}
	// A live record after the replay goes on from it.
	deliver(ids[0], turnRecord(turns-1, harness.StateOK))
	waitFor(t, func() bool {
		it, ok := e2.log.Get(ids[0], fmt.Sprintf("t%03d", turns-1))
		return ok && it.Turn.State == harness.StateOK
	})
	if n := notes.Load(); n > 1 {
		t.Errorf("%d notifications after the restart; the replay must announce nothing", n)
	}
	for _, id := range ids {
		e2.Forget(id)
		if _, err := os.Stat(hookStorePath(togoHome, id)); !os.IsNotExist(err) {
			t.Errorf("%s: the store outlived its session: %v", id, err)
		}
	}
}

func line(i int, size int) hookLine {
	data, _ := json.Marshal(map[string]any{"i": i, "pad": strings.Repeat("x", size)})
	return hookLine{Feed: "events", At: time.Unix(int64(1_800_000_000+i), 0).UTC(), Data: data,
		Pre: json.RawMessage(fmt.Sprintf(`{"n":%d}`, i)), Post: json.RawMessage(fmt.Sprintf(`{"n":%d}`, i+1))}
}

func indexOf(t *testing.T, l hookLine) int {
	t.Helper()
	var v struct{ I int }
	if err := json.Unmarshal(l.Data, &v); err != nil {
		t.Fatal(err)
	}
	return v.I
}

func TestHookStoreRoundTrip(t *testing.T) {
	path := hookStorePath(t.TempDir(), "s_store01")
	h := &hookStore{path: path}
	for i := range 5 {
		if err := h.add(line(i, 10)); err != nil {
			t.Fatal(err)
		}
	}
	got := (&hookStore{path: path}).load(time.Now())
	if len(got) != 5 || indexOf(t, got[4]) != 4 || !got[2].At.Equal(line(2, 0).At) || string(got[3].Post) != `{"n":4}` {
		t.Fatalf("loaded %+v", got)
	}
	rec, ok := replayRecord("events", got)
	if !ok || len(rec.Replay) != 5 || len(rec.ReplayAt) != 5 || len(rec.ReplayMarks) != 5 || string(rec.ReplayMarks[0].Pre) != `{"n":0}` {
		t.Fatalf("replay %+v", rec)
	}
	if _, ok := replayRecord("other", got); ok {
		t.Fatal("another feed's replay is not empty")
	}
}

// A line torn by a crash is cut; what follows it is read.
func TestHookStoreTornLine(t *testing.T) {
	path := hookStorePath(t.TempDir(), "s_store02")
	h := &hookStore{path: path}
	for i := range 3 {
		_ = h.add(line(i, 10))
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"feed":"events","at":"2026-`)
	f.Close()
	h = &hookStore{path: path}
	if got := h.load(time.Now()); len(got) != 3 {
		t.Fatalf("%d records", len(got))
	}
	_ = h.add(line(3, 10))
	got := (&hookStore{path: path}).load(time.Now())
	if len(got) != 4 || indexOf(t, got[3]) != 3 {
		t.Fatalf("after the torn line: %d records", len(got))
	}
	// A line that is not JSON in the middle is skipped.
	b, _ := os.ReadFile(path)
	lines := strings.SplitAfter(string(b), "\n")
	lines[2] = "garbage\n"
	_ = os.WriteFile(path, []byte(strings.Join(lines, "")), 0o600)
	if got := (&hookStore{path: path}).load(time.Now()); len(got) != 3 {
		t.Fatalf("with a bad line: %d records", len(got))
	}
}

// A file of a newer format, or not one of ours, is ignored and replaced.
func TestHookStoreOtherFormats(t *testing.T) {
	for name, head := range map[string]string{"newer": `{"togo_hooks":2}`, "garbage": `not json`, "empty": ``} {
		path := hookStorePath(t.TempDir(), "s_store03")
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		body := head + "\n" + `{"feed":"events","at":"2026-11-18T00:00:00Z","data":{"i":9}}` + "\n"
		_ = os.WriteFile(path, []byte(body), 0o600)
		h := &hookStore{path: path}
		if got := h.load(time.Now()); len(got) != 0 {
			t.Fatalf("%s: read %d records", name, len(got))
		}
		_ = h.add(line(1, 10))
		got := (&hookStore{path: path}).load(time.Now())
		if len(got) != 1 || indexOf(t, got[0]) != 1 {
			t.Fatalf("%s: replaced file holds %d records", name, len(got))
		}
	}
}

// Past either bound the oldest are cut, by count and by size.
func TestHookStoreBounds(t *testing.T) {
	path := hookStorePath(t.TempDir(), "s_store04")
	h := &hookStore{path: path}
	for i := range hookKeep + 1 {
		if err := h.add(line(i, 0)); err != nil {
			t.Fatal(err)
		}
	}
	got := (&hookStore{path: path}).load(time.Now())
	if len(got) != hookKeep*3/4 || indexOf(t, got[len(got)-1]) != hookKeep {
		t.Fatalf("count bound: %d records, last %d", len(got), indexOf(t, got[len(got)-1]))
	}

	path = hookStorePath(t.TempDir(), "s_store05")
	h = &hookStore{path: path}
	const big = 256 << 10
	n := hookKeepBytes/big + 2
	for i := range n {
		if err := h.add(line(i, big)); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := os.Stat(path)
	if st.Size() > hookKeepBytes {
		t.Fatalf("size bound: %d bytes", st.Size())
	}
	got = (&hookStore{path: path}).load(time.Now())
	if len(got) == 0 || indexOf(t, got[len(got)-1]) != n-1 {
		t.Fatalf("size bound: %d records", len(got))
	}
	if err := h.add(line(0, hookKeepBytes/4)); err == nil {
		t.Fatal("a record over a quarter of the size bound was kept")
	}
}

// A file not written for longer than the age bound is dropped, at load
// and by the sweep a new engine runs.
func TestHookStoreAge(t *testing.T) {
	togoHome := t.TempDir()
	path := hookStorePath(togoHome, "s_store06")
	h := &hookStore{path: path}
	_ = h.add(line(1, 10))
	old := time.Now().Add(-hookKeepAge - time.Hour)
	_ = os.Chtimes(path, old, old)
	if got := (&hookStore{path: path}).load(time.Now()); len(got) != 0 {
		t.Fatalf("an old file gave %d records", len(got))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("old file kept: %v", err)
	}
	fresh, stale := hookStorePath(togoHome, "s_store07"), hookStorePath(togoHome, "s_store08")
	_ = (&hookStore{path: fresh}).add(line(1, 10))
	_ = (&hookStore{path: stale}).add(line(1, 10))
	_ = os.Chtimes(stale, old, old)
	other := filepath.Join(togoHome, "run", "follow-s_store08.json")
	_ = os.WriteFile(other, []byte(`{}`), 0o600)
	_ = os.Chtimes(other, old, old)
	sweepHookStores(togoHome, time.Now())
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh file swept: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file kept: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("another kind of file swept: %v", err)
	}
}

func TestHookStorePath(t *testing.T) {
	for _, id := range []string{"", "../x", ".hidden", "a/b"} {
		if p := hookStorePath("/home/u/.togo", id); p != "" {
			t.Errorf("%q: %s", id, p)
		}
	}
	if p := hookStorePath("", "s_x"); p != "" {
		t.Errorf("no run folder: %s", p)
	}
}

// The file is never read or written through a symbolic link.
func TestHookStoreRefusesALink(t *testing.T) {
	togoHome := t.TempDir()
	path := hookStorePath(togoHome, "s_store09")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	target := filepath.Join(t.TempDir(), "elsewhere")
	_ = os.WriteFile(target, []byte("{\"togo_hooks\":1}\n"+string(mustLine(t, line(1, 10)))), 0o600)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	h := &hookStore{path: path}
	if got := h.load(time.Now()); len(got) != 0 {
		t.Fatalf("read %d records through a link", len(got))
	}
	_ = os.Symlink(target, path)
	before, _ := os.ReadFile(target)
	if err := h.add(line(2, 10)); err == nil {
		t.Fatal("wrote through a link")
	}
	if after, _ := os.ReadFile(target); string(after) != string(before) {
		t.Fatal("the link's target changed")
	}
}

func mustLine(t *testing.T, l hookLine) []byte {
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}
