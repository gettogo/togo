package hooks

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const usersLine = `bash ~/.claude/statusline-command.sh --name 'it''s mine' "x"`

func statusLineCommand(t *testing.T, e env) string {
	t.Helper()
	var doc struct {
		StatusLine struct {
			Command string `json:"command"`
			Padding int    `json:"padding"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal([]byte(read(t, settingsPath(e))), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.StatusLine.Command
}

func withUsersLine(t *testing.T) (env, []byte) {
	e := newEnv(t)
	orig, _ := json.MarshalIndent(map[string]any{"model": "opus", "statusLine": map[string]any{"type": "command", "command": usersLine, "padding": 1}}, "", "  ")
	orig = append(orig, '\n')
	write(t, settingsPath(e), orig)
	return e, orig
}

func TestChainWrapsTheUsersLineOnceAndRestoresExactly(t *testing.T) {
	e, orig := withUsersLine(t)
	mustOK(t, Install(e.o, []string{Claude}))
	want := "'/opt/togo/bin/togo' hook claude statusline --then " + shellQuote(usersLine)
	if got := statusLineCommand(t, e); got != want {
		t.Fatalf("command %q\nwant %q", got, want)
	}
	if !strings.Contains(read(t, settingsPath(e)), `"padding": 1`) {
		t.Error("the status line's other fields were lost")
	}
	if b, err := os.ReadFile(settingsPath(e) + backupSuffix); err != nil || string(b) != string(orig) {
		t.Errorf("backup %q err %v", b, err)
	}
	if st, ok := loadStatusLineState(e.o.TogoHome); !ok || st.Original != usersLine {
		t.Fatalf("state %+v %v", st, ok)
	}
	// Idempotent: a second install changes nothing and never wraps twice.
	rs := Install(e.o, []string{Claude})
	if rs[0].State != "unchanged" || statusLineCommand(t, e) != want {
		t.Fatalf("second install %s: %q", rs[0].State, statusLineCommand(t, e))
	}
	mustOK(t, Uninstall(e.o, []string{Claude}))
	if got := read(t, settingsPath(e)); got != string(orig) {
		t.Fatalf("uninstall left\n%s\nwant\n%s", got, orig)
	}
}

// The user changed something else after install: the file cannot be
// restored whole, but the status line still gets its exact original back.
func TestUninstallRestoresTheLineAfterOtherEdits(t *testing.T) {
	e, _ := withUsersLine(t)
	mustOK(t, Install(e.o, []string{Claude}))
	cur := read(t, settingsPath(e))
	write(t, settingsPath(e), []byte(strings.Replace(cur, `"model": "opus"`, `"model": "sonnet"`, 1)))
	mustOK(t, Uninstall(e.o, []string{Claude}))
	if got := statusLineCommand(t, e); got != usersLine {
		t.Fatalf("command %q", got)
	}
	if !strings.Contains(read(t, settingsPath(e)), `"sonnet"`) {
		t.Error("the user's later edit was lost")
	}
	if _, ok := loadStatusLineState(e.o.TogoHome); ok {
		t.Error("the state file outlived the uninstall")
	}
}

func TestUninstallLeavesALineTheUserEdited(t *testing.T) {
	e, _ := withUsersLine(t)
	mustOK(t, Install(e.o, []string{Claude}))
	cur := read(t, settingsPath(e))
	edited := strings.Replace(cur, "statusline-command.sh", "other.sh", 1)
	write(t, settingsPath(e), []byte(edited))
	mustOK(t, Uninstall(e.o, []string{Claude}))
	if got := statusLineCommand(t, e); !strings.Contains(got, "other.sh") || !strings.Contains(got, "--then") {
		t.Fatalf("an edited chain was changed: %q", got)
	}
}

func TestNoStatusLineGetsTogosOwn(t *testing.T) {
	e := newEnv(t)
	write(t, settingsPath(e), []byte("{}\n"))
	mustOK(t, Install(e.o, []string{Claude}))
	if got := statusLineCommand(t, e); got != "'/opt/togo/bin/togo' hook claude statusline" {
		t.Fatalf("command %q", got)
	}
	if _, ok := loadStatusLineState(e.o.TogoHome); ok {
		t.Error("a state file for Togo's own line")
	}
}

func TestChainFollowsANewBinaryPath(t *testing.T) {
	e, _ := withUsersLine(t)
	mustOK(t, Install(e.o, []string{Claude}))
	e.o.Binary = "/usr/local/bin/togo"
	mustOK(t, Install(e.o, []string{Claude}))
	want := "'/usr/local/bin/togo' hook claude statusline --then " + shellQuote(usersLine)
	if got := statusLineCommand(t, e); got != want {
		t.Fatalf("command %q", got)
	}
	mustOK(t, Uninstall(e.o, []string{Claude}))
	if got := statusLineCommand(t, e); got != usersLine {
		t.Fatalf("restored %q", got)
	}
}
