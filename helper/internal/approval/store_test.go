package approval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

// fakeTmux shows a fixed screen and records keys, for tests that are about
// store bookkeeping rather than typing.
type fakeTmux struct {
	mu     sync.Mutex
	screen string
	sent   [][]string
}

func (f *fakeTmux) ListSessions(context.Context) ([]tmux.SessionInfo, error) {
	return []tmux.SessionInfo{{Name: "s_aaaaaaaaaa", Bib: "03", Agent: "claude"}}, nil
}

func (f *fakeTmux) CapturePane(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.screen, nil
}

func (f *fakeTmux) SendKeys(_ context.Context, _ string, _ bool, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, keys)
	return nil
}

func (f *fakeTmux) PasteText(_ context.Context, _ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, []string{"paste", text})
	return nil
}

const menu = "Do you want to proceed?\n❯ 1. Yes\n  2. No\n"

func TestExpiryClosesApproval(t *testing.T) {
	pub := &fakePub{}
	st := New(Config{Tmux: &fakeTmux{}, Pub: pub, TTL: 30 * time.Millisecond})
	a, ch := st.OpenAsk(context.Background(), wire.AskParams{Title: "Ship it?", Options: []string{"Yes", "No"}})
	select {
	case d, ok := <-ch:
		if ok {
			t.Fatalf("ask received %q, want close on expiry", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approval never expired")
	}
	if got := pub.closed(); len(got) != 1 || got[0] != wire.ResolutionExpired {
		t.Fatalf("closed events = %v, want [expired]", got)
	}
	wantConflict(t, st.Answer(context.Background(), a.RequestID, "option:1", wire.ByPhone), wire.ConflictExpired, "")
}

func TestAnswerPastExpiresAtIsExpired(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	f := &fakeTmux{screen: menu}
	st := New(Config{Tmux: f, Pub: &fakePub{}, Clock: clock, TTL: time.Hour})
	pr, _ := detect.Detect(menu)
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	a := st.Pending("")[0]
	mu.Lock()
	now = now.Add(time.Hour)
	mu.Unlock()
	wantConflict(t, st.Answer(context.Background(), a.RequestID, "approve", wire.ByPhone), wire.ConflictExpired, "")
	if len(f.sent) != 0 {
		t.Fatalf("keys sent after expiry: %v", f.sent)
	}
}

func TestAskReceivesChosenLabelAndTypesNothing(t *testing.T) {
	f := &fakeTmux{}
	st := New(Config{Tmux: f, Pub: &fakePub{}})
	sid := "s_aaaaaaaaaa"
	a, ch := st.OpenAsk(context.Background(), wire.AskParams{SessionID: &sid, Title: "Deploy?", Options: []string{"staging", "prod"}})
	if *a.Bib != "03" || a.Source != wire.SourceAsk {
		t.Fatalf("approval = %+v", a)
	}
	if err := st.Answer(context.Background(), a.RequestID, "option:2", wire.ByPhone); err != nil {
		t.Fatal(err)
	}
	if d := <-ch; d != "prod" {
		t.Fatalf("ask received %q, want prod", d)
	}
	if len(f.sent) != 0 {
		t.Fatalf("ask typed keys: %v", f.sent)
	}
}

func TestAskTextNeedsAllowText(t *testing.T) {
	st := New(Config{Tmux: &fakeTmux{}, Pub: &fakePub{}})
	a, _ := st.OpenAsk(context.Background(), wire.AskParams{Title: "Name?"})
	wantInvalid(t, st.Answer(context.Background(), a.RequestID, "text:x", wire.ByPhone))
	b, ch := st.OpenAsk(context.Background(), wire.AskParams{Title: "Name?", AllowText: true})
	if err := st.Answer(context.Background(), b.RequestID, "text:release 2", wire.ByPhone); err != nil {
		t.Fatal(err)
	}
	if d := <-ch; d != "release 2" {
		t.Fatalf("ask received %q", d)
	}
}

func TestSamePromptSeenTwiceOpensOnce(t *testing.T) {
	pub := &fakePub{}
	st := New(Config{Tmux: &fakeTmux{}, Pub: pub})
	pr, _ := detect.Detect(menu)
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	if n := len(st.Pending("s_aaaaaaaaaa")); n != 1 {
		t.Fatalf("pending = %d, want 1", n)
	}
	other, _ := detect.Detect("Allow this edit?\n❯ 1. Yes\n  2. No\n")
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", other)
	got := st.Pending("s_aaaaaaaaaa")
	if len(got) != 1 || got[0].Title != "Allow this edit?" {
		t.Fatalf("pending = %+v, want only the new prompt", got)
	}
	if c := pub.closed(); len(c) != 1 || c[0] != wire.ResolutionDesk {
		t.Fatalf("closed events = %v, want [desk]", c)
	}
}

func TestHookApprovalTakesOptionsFromScreen(t *testing.T) {
	f := &fakeTmux{screen: menu}
	st := New(Config{Tmux: f, Pub: &fakePub{}})
	a := st.OpenFromHook(context.Background(), "s_aaaaaaaaaa", wire.HookEvent{
		Agent: "claude", Data: wire.HookData{Tool: "Bash", Command: "make build"},
	})
	if a.Source != wire.SourceHook || a.Title != "Bash" || *a.Detail != "make build" || len(a.Options) != 2 {
		t.Fatalf("approval = %+v", a)
	}
	if err := st.Answer(context.Background(), a.RequestID, "approve", wire.ByPhone); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.sent, [][]string{{"1"}}) {
		t.Fatalf("sent %v, want [[1]]", f.sent)
	}
}

func TestHookApprovalAdoptsLaterPrompt(t *testing.T) {
	f := &fakeTmux{}
	st := New(Config{Tmux: f, Pub: &fakePub{}})
	a := st.OpenFromHook(context.Background(), "s_aaaaaaaaaa", wire.HookEvent{Agent: "claude", Data: wire.HookData{Tool: "Bash"}})
	if a.Answerable == nil || *a.Answerable {
		t.Fatalf("a hook request with nothing on screen is answerable: %+v", a)
	}
	var inv *session.InvalidParamsError
	if err := st.Answer(context.Background(), a.RequestID, "deny", wire.ByPhone); !errors.As(err, &inv) {
		t.Fatalf("err = %v, want invalid params", err)
	}

	b := st.OpenFromHook(context.Background(), "s_aaaaaaaaaa", wire.HookEvent{Agent: "claude", Data: wire.HookData{Tool: "Bash"}})
	f.mu.Lock()
	f.screen = menu
	f.mu.Unlock()
	pr, _ := detect.Detect(menu)
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	got := st.Pending("")
	if len(got) != 1 || got[0].RequestID != b.RequestID || len(got[0].Options) != 2 {
		t.Fatalf("pending = %+v, want the hook approval with screen options", got)
	}
	if err := st.Answer(context.Background(), b.RequestID, "deny", wire.ByPhone); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.sent, [][]string{{"2"}}) {
		t.Fatalf("sent %v, want [[2]]", f.sent)
	}
}

