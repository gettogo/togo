package approval

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// With tools running side by side, one finishing must not close the
// approval of another that still shows its permission prompt.
func TestToolDoneKeepsAnApprovalWhosePromptIsStillShown(t *testing.T) {
	ctx := context.Background()
	const id = "s_aaaaaaaaaa"
	tm := &fakeTmux{screen: menu}
	st, pub := newStore(t, tm)
	p, ok := detect.Detect(menu)
	if !ok {
		t.Fatal("the menu is not detected")
	}
	st.PromptSeen(ctx, id, p)
	st.CloseAnswered(ctx, id)
	if got := pub.closed(); len(got) != 0 {
		t.Fatalf("closed %v while the prompt is on screen", got)
	}
	if len(st.Pending(id)) != 1 {
		t.Fatal("the approval is no longer pending")
	}

	// Answered at the desk: the prompt left, the tool ran and finished.
	tm.mu.Lock()
	tm.screen = "ran the command\n"
	tm.mu.Unlock()
	st.CloseAnswered(ctx, id)
	if got := pub.closed(); len(got) != 1 || got[0] != wire.ResolutionDesk {
		t.Fatalf("closed events = %v, want [desk]", got)
	}
}

// A hook's approval whose prompt has not been read yet stays while any
// prompt is showing, and closes once none is.
func TestToolDoneKeepsAHookApprovalBeforeItsPromptIsRead(t *testing.T) {
	ctx := context.Background()
	const id = "s_aaaaaaaaaa"
	tm := &fakeTmux{screen: "working\n"}
	st, pub := newStore(t, tm)
	st.OpenFromHook(ctx, id, wire.HookEvent{Agent: "claude", Data: wire.HookData{Tool: "Bash"}})
	tm.mu.Lock()
	tm.screen = menu
	tm.mu.Unlock()
	st.CloseAnswered(ctx, id)
	if got := pub.closed(); len(got) != 0 {
		t.Fatalf("closed %v while a prompt is on screen", got)
	}
	tm.mu.Lock()
	tm.screen = "done\n"
	tm.mu.Unlock()
	st.CloseAnswered(ctx, id)
	if got := pub.closed(); len(got) != 1 || got[0] != wire.ResolutionDesk {
		t.Fatalf("closed events = %v, want [desk]", got)
	}
}
