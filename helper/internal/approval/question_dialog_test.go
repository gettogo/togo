package approval

import (
	"context"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
)

// Claude Code's question dialog, while its AskUserQuestion hook waits for
// the phone, is that question: a step of it the watcher finds (a question
// or the review) opens no screen request beside it, and a step after it
// closes none. Another prompt still opens one.
func TestQuestionDialogWhileItsHookWaitsOpensNothing(t *testing.T) {
	pub := &fakePub{}
	st := New(Config{Tmux: &fakeTmux{}, Pub: pub})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = st.WaitQuestion(ctx, sess, []string{"Which colour do you prefer?", "Which fruit do you prefer?"})
	}()
	deadline := time.Now().Add(30 * time.Second)
	for !st.QuestionWaiting(sess) {
		if time.Now().After(deadline) {
			t.Fatal("the wait never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, p := range []detect.Prompt{
		{Kind: detect.KindQuestion, Title: "Which colour do you prefer?", Fingerprint: "a", Ask: true},
		{Kind: detect.KindQuestion, Title: "Which fruit do you prefer?", Fingerprint: "b", Ask: true},
		{Kind: detect.KindQuestion, Title: "Ready to submit your answers?", Fingerprint: "c", Ask: true},
	} {
		st.PromptSeen(context.Background(), sess, p)
		if n := len(st.Pending(sess)); n != 0 {
			t.Fatalf("%q opened %d requests", p.Title, n)
		}
	}
	st.PromptGone(context.Background(), sess)
	if c := pub.closed(); len(c) != 0 {
		t.Fatalf("closed %v", c)
	}
	st.PromptSeen(context.Background(), sess, detect.Prompt{Kind: detect.KindPermission, Title: "Bash command", Fingerprint: "d"})
	if n := len(st.Pending(sess)); n != 1 {
		t.Fatalf("a permission prompt opened %d requests", n)
	}
}

// A request the watcher opened for an earlier step of the dialog, before
// the step was known to be the question's, is closed as answered at the
// desk once a later step is: no stale request lingers, not answerable.
func TestEarlierStepRequestClosesWhenTheDialogIsPaired(t *testing.T) {
	pub := &fakePub{}
	st := New(Config{Tmux: &fakeTmux{}, Pub: pub})
	st.PromptSeen(context.Background(), sess, detect.Prompt{Kind: detect.KindQuestion, Title: "Which colour do you prefer?", Fingerprint: "a", Ask: true})
	if n := len(st.Pending(sess)); n != 1 {
		t.Fatalf("the first step opened %d requests", n)
	}
	st.CloseScreenPrompts(context.Background(), sess)
	if n := len(st.Pending(sess)); n != 0 || len(pub.closed()) != 1 {
		t.Fatalf("pending %d, closed %v", n, pub.closed())
	}
	// The same through a waiting hook.
	st.PromptSeen(context.Background(), sess, detect.Prompt{Kind: detect.KindQuestion, Title: "Which colour do you prefer?", Fingerprint: "b", Ask: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = st.WaitQuestion(ctx, sess, []string{"Which colour do you prefer?"}) }()
	for deadline := time.Now().Add(30 * time.Second); !st.QuestionWaiting(sess); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the wait never registered")
		}
	}
	st.PromptSeen(context.Background(), sess, detect.Prompt{Kind: detect.KindQuestion, Title: "Which fruit do you prefer?", Fingerprint: "c", Ask: true})
	if n := len(st.Pending(sess)); n != 0 {
		t.Fatalf("pending %d after a paired step", n)
	}
}
