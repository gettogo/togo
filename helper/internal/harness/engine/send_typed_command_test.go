package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("faketui", []byte(`
adapter: faketui
name: Fake TUI commands
revision: 1
match: { agents: [faketui] }
capabilities: [commands]
actions:
  command: { cli: { argv: [rec, command, '{{.command}}', '{{.arguments}}'] }, stability: documented }
commands:
  static:
    - { name: new, detail: New session, kind: terminal }
    - { name: add, hint: '<file>' }
`))
}

// Seen live on OpenCode 2.3.0: /new from the phone went to the server's
// command route, which knows only the server's commands, and was refused;
// typed in the terminal it opened a new conversation. A / line that names a
// command the adapter lists as the terminal's own (kind terminal) is typed;
// any other / line still runs as the harness's command.
func TestSendTypesTheTerminalsOwnCommand(t *testing.T) {
	const id = "s_faketui001"
	bin, home := t.TempDir(), t.TempDir()
	rec := "#!/bin/sh\necho \"$@\" >> rec.log\n"
	if err := os.WriteFile(filepath.Join(bin, "rec"), []byte(rec), 0o755); err != nil {
		t.Fatal(err)
	}
	pane := &seqPane{}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "faketui", Managed: true, Cwd: home, Command: []string{"faketui"}}},
		Typist:   pane, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"/new", "/init", "/add main.go"} {
		r, err := e.Send(ctx, harness.SendRequest{Session: id, Text: text, Mode: harness.SendNow})
		if err != nil || r.Item.User == nil || r.Item.User.Text != text {
			t.Fatalf("send %q: %+v %v", text, r, err)
		}
	}
	if _, typed := pane.snapshot(); !reflect.DeepEqual(typed, []string{"/new"}) {
		t.Errorf("typed %q, want only /new", typed)
	}
	var got []byte
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if got, _ = os.ReadFile(filepath.Join(home, "rec.log")); len(got) >= len("command init \ncommand add main.go\n") {
			break
		}
	}
	if want := "command init \ncommand add main.go\n"; string(got) != want {
		t.Errorf("command calls\n%s\nwant\n%s", got, want)
	}
}
