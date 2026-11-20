package cline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// /new in the TUI wrote a second session, then the phone resumed the
// first (--id), which Cline rewrote last. After a helper restart the
// sessions feed reads both documents in either order: the chat stays on
// the resumed session, without starting over, because the session it
// followed is remembered under run/.
func TestRestartKeepsTheFollowedSession(t *testing.T) {
	h, doc := home(t)
	interactive(t, h, fixtureID)
	const other = "1794200000003_nopid"
	newer := addSession(t, h, other, 0, doc)
	interactive(t, h, other)
	resumed := []byte(strings.Replace(string(doc), "2026-11-08T03:38:03.002Z", "2026-11-08T05:02:00.000Z", 1))
	if err := os.WriteFile(filepath.Join(h, ".cline", "data", "sessions", fixtureID, fixtureID+".messages.json"), resumed, 0o600); err != nil {
		t.Fatal(err)
	}
	e := clineEnv(h, fakeProcs{tree: []int{100}})
	e.SessionID = "s_clinefol1"
	e.Vars[source.VarTogoHome] = t.TempDir()

	m := newMessages(e)
	m.Map(harness.Record{Data: newer})
	m.Relaunched([]string{"cline", "-i", "--id", fixtureID}, time.Now())
	m.Map(harness.Record{Data: resumed})
	if got := m.State()["harness_session"]; got != fixtureID {
		t.Fatalf("before the restart: %q", got)
	}

	for _, order := range [][][]byte{{newer, resumed}, {resumed, newer}} {
		after := newMessages(e) // the helper restarted; Cline did not
		var mapped int
		for _, d := range order {
			items, _ := after.Map(harness.Record{Data: d})
			mapped += len(items)
		}
		st := after.State()
		if st["harness_session"] != fixtureID || st[harness.StateMoves] != "" || mapped == 0 {
			t.Fatalf("after the restart: session %q, moves %q, %d items; want %q, no move", st["harness_session"], st[harness.StateMoves], mapped, fixtureID)
		}
	}

	source.RemoveFollows(e.Vars[source.VarTogoHome], e.SessionID)
	if got := newMessages(e).State()["harness_session"]; got != "" {
		t.Fatalf("the removed session's follow outlived it: %q", got)
	}
}

// The phone resumed X (the command line is now `cline --id X`, kept
// across a helper restart), then /new at the desk moved the chat to Y.
// After a restart the chat stays on Y; a later phone resume of another
// session still wins.
func TestRestartPrefersTheFollowedSessionOverTheResumeID(t *testing.T) {
	h, doc := home(t)
	interactive(t, h, fixtureID)
	const y = "1794200000003_nopid"
	newer := addSession(t, h, y, 0, doc)
	interactive(t, h, y)
	if err := os.WriteFile(filepath.Join(h, ".cline", "data", "sessions", y, y+".messages.json"), newer, 0o600); err != nil {
		t.Fatal(err)
	}
	e := clineEnv(h, fakeProcs{tree: []int{100}})
	e.SessionID = "s_clinefol2"
	e.Vars[source.VarTogoHome] = t.TempDir()
	e.Argv = []string{"cline", "-i", "--id", fixtureID}

	m := newMessages(e)
	if got := follows(m, newer, y); got != y {
		t.Fatalf("the /new is not followed: %q", got)
	}
	after := newMessages(e) // restarted, still with --id X on the command line
	if got := after.State()["harness_session"]; got != y {
		t.Fatalf("after the restart: %q, want the /new session %q", got, y)
	}
	if got := source.LoadFollowed(e, followName); got != y {
		t.Fatalf("the restart overwrote the kept session with %q", got)
	}
	after.Relaunched([]string{"cline", "-i", "--id", fixtureID}, time.Now())
	if got := newMessages(e).State()["harness_session"]; got != fixtureID {
		t.Fatalf("a later resume relaunch did not win: %q", got)
	}
}
