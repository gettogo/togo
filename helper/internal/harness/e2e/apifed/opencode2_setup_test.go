package apifed

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// OpenCode 2.x: the engine starts the agent's server itself (a stand-in
// process that passes to the rig's APIServer, enginetest/standin.go), the
// pane is the terminal prompts are typed into, and the history comes from
// the paged messages route. sessiongen.OpenCode2 writes the stream.

// oc2 is a generated 2.x session served by the rig.
type oc2 struct {
	e   support.Env
	p   *sessiongen.OpenCode2
	l   *sessiongen.Layout
	r   *enginetest.Rig
	api *enginetest.APIServer
	mu  sync.Mutex // writes from the terminal's submit hook and the test
}

func oc2Start(t *testing.T, seed int64, mod func(*enginetest.Options), turns ...sessiongen.Turn) *oc2 {
	t.Helper()
	e := support.NewEnv(t, 10*time.Minute)
	p := &sessiongen.OpenCode2{}
	l, err := sessiongen.Write(e.Home, p, sessiongen.Script{Seed: seed, Start: time.Now().UTC(), Gap: time.Millisecond, Cwd: e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	o := enginetest.Options{Agent: "opencode2", NoRedact: true}
	if mod != nil {
		mod(&o)
	}
	r := e.RigWith(t, o)
	s := &oc2{e: e, p: p, l: l, r: r, api: r.API()}
	if s.api == nil || r.ServerPort() == 0 {
		t.Fatalf("no 2.x server runs (port %d)", r.ServerPort())
	}
	return s
}

// add writes more turns, as the agent working.
func (s *oc2) add(t *testing.T, turns ...sessiongen.Turn) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	support.Now(s.l)
	if err := s.l.Append(turns...); err != nil {
		t.Fatal(err)
	}
}

// do runs f on the profile under the lock, the clock at the present.
func (s *oc2) do(t *testing.T, f func() error) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	support.Now(s.l)
	if err := f(); err != nil {
		t.Fatal(err)
	}
}

// answerTyped makes the agent run a short turn for each prompt typed into
// its terminal, in the session the terminal shows, as the real one does.
func (s *oc2) answerTyped(t *testing.T) {
	s.r.Term.OnSubmit = func(text string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		support.Now(s.l)
		_ = s.l.Append(sessiongen.Turn{Prompt: text, Say: 12})
	}
}

// posts lists the non-GET calls whose path, without its query, ends in
// suffix.
func (s *oc2) posts(suffix string) []enginetest.APICall {
	var out []enginetest.APICall
	for _, c := range s.api.Posts() {
		if strings.HasSuffix(strings.SplitN(c.Path, "?", 2)[0], suffix) {
			out = append(out, c)
		}
	}
	return out
}

// waitPost waits for a call to a path ending in suffix.
func (s *oc2) waitPost(t *testing.T, suffix string) enginetest.APICall {
	t.Helper()
	c, ok := s.api.WaitCall(5*time.Second, func(c enginetest.APICall) bool {
		return c.Method != "GET" && strings.HasSuffix(strings.SplitN(c.Path, "?", 2)[0], suffix)
	})
	if !ok {
		t.Fatalf("no call to %s: %+v", suffix, s.api.Posts())
	}
	return c
}

// conversation is the session the chat follows, as the engine keeps it
// for a helper restart (run/harness/<session>.conversation).
func (s *oc2) conversation() string {
	b, _ := os.ReadFile(filepath.Join(s.r.TogoHome, "run", "harness", s.r.ID+".conversation"))
	return strings.TrimSpace(string(b))
}

// textsOf lists the user prompts' texts in the chat, oldest first.
func textsOf(st enginetest.State) []string {
	var out []string
	for _, it := range support.UserPrompts(st) {
		out = append(out, it.User.Text)
	}
	return out
}

// shown reports whether the chat holds a prompt with this text.
func shown(text string) func(enginetest.State) bool {
	return func(st enginetest.State) bool {
		for _, it := range support.UserPrompts(st) {
			if it.User.Text == text {
				return true
			}
		}
		return false
	}
}

// historyReads lists the messages route calls the helper made.
func (s *oc2) historyReads() []enginetest.APICall {
	var out []enginetest.APICall
	for _, c := range s.api.Calls() {
		if c.Method == "GET" && strings.Contains(c.Path, "/message?") {
			out = append(out, c)
		}
	}
	return out
}

// noticeWith reports a notice whose text holds sub.
func noticeWith(st enginetest.State, sub string) bool {
	for _, it := range support.Items(st, support.OfKind(harness.KindNotice)) {
		if strings.Contains(it.Notice.Text, sub) {
			return true
		}
	}
	return false
}
