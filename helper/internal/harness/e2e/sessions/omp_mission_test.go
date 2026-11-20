package sessions

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
	"github.com/gettogo/togo/helper/internal/ports"
)

// gitIn runs git in dir with no user or system configuration.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// The files omp's edit calls name are the files git shows changed in the
// folder: the Changes section lists them with what happened to each and a
// diff to open, and each edit step and the turn that made it name them too.
func TestOMPChangesAreTheFilesItsEditsTouched(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "omp/capability/changes")
	e := support.NewEnv(t, time.Hour)
	// e0.go is in the repository and edited; e1.go is new.
	gitIn(t, e.Cwd, "init", "-q")
	if err := os.WriteFile(filepath.Join(e.Cwd, "e0.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, e.Cwd, "add", ".")
	gitIn(t, e.Cwd, "commit", "-qm", "first")
	for name, body := range map[string]string{"e0.go": "package b\n", "e1.go": "package c\n"} {
		if err := os.WriteFile(filepath.Join(e.Cwd, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l, err := sessiongen.Write(e.Home, &sessiongen.OMP{TTY: "ttys042", Extra: map[int]sessiongen.OMPTurn{1: {Edits: 2}}},
		sessiongen.Script{Seed: 53, Start: e.Start, Cwd: e.Cwd, Turns: []sessiongen.Turn{{Say: 30}, {Say: 30}}})
	if err != nil {
		t.Fatal(err)
	}
	r := e.RigWith(t, enginetest.Options{Agent: "omp", NoRedact: true, Git: true})
	st := r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		return enginetest.Has(l.Truth.Last)(st) && len(sectionRows(st, harness.SectionChanges)) == 2
	})
	rows := map[string]harness.Row{}
	for _, row := range sectionRows(st, harness.SectionChanges) {
		rows[row.ID] = row
	}
	if rows["e0.go"].Subtitle != "modified" || rows["e1.go"].Subtitle != "new" {
		t.Errorf("changes %+v, want e0.go modified and e1.go new", rows)
	}
	for id, row := range rows {
		if row.Open == nil || row.Open.Kind != harness.OpenDiff || row.Open.Ref != id {
			t.Errorf("%s opens %+v, want its diff", id, row.Open)
		}
	}
	var named []string
	for _, it := range support.Items(st, func(it harness.Item) bool { return it.Tool != nil && it.Tool.Category == harness.ToolEdit }) {
		if len(it.Tool.Files) != 1 || it.Tool.State != harness.StateOK {
			t.Errorf("edit step %+v", it.Tool)
			continue
		}
		named = append(named, it.Tool.Files[0].Path)
	}
	if strings.Join(named, " ") != "e0.go e1.go" {
		t.Errorf("edit steps name %v, want e0.go e1.go (relative to the folder)", named)
	}
	changed := map[int]int{}
	for _, it := range support.Items(st, func(it harness.Item) bool { return it.Turn != nil }) {
		changed[it.Turn.FilesChanged]++
	}
	if changed[2] != 1 || changed[0] != 1 {
		t.Errorf("files changed per turn %v, want one turn with 2 and one with none", changed)
	}
	// A file edited again (agent and desk alike) and one deleted: the
	// section follows the working tree after the next turn ends.
	if err := os.Remove(filepath.Join(e.Cwd, "e1.go")); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(sessiongen.Turn{Say: 20}); err != nil {
		t.Fatal(err)
	}
	st = r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		return enginetest.Has(l.Truth.Last)(st) && len(sectionRows(st, harness.SectionChanges)) == 1
	})
	if rows := sectionRows(st, harness.SectionChanges); rows[0].ID != "e0.go" {
		t.Errorf("changes after the delete: %+v", rows)
	}
}

