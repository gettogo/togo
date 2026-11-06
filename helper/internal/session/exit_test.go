package session_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// The exit is kept in a session option, so a daemon that starts again
// reads it from the listing; removing it leaves the session without one.
func TestSaveExitOutlivesTheProcess(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	created := mustCreate(t, s, session.CreateOpts{})
	code, detail := 3, "bad flag"
	saved := &session.SavedExit{Status: wire.StatusFailed, Exit: wire.SessionExit{Code: &code, Reason: wire.ExitReasonExit, Detail: &detail}}
	if err := s.SaveExit(ctx, created.ID, saved); err != nil {
		t.Fatal(err)
	}
	if got := mustList(t, s)[0].SavedExit; !reflect.DeepEqual(got, saved) {
		t.Fatalf("listed exit %+v, want %+v", got, saved)
	}
	if err := s.SaveExit(ctx, created.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := mustList(t, s)[0].SavedExit; got != nil {
		t.Fatalf("listed exit %+v after removing it", got)
	}
	if err := s.SaveExit(ctx, "s_bbbbbbbbbb", nil); err == nil {
		t.Fatal("saving the exit of a missing session succeeded")
	}
}

// Anyone with the socket can write the option, so a value SaveExit could
// not have written is ignored and logged without its content.
func TestForgedExitIgnored(t *testing.T) {
	s := newService(t)
	var logs []string
	s.Logf = func(format string, args ...any) { logs = append(logs, format) }
	created := mustCreate(t, s, session.CreateOpts{})
	for _, v := range []string{
		`{"status":"idle","exit":{"code":0,"reason":"exit","detail":null}}`,
		`{"status":"ended","exit":{"code":300,"reason":"exit","detail":null}}`,
		`{"status":"ended","exit":{"code":0,"reason":"restart","detail":null}}`,
		`{"status":"ended","exit":{"code":0,"reason":"exit","detail":null},"x":1}`,
		`not json`,
	} {
		if err := s.Tmux.SetOption(context.Background(), created.ID, "@togo_exit", v); err != nil {
			t.Fatal(err)
		}
		if got := mustList(t, s)[0].SavedExit; got != nil {
			t.Errorf("%s: listed %+v, want none", v, got)
		}
	}
	if len(logs) != 5 || strings.Contains(strings.Join(logs, ""), "restart") {
		t.Fatalf("logs %q", logs)
	}
}
