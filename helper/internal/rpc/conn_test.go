package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

// pipeServer runs Serve over in-memory pipes and returns the client ends.
func pipeServer(t *testing.T, h Handler, info HelperInfo) (in *io.PipeWriter, out *io.PipeReader, served <-chan error) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		err := Serve(context.Background(), inR, outW, h, info)
		outW.Close()
		errc <- err
	}()
	t.Cleanup(func() {
		inW.Close()
		outR.Close()
	})
	return inW, outR, errc
}

func writeReq(t *testing.T, w io.Writer, payload string) {
	t.Helper()
	if err := WriteFrame(w, []byte(payload)); err != nil {
		t.Errorf("write frame: %v", err)
	}
}

func readMsg(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	raw, err := ReadFrame(r)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("frame is not JSON: %v", err)
	}
	return m
}

type connGrabber struct {
	got chan *Conn
}

func (g *connGrabber) Handle(ctx context.Context, method string, _ json.RawMessage) (any, *Error) {
	if method == "grab" {
		select {
		case g.got <- ConnFrom(ctx):
		default:
		}
	}
	return map[string]string{"m": method}, nil
}

func TestNotifyInterleavesWithResponsesWithoutTornFrames(t *testing.T) {
	g := &connGrabber{got: make(chan *Conn, 1)}
	in, out, served := pipeServer(t, g, testInfo)
	writeReq(t, in, helloV1)
	readMsg(t, out)
	writeReq(t, in, `{"jsonrpc":"2.0","id":2,"method":"grab"}`)
	readMsg(t, out)
	conn := <-g.got

	const n = 100
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range n {
			if err := conn.Notify("event", map[string]int{"seq": i + 1}); err != nil {
				t.Errorf("notify %d: %v", i, err)
			}
		}
	})
	wg.Go(func() {
		for i := range n {
			writeReq(t, in, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"echo"}`, 100+i))
		}
	})

	var notes, resps int
	lastSeq := 0.0
	for notes+resps < 2*n {
		m := readMsg(t, out)
		switch {
		case m["method"] == "event":
			seq := m["params"].(map[string]any)["seq"].(float64)
			if seq <= lastSeq {
				t.Fatalf("notification seq %v after %v", seq, lastSeq)
			}
			lastSeq = seq
			notes++
		case m["result"] != nil:
			resps++
		default:
			t.Fatalf("unexpected frame %v", m)
		}
	}
	wg.Wait()
	in.Close()
	if err := <-served; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestNotifyBeforeHelloIsRefused(t *testing.T) {
	var buf bytes.Buffer
	c := newConn(&buf)
	if err := c.Notify("event", map[string]int{"seq": 1}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("Notify before hello = %v, want ErrNotReady", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("refused notify wrote %d bytes", buf.Len())
	}
	c.setReady()
	if err := c.Notify("event", map[string]int{"seq": 1}); err != nil {
		t.Fatalf("Notify after hello: %v", err)
	}
	c.close()
	if err := c.Notify("event", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Notify after close = %v, want ErrClosed", err)
	}
	select {
	case <-c.Done():
	default:
		t.Fatal("Done not closed after close")
	}
}

func TestNotifyAfterStreamEndsReturnsErrClosed(t *testing.T) {
	g := &connGrabber{got: make(chan *Conn, 1)}
	in, out, served := pipeServer(t, g, testInfo)
	writeReq(t, in, helloV1)
	readMsg(t, out)
	writeReq(t, in, `{"jsonrpc":"2.0","id":2,"method":"grab"}`)
	readMsg(t, out)
	conn := <-g.got
	in.Close()
	if err := <-served; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	select {
	case <-conn.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done not closed after the stream ended")
	}
	if err := conn.Notify("event", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Notify = %v, want ErrClosed", err)
	}
}

type blockingHandler struct {
	release chan struct{}
}

func (b *blockingHandler) Handle(ctx context.Context, method string, _ json.RawMessage) (any, *Error) {
	if method == "block" {
		select {
		case <-b.release:
		case <-ctx.Done():
			return nil, NewError(CodeInternal, nil)
		}
	}
	return map[string]string{"m": method}, nil
}

func TestConcurrentMethodDoesNotBlockOthers(t *testing.T) {
	b := &blockingHandler{release: make(chan struct{})}
	info := testInfo
	info.Concurrent = []string{"block"}
	in, out, served := pipeServer(t, b, info)
	writeReq(t, in, helloV1)
	readMsg(t, out)

	writeReq(t, in, `{"jsonrpc":"2.0","id":2,"method":"block"}`)
	writeReq(t, in, `{"jsonrpc":"2.0","id":3,"method":"echo"}`)
	if m := readMsg(t, out); m["id"] != 3.0 {
		t.Fatalf("first reply id = %v, want 3 while 2 blocks", m["id"])
	}
	close(b.release)
	m := readMsg(t, out)
	if m["id"] != 2.0 || m["result"].(map[string]any)["m"] != "block" {
		t.Fatalf("blocked reply = %v", m)
	}
	in.Close()
	if err := <-served; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestServeCancelsBlockedConcurrentHandlerAtEOF(t *testing.T) {
	b := &blockingHandler{release: make(chan struct{})}
	info := testInfo
	info.Concurrent = []string{"block"}
	in, out, served := pipeServer(t, b, info)
	writeReq(t, in, helloV1)
	readMsg(t, out)
	writeReq(t, in, `{"jsonrpc":"2.0","id":2,"method":"block"}`)
	in.Close()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return while a concurrent handler blocked")
	}
}
