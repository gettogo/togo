package surface_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/surface"
	"github.com/gettogo/togo/helper/internal/tmux"
)

func checkLiveColorReplyHandback(t *testing.T, ctx context.Context, terminal *tmux.Client, companions *surface.Companions, config surface.CompanionConfig) {
	t.Helper()
	data, err := os.ReadFile("testdata/terminal-color-reply.json")
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(data, &recorded); err != nil {
		t.Fatal(err)
	}
	before, err := companions.State(config)
	if err != nil || !before.Native {
		t.Fatal("original renderer not native before color reply", err)
	}
	// The second full reply outlives any active background-color probe.
	if err := terminal.SendKeys(ctx, before.Pane, true, recorded.Reply+recorded.Reply); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(until) {
		current, err := companions.State(config)
		if err != nil || !current.Native || current.Owner != before.Owner {
			t.Fatal("late terminal color reply released native ownership", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := terminal.SendKeys(ctx, before.Pane, true, "desktop"); err != nil {
		t.Fatal(err)
	}
	for {
		current, err := companions.State(config)
		if err != nil {
			t.Fatal(err)
		}
		if !current.Native && current.Reason == "terminal_input" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("real desktop typing did not restore the terminal")
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Log("late recorded color reply retained native ownership; real desk typing handed back")
}
