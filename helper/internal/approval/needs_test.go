package approval

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

const sess = "s_aaaaaaaaaa"

func TestHookWithoutScreenNeverReplacesAScreenRequest(t *testing.T) {
	f := &fakeTmux{}
	pub := &fakePub{}
	st := New(Config{Tmux: f, Pub: pub})
	pr, _ := detect.Detect(menu)
	st.PromptSeen(context.Background(), sess, pr)
	seen := st.Pending(sess)[0]
	// The hook fires after the screen moved on (capture shows nothing).
	got := st.OpenFromHook(context.Background(), sess, wire.HookEvent{Agent: "claude", Data: wire.HookData{Message: "Claude needs your permission"}})
	if got.RequestID != seen.RequestID {
		t.Fatalf("the hook opened a second request %+v", got)
	}
	p := st.Pending(sess)
	if len(p) != 1 || len(p[0].Options) != 2 || p[0].Answerable != nil {
		t.Fatalf("pending = %+v, want the screen request, still answerable", p)
	}
	if c := pub.closed(); len(c) != 0 {
		t.Fatalf("closed %v, want none", c)
	}
}

func TestTabbedQuestionDialogIsReadOnly(t *testing.T) {
	screen, err := os.ReadFile("../detect/testdata/claude/ask-tabs.txt")
	if err != nil {
		t.Fatal(err)
	}
	st := New(Config{Tmux: &fakeTmux{}, Pub: &fakePub{}})
	pr, ok := detect.Detect(string(screen))
	if !ok {
		t.Fatal("no prompt")
	}
	st.PromptSeen(context.Background(), sess, pr)
	a := st.Pending(sess)[0]
	if a.Answerable == nil || *a.Answerable {
		t.Fatalf("a tabbed question dialog is answerable: %+v", a)
	}
}

func TestQuestionWaitDeliversAnswersAndSuppressesHookApprovals(t *testing.T) {
	st := New(Config{Tmux: &fakeTmux{}, Pub: &fakePub{}})
	prompts := []string{"Which color?", "Which fruits?"}
	got := make(chan map[string]string, 1)
	go func() {
		a, _ := st.WaitQuestion(context.Background(), sess, prompts)
		got <- a
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !st.QuestionWaiting(sess) {
		if time.Now().After(deadline) {
			t.Fatal("the wait never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Claude's permission notification for the same dialog opens nothing.
	if a := st.OpenFromHook(context.Background(), sess, wire.HookEvent{Agent: "claude"}); a.RequestID != "" || len(st.Pending(sess)) != 0 {
		t.Fatalf("a request opened while a question hook waits: %+v", a)
	}
	if st.AnswerQuestion(sess, []string{"Which color?"}, nil) {
		t.Fatal("answers for other prompts were delivered")
	}
	want := map[string]string{"Which color?": "Blue", "Which fruits?": "Apple, Plum"}
	if !st.AnswerQuestion(sess, prompts, want) {
		t.Fatal("no waiting hook took the answers")
	}
	if a := <-got; a["Which fruits?"] != "Apple, Plum" {
		t.Fatalf("hook got %v", a)
	}
	if st.QuestionWaiting(sess) {
		t.Fatal("the wait stayed registered")
	}
}

func TestQuestionWaitEndsWithItsContext(t *testing.T) {
	st := New(Config{Tmux: &fakeTmux{}, Pub: &fakePub{}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, ok := st.WaitQuestion(ctx, sess, []string{"Q?"}); ok {
		t.Fatal("answered without an answer")
	}
	if st.QuestionWaiting(sess) || st.AnswerQuestion(sess, []string{"Q?"}, nil) {
		t.Fatal("a finished wait still takes answers")
	}
}
