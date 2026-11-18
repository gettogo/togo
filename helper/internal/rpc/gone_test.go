package rpc

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// When the client stops sending, a call still running learns it at once,
// before its moment to reply is over and before its context is cancelled.
func TestARunningCallLearnsTheClientIsGoneAtOnce(t *testing.T) {
	pr, pw := io.Pipe()
	started, sawGone := make(chan struct{}), make(chan bool, 1)
	h := goneHandler(func(ctx context.Context, method string) (any, *Error) {
		if method != "slow" {
			return map[string]any{}, nil
		}
		if ClientGone(ctx) {
			t.Error("gone before the stream ended")
		}
		close(started)
		deadline := time.After(eofGrace / 2)
		for {
			select {
			case <-deadline:
				sawGone <- ClientGone(ctx)
				return map[string]any{}, nil
			case <-time.After(5 * time.Millisecond):
				if ClientGone(ctx) {
					sawGone <- ctx.Err() == nil
					return map[string]any{}, nil
				}
			}
		}
	})
	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), pr, io.Discard, h, HelperInfo{Concurrent: []string{"slow"}})
	}()
	write := func(body string) {
		if err := WriteFrame(pw, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	write(helloV1)
	write(`{"jsonrpc":"2.0","id":2,"method":"slow"}`)
	<-started
	pw.Close()
	select {
	case ok := <-sawGone:
		if !ok {
			t.Error("the call did not learn the client was gone while its context was still live")
		}
	case <-time.After(eofGrace + time.Second):
		t.Fatal("the call never learned the client was gone")
	}
	<-done
	if ClientGone(context.Background()) {
		t.Error("a context with no connection reports a client gone")
	}
}

type goneHandler func(ctx context.Context, method string) (any, *Error)

func (h goneHandler) Handle(ctx context.Context, method string, _ json.RawMessage) (any, *Error) {
	return h(ctx, method)
}
