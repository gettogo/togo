package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
)

// When only the session's state names the model, the status block follows
// a switch. Seen on Kilo 7.8.27: the second turn ran on aion-2.0, and the
// chip still read glm-5.3-flash, because the old model was taken for one a
// status line had named.
func TestStatusModelFollowsAStateSwitch(t *testing.T) {
	const id = "s_fakestat01"
	home := t.TempDir()
	pane := &seqPane{}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakestart", Managed: true, Cwd: home, Command: []string{"fakestart"}}},
		Typist:   pane, Marker: &fakeMarker{}, Screen: pane.Screen, Home: home,
		LoginPath: func(context.Context) string { return os.Getenv("PATH") },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	wait := func(want string) {
		t.Helper()
		for i := 0; i < 100; i++ {
			if st := statusOf(e, id); st != nil && st.Model == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("status %+v, want model %s", statusOf(e, id), want)
	}
	deliver(id, map[string]any{"state": map[string]string{"harness_session": "ses_1", "model": "openrouter/z-ai/glm-5.3-flash"}})
	wait("openrouter/z-ai/glm-5.3-flash")
	deliver(id, map[string]any{"state": map[string]string{"model": "openrouter/aion-labs/aion-2.0"}})
	wait("openrouter/aion-labs/aion-2.0")
}
