package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// An agent restarted with new flags (OpenCode 2.x on --session ses_x) is
// read as it now runs when its session state is rebuilt, not as it was
// created: the mapper learns the session from the flag. A launch that is
// no relaunch (a restore) drops it again.
func TestRebuiltSessionReadsTheRelaunchedArgv(t *testing.T) {
	const id = "s_relargv01"
	pane := &seqPane{}
	e := newSeq(t, id, pane, func(context.Context, string, []string) error { return nil })
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	act := spec.Action{Routes: spec.Routes{Relaunch: &spec.Relaunch{Argv: []string{"fake", "--session", "{{.id}}"}}}, Stability: "documented"}
	if res, err := e.run(context.Background(), s, "x", act, map[string]any{"id": "ses_x"}); err != nil || res.Result != harness.ResultRelaunched {
		t.Fatalf("relaunch = %+v %v", res, err)
	}
	e.launched(id) // the relaunch's own launch
	e.restart(id)
	s2, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s2.env.Argv, " "); got != "fake --session ses_x" {
		t.Fatalf("rebuilt argv %q", got)
	}
	e.launched(id) // a restore
	e.restart(id)
	s3, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s3.env.Argv, " "); got != "fake" {
		t.Fatalf("after a restore argv %q", got)
	}
}