// replayFixture runs a protocol fixture against the real API with the
// store's methods registered, after seed has prepared the store.
func replayFixture(t *testing.T, name string, seed func(*Store)) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../../protocol/v1/fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Setup    []json.RawMessage `json:"setup"`
		Request  json.RawMessage   `json:"request"`
		Response json.RawMessage   `json:"response"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	togoHome := t.TempDir()
	recent := &state.Recent{File: filepath.Join(togoHome, "recent.json")}
	tm := tmuxtest.New(t)
	svc := &session.Service{
		Tmux: tm, Recent: recent, Shell: "/bin/sh", Path: t.TempDir(),
		LockFile: filepath.Join(togoHome, "run", "bib.lock"),
		NewID:    func() string { return "s_abcdefghij" },
	}
	st := New(Config{Tmux: tm, Pub: &fakePub{}, NewID: func() string { return "r_abcdefghij" }})
	seed(st)
	a := &api.API{Sessions: svc, Recent: recent, Home: home, Ext: st.Methods()}

	var in bytes.Buffer
	for _, req := range append(append([]json.RawMessage{}, fx.Setup...), fx.Request) {
		if err := rpc.WriteFrame(&in, req); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := rpc.Serve(context.Background(), &in, &out, a, rpc.HelperInfo{Version: "0.0.0-test"}); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var last json.RawMessage
	for out.Len() > 0 {
		if last, err = rpc.ReadFrame(&out); err != nil {
			t.Fatal(err)
		}
	}
	var got, want any
	if err := json.Unmarshal(last, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fx.Response, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("response %s, want %s", last, fx.Response)
	}
}

func TestListFixture(t *testing.T) {
	replayFixture(t, "approval.list.json", func(*Store) {})
}

func TestAnswerConflictFixture(t *testing.T) {
	replayFixture(t, "approval.answer-conflict.json", func(st *Store) {
		pr, _ := detect.Detect(menu)
		st.PromptSeen(context.Background(), "s_abcdefghij", pr)
		st.CloseDesk("r_abcdefghij")
	})
}

func TestHookForWatchedPromptKeepsTheApproval(t *testing.T) {
	pub := &fakePub{}
	st := New(Config{Tmux: &fakeTmux{screen: menu}, Pub: pub})
	pr, _ := detect.Detect(menu)
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	first := st.Pending("")[0]
	a := st.OpenFromHook(context.Background(), "s_aaaaaaaaaa", wire.HookEvent{Agent: "claude", Data: wire.HookData{Tool: "Bash"}})
	if a.RequestID != first.RequestID || a.Source != wire.SourceHook || len(pub.closed()) != 0 {
		t.Fatalf("hook approval = %+v, closed %v; want the watched approval upgraded", a, pub.closed())
	}
}

func TestExpireClosesAskEarly(t *testing.T) {
	pub := &fakePub{}
	st := New(Config{Tmux: &fakeTmux{}, Pub: pub, TTL: time.Hour})
	a, ch := st.OpenAsk(context.Background(), wire.AskParams{Title: "Ship it?", Options: []string{"Yes"}})
	st.Expire(a.RequestID)
	st.Expire(a.RequestID)
	if d, ok := <-ch; ok {
		t.Fatalf("ask received %q after expiry", d)
	}
	if got := pub.closed(); len(got) != 1 || got[0] != wire.ResolutionExpired {
		t.Fatalf("closed events = %v, want [expired]", got)
	}
	wantConflict(t, st.Answer(context.Background(), a.RequestID, "option:1", wire.ByPhone), wire.ConflictExpired, "")
}

func TestSessionGoneClosesItsApprovalsButNotAsks(t *testing.T) {
	pub := &fakePub{}
	st := New(Config{Tmux: &fakeTmux{}, Pub: pub})
	sid, other := "s_aaaaaaaaaa", "s_bbbbbbbbbb"
	hook := st.OpenFromHook(context.Background(), sid, wire.HookEvent{Agent: "claude", Data: wire.HookData{Tool: "Bash"}})
	ask, _ := st.OpenAsk(context.Background(), wire.AskParams{SessionID: &sid, Title: "Deploy?", Options: []string{"yes", "no"}})
	kept := st.OpenFromHook(context.Background(), other, wire.HookEvent{Agent: "claude", Data: wire.HookData{Tool: "Bash"}})

	st.SessionGone(context.Background(), sid)

	if got := pub.closed(); !reflect.DeepEqual(got, []string{wire.ResolutionGone}) {
		t.Fatalf("closed events = %v, want [gone]", got)
	}
	var open []string
	for _, a := range st.Pending("") {
		open = append(open, a.RequestID)
	}
	if want := []string{ask.RequestID, kept.RequestID}; !slices.Equal(slices.Sorted(slices.Values(open)), slices.Sorted(slices.Values(want))) {
		t.Fatalf("pending %v, want the ask and the other session's approval; hook %s", open, hook.RequestID)
	}
}

const bashPrompt = "│ Bash command               │\n│   rm -rf ./build && make   │\n│ Do you want to proceed?    │\n│ ❯ 1. Yes                   │\n│   2. No                    │\n"

func TestHookSubjectWinsOverScreen(t *testing.T) {
	hook := wire.HookEvent{Agent: "claude", Data: wire.HookData{
		Tool: "Bash", Command: "rm -rf ./build && make clean",
		Subject: &wire.Subject{Kind: wire.SubjectCommand, Text: "rm -rf ./build && make clean"},
	}}
	screenSubject := wire.Subject{Kind: wire.SubjectCommand, Text: "rm -rf ./build && make"}
	pr, ok := detect.Detect(bashPrompt)
	if !ok || pr.Subject == nil || *pr.Subject != screenSubject {
		t.Fatalf("screen subject = %+v", pr.Subject)
	}
	check := func(t *testing.T, a wire.Approval) {
		t.Helper()
		if a.Subject == nil || *a.Subject != *hook.Data.Subject {
			t.Fatalf("subject = %+v, want the hook's", a.Subject)
		}
		if a.Detail == nil || *a.Detail != hook.Data.Subject.Text {
			t.Fatalf("detail = %v, want the subject text", a.Detail)
		}
	}

	t.Run("screen first", func(t *testing.T) {
		st := New(Config{Tmux: &fakeTmux{screen: bashPrompt}, Pub: &fakePub{}})
		st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
		if got := st.Pending(""); len(got) != 1 || got[0].Subject == nil || *got[0].Subject != screenSubject {
			t.Fatalf("screen approval = %+v", got)
		}
		check(t, st.OpenFromHook(context.Background(), "s_aaaaaaaaaa", hook))
		check(t, st.Pending("")[0])
	})
	t.Run("hook first", func(t *testing.T) {
		f := &fakeTmux{}
		st := New(Config{Tmux: f, Pub: &fakePub{}})
		check(t, st.OpenFromHook(context.Background(), "s_aaaaaaaaaa", hook))
		f.mu.Lock()
		f.screen = bashPrompt
		f.mu.Unlock()
		st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
		got := st.Pending("")
		if len(got) != 1 || len(got[0].Options) != 2 {
			t.Fatalf("pending = %+v", got)
		}
		check(t, got[0])
	})
	t.Run("hook without a subject takes the screen's", func(t *testing.T) {
		st := New(Config{Tmux: &fakeTmux{screen: bashPrompt}, Pub: &fakePub{}})
		a := st.OpenFromHook(context.Background(), "s_aaaaaaaaaa", wire.HookEvent{Agent: "claude", Data: wire.HookData{Tool: "Bash"}})
		if a.Subject == nil || *a.Subject != screenSubject || a.Detail == nil || *a.Detail != screenSubject.Text {
			t.Fatalf("approval = %+v", a)
		}
	})
}
