package rpc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// After a helper restart the server lists the thread the desk left
// before the one the chat followed. The followed one is joined first, so
// the chat names it at once and never starts over when it comes later.
func TestFeedJoinsTheFollowedThreadFirst(t *testing.T) {
	sock := filepath.Join(socketDir(t), "s.sock")
	f := startFake(t, sock)
	var mu sync.Mutex
	var order []string
	f.handle = func(method string, params json.RawMessage) (any, *Error) {
		if method == MethodResume {
			var p struct {
				ThreadID string `json:"threadId"`
			}
			json.Unmarshal(params, &p)
			mu.Lock()
			order = append(order, p.ThreadID)
			mu.Unlock()
		}
		return resumeHandler(method, params)
	}
	f.loaded = []string{"thr-left", "thr-followed", "thr-sub"}
	env := harness.Env{SessionID: "s_rpcfollow", Endpoint: "unix://" + sock, Vars: map[string]string{source.VarTogoHome: t.TempDir()}}
	source.SaveFollowed(env, FollowedThread, "thr-followed")
	r := NewRunner()
	r.Poll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &recorder{}
	go r.Watch(ctx, feedSpec(t, spec.RPC{Method: MethodResume, Params: map[string]string{"threadId": ""}}), env, rec.emit)
	waitFor(t, "three joins", func() bool { return f.count(MethodResume) >= 3 })
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 || order[0] != "thr-followed" || order[1] != "thr-left" || order[2] != "thr-sub" {
		t.Fatalf("joined %v, want the followed thread first, then the server's order", order)
	}
}

func TestFollowedFirst(t *testing.T) {
	for _, c := range []struct {
		ids  []string
		id   string
		want string
	}{{[]string{"a", "b", "c"}, "c", "c a b"}, {[]string{"a", "b"}, "a", "a b"}, {[]string{"a", "b"}, "x", "a b"}, {[]string{"a"}, "", "a"}, {nil, "a", ""}} {
		got := followedFirst(c.ids, c.id)
		s := ""
		for i, x := range got {
			if i > 0 {
				s += " "
			}
			s += x
		}
		if s != c.want {
			t.Errorf("followedFirst(%v, %q) = %q, want %q", c.ids, c.id, s, c.want)
		}
	}
}
