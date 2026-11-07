package hub_test

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/hub"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

func TestDecorateFillsActivityHistory(t *testing.T) {
	now := time.Unix(30*1000, 0)
	h := hub.New(&session.Service{Tmux: tmuxtest.New(t)}, func() time.Time { return now })
	if got := decorated(h).ActivityHistory; got != nil {
		t.Fatalf("history before output = %v, want nil", got)
	}
	h.RecordOutput(exitID)
	got := decorated(h).ActivityHistory
	if len(got) != 16 || got[15] != 1.0/30 {
		t.Fatalf("history = %v", got)
	}
	un := h.Decorate([]session.Session{{ID: "u_x", Managed: false}})[0]
	if un.ActivityHistory != nil {
		t.Fatalf("unmanaged history = %v", un.ActivityHistory)
	}
}
