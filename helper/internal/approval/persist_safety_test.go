package approval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// kept is New, with its writes finished before the test's folders go.
func kept(t *testing.T, cfg Config) *Store {
	st := New(cfg)
	t.Cleanup(st.Flush)
	return st
}

func (p *fakePub) snapshot() []event {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]event(nil), p.events...)
}

func otherPrompt(t *testing.T) (string, detect.Prompt) {
	b, err := os.ReadFile("../detect/testdata/generic/yn.txt")
	if err != nil {
		t.Fatal(err)
	}
	p, ok := detect.Detect(string(b))
	if !ok {
		t.Fatal("the fixture is no prompt")
	}
	return string(b), p
}

// A hook approval whose prompt was not drawn yet is not kept: after a
// restart it cannot attach itself to whatever prompt shows next, so
// nothing is typed for it and nothing announces it with buttons.
func TestHookApprovalWithoutPromptIsNotKept(t *testing.T) {
	path := keptPath(t)
	f := &fakeTmux{screen: "$ "}
	st := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path})
	subject := &wire.Subject{Kind: wire.SubjectCommand, Text: "rm -rf build"}
	a := st.OpenFromHook(context.Background(), "s_aaaaaaaaaa", wire.HookEvent{Agent: "copilot", Data: wire.HookData{Tool: "bash", Subject: subject}})
	st.Flush()
	// An older helper may have kept one: it is dropped on load too.
	forged, _ := json.Marshal(persisted{V: persistVersion, Approvals: []persistOf{{Approval: a}}})
	_ = os.WriteFile(path, forged, 0o600)

	screen, other := otherPrompt(t)
	f.mu.Lock()
	f.screen = screen
	f.mu.Unlock()
	pub := &fakePub{}
	st2 := kept(t, Config{Tmux: f, Pub: pub, Path: path})
	st2.PromptSeen(context.Background(), "s_aaaaaaaaaa", other)
	t.Cleanup(st2.Flush)
	for _, ev := range pub.snapshot() {
		if o, ok := ev.payload.(wire.ApprovalOpenEvent); ok && o.Approval.RequestID == a.RequestID {
			t.Fatalf("the hook approval was announced again: %+v", o.Approval)
		}
	}
	if err := st2.Answer(context.Background(), a.RequestID, wire.DecisionApprove, wire.ByPhone); !errors.Is(err, ErrUnknown) {
		t.Fatalf("answer to the old hook approval: %v", err)
	}
	if len(f.sent) != 0 {
		t.Fatalf("keys typed: %v", f.sent)
	}
}

// A kept approval whose prompt was replaced by another while the helper
// was down closes as answered at the desk; nothing is typed.
func TestRestoredApprovalWhosePromptChanged(t *testing.T) {
	path := keptPath(t)
	f := &fakeTmux{screen: menu}
	st := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path})
	pr, _ := detect.Detect(menu)
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	a := st.Pending("")[0]
	st.Flush()
	screen, _ := otherPrompt(t)
	f.mu.Lock()
	f.screen = screen
	f.mu.Unlock()
	pub := &fakePub{}
	st2 := kept(t, Config{Tmux: f, Pub: pub, Path: path})
	eventually(t, "the changed prompt's approval closes", func() bool { return len(pub.closed()) == 1 })
	if pub.closed()[0] != wire.ResolutionDesk {
		t.Fatalf("closed as %v", pub.closed())
	}
	if err := st2.Answer(context.Background(), a.RequestID, wire.DecisionApprove, wire.ByPhone); err == nil {
		t.Fatal("the closed approval took an answer")
	}
	if len(f.sent) != 0 {
		t.Fatalf("keys typed: %v", f.sent)
	}
}

// Restored requests and their later updates are marked, so the push
// sender does not ring for them again.
func TestRestoredApprovalIsMarked(t *testing.T) {
	path := keptPath(t)
	f := &fakeTmux{screen: menu}
	st := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path})
	pr, _ := detect.Detect(menu)
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	st.Flush()
	pub := &fakePub{}
	st2 := kept(t, Config{Tmux: f, Pub: pub, Path: path})
	st2.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	opens := 0
	for _, ev := range pub.snapshot() {
		if o, ok := ev.payload.(wire.ApprovalOpenEvent); ok {
			opens++
			if !o.Restored {
				t.Fatalf("an update of a restored request is not marked: %+v", o)
			}
		}
	}
	if opens < 2 {
		t.Fatalf("%d open events, want the restore and the snippet", opens)
	}
}

// A kept file's options are not trusted: once the prompt is read again
// they are the screen's.
func TestRestoredOptionsComeFromTheScreen(t *testing.T) {
	path := keptPath(t)
	f := &fakeTmux{screen: menu}
	st := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path})
	pr, _ := detect.Detect(menu)
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
	st.Flush()
	var in persisted
	b, _ := os.ReadFile(path)
	_ = json.Unmarshal(b, &in)
	for i := range in.Approvals[0].Approval.Options {
		in.Approvals[0].Approval.Options[i].Label = "Yes, and always allow"
		in.Approvals[0].Approval.Options[i].Role = wire.RoleApprove
	}
	b, _ = json.Marshal(in)
	_ = os.WriteFile(path, b, 0o600)
	st2 := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path})
	eventually(t, "the options are read again", func() bool {
		got := st2.Pending("")
		return len(got) == 1 && reflect.DeepEqual(got[0].Options, pr.Options)
	})
}

// A write that stalls on the disk holds up no answer.
func TestStalledWriteDoesNotBlockAnswers(t *testing.T) {
	path := keptPath(t)
	f := &fakeTmux{screen: menu}
	st := kept(t, Config{Tmux: f, Pub: &fakePub{}, Path: path})
	st.Flush()
	release := make(chan struct{})
	st.saver.write = func(p string, b []byte) error {
		<-release
		return writeKept(p, b)
	}
	pr, _ := detect.Detect(menu)
	done := make(chan error, 1)
	go func() {
		st.PromptSeen(context.Background(), "s_aaaaaaaaaa", pr)
		a := st.Pending("")[0]
		done <- st.Answer(context.Background(), a.RequestID, wire.DecisionApprove, wire.ByPhone)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an answer waited for the disk")
	}
	close(release)
	st.Flush()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the last version was not written: %v", err)
	}
}