// The ports the session's processes listen on are listed with the program
// that holds each, and open as a URL. A server that stops is gone from the
// list the next time the engine reads it.
func TestOMPPortsAreTheSessionsListeners(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "omp/capability/ports")
	listen := []ports.Listener{{Port: 3000, Address: "127.0.0.1", PID: 4101, Command: "node"}, {Port: 5173, Address: "127.0.0.1", PID: 4102, Command: "vite"}}
	_, l, r := ompSession(t, time.Hour, nil, enginetest.Options{Ports: true, Listeners: listen}, sessiongen.Turn{Say: 30})
	st := r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		return enginetest.Has(l.Truth.Last)(st) && len(sectionRows(st, harness.SectionPorts)) == 2
	})
	got := map[string]harness.Row{}
	for _, row := range sectionRows(st, harness.SectionPorts) {
		got[row.ID] = row
	}
	for _, want := range []struct{ id, cmd string }{{"3000", "node"}, {"5173", "vite"}} {
		row := got[want.id]
		if row.Title != ":"+want.id || row.Subtitle != want.cmd || row.State != harness.StateRunning ||
			row.Open == nil || row.Open.Kind != harness.OpenURL || row.Open.Ref != want.id {
			t.Errorf("port %s: %+v", want.id, row)
		}
	}
	// The server on 5173 stops; the helper restarts and reads the ports again.
	r.SetListeners(listen[0])
	r.Restart()
	st = r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		return enginetest.Has(l.Truth.Last)(st) && len(sectionRows(st, harness.SectionPorts)) == 1
	})
	if rows := sectionRows(st, harness.SectionPorts); rows[0].ID != "3000" {
		t.Errorf("ports after the server stopped: %+v", rows)
	}
}

// /fork in omp writes a new conversation file that names the one it came
// from. The chat starts over on it (a new epoch, none of the old items),
// and a prompt sent from the phone right after reaches omp.
func TestOMPForkStartsTheChatOver(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "omp/lifecycle/fork")
	a := newOMPAgent(t, map[int]sessiongen.OMPTurn{3: {Fork: true}}, sessiongen.Turn{Say: 30, Tools: 1, Output: 40}, sessiongen.Turn{Say: 30})
	r := a.r
	before := r.SettleUntil(support.FileLimit, enginetest.Has(a.l.Truth.Last))
	old := map[string]bool{}
	for _, it := range before.Items {
		old[it.ID] = true
	}
	first := a.l.Main()
	if err := a.l.Append(sessiongen.Turn{Say: 30}); err != nil {
		t.Fatal(err)
	}
	if a.l.Main() == first {
		t.Fatal("the fork wrote no new file")
	}
	after := r.SettleUntil(support.FileLimit, enginetest.Has(a.l.Truth.Last))
	if after.Epoch == before.Epoch {
		t.Errorf("the chat kept epoch %s after the fork", after.Epoch)
	}
	for _, it := range after.Items {
		if old[it.ID] {
			t.Errorf("%s (%s) of the old conversation is still shown after the fork", it.ID, it.Kind)
		}
	}
	if n := len(support.UserPrompts(after)); n != 1 {
		t.Errorf("%d prompts after the fork, want only the fork's", n)
	}
	// omp takes a prompt typed to the fork and records it in the fork's file.
	hook, written := recordAfterSent(t, r, func(text string) error {
		return a.l.Append(sessiongen.Turn{Prompt: text, Say: 20})
	})
	r.Term.OnSubmit = hook
	if _, err := r.Send("Carry on in the fork", harness.SendNow); err != nil {
		t.Fatalf("send: %v", err)
	}
	<-written
	st := r.SettleUntil(support.FileLimit, enginetest.Has(a.l.Truth.Last))
	if got := r.Term.Submitted(); len(got) != 1 || got[0] != "Carry on in the fork" {
		t.Errorf("omp was given %q", got)
	}
	var seen bool
	for _, it := range support.UserPrompts(st) {
		seen = seen || it.User.Text == "Carry on in the fork"
	}
	if !seen || len(support.UserPrompts(st)) != 2 {
		t.Errorf("prompts in the fork's chat: %d, the sent one shown: %v", len(support.UserPrompts(st)), seen)
	}
}
