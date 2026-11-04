package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
)

func readPipeMessage(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	type res struct {
		payload []byte
		err     error
	}
	ch := make(chan res, 1)
	go func() {
		p, err := rpc.ReadFrame(r)
		ch <- res{p, err}
	}()
	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("read frame: %v", got.err)
		}
		var m map[string]any
		if err := json.Unmarshal(got.payload, &m); err != nil {
			t.Fatalf("frame %s: %v", got.payload, err)
		}
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no frame from the proxy")
		return nil
	}
}

func TestServeProxiesToRunningDaemon(t *testing.T) {
	home := shortHome(t)
	startDaemon(t, home)
	started := false
	conn, err := Connect(context.Background(), SocketPath(home), func() error { started = true; return nil }, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if started {
		t.Error("Connect started a daemon although one was running")
	}

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	pumped := make(chan error, 1)
	go func() {
		pumped <- Pump(context.Background(), conn, stdinR, stdoutW)
		stdoutW.Close()
	}()

	for _, req := range []string{helloRequest, `{"jsonrpc":"2.0","id":2,"method":"notify"}`} {
		if err := rpc.WriteFrame(stdinW, []byte(req)); err != nil {
			t.Fatal(err)
		}
	}
	if hello := readPipeMessage(t, stdoutR); hello["id"] != float64(1) || hello["result"] == nil {
		t.Fatalf("first frame = %v, want the hello result", hello)
	}
	if note := readPipeMessage(t, stdoutR); note["method"] != "event" || note["id"] != nil {
		t.Fatalf("second frame = %v, want the event notification", note)
	}
	if reply := readPipeMessage(t, stdoutR); reply["id"] != float64(2) || reply["result"] != "sent" {
		t.Fatalf("third frame = %v, want the notify reply", reply)
	}

	stdinW.Close()
	select {
	case err := <-pumped:
		if err != nil {
			t.Fatalf("Pump = %v, want nil after stdin ends", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Pump did not return after stdin ended")
	}
}

func TestConnectStartsDaemonWhenAbsent(t *testing.T) {
	home := shortHome(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	conn, err := Connect(context.Background(), SocketPath(home), func() error {
		go func() { done <- Run(ctx, Config{Home: home, Build: buildPing, Info: testInfo}) }()
		return nil
	}, 2*time.Second)
	if err != nil {
		t.Fatalf("Connect = %v, want a connection to the started daemon", err)
	}
	conn.Close()
	cancel()
	if err := <-done; err != nil {
		t.Errorf("daemon Run = %v", err)
	}
}

func TestConnectReportsStartFailure(t *testing.T) {
	home := shortHome(t)
	boom := errors.New("no binary")
	_, err := Connect(context.Background(), SocketPath(home), func() error { return boom }, time.Second)
	if !errors.Is(err, boom) {
		t.Fatalf("Connect = %v, want the start error", err)
	}
}

func TestConnectGivesUpAfterWait(t *testing.T) {
	home := shortHome(t)
	begin := time.Now()
	_, err := Connect(context.Background(), SocketPath(home), func() error { return nil }, 200*time.Millisecond)
	if err == nil {
		t.Fatal("Connect succeeded with no daemon")
	}
	if waited := time.Since(begin); waited < 200*time.Millisecond || waited > 2*time.Second {
		t.Errorf("Connect waited %v, want about 200ms", waited)
	}
}

func TestConnectRejectsDaemonThatNeverAnswers(t *testing.T) {
	home := shortHome(t)
	defer func(d time.Duration) { probeTimeout = d }(probeTimeout)
	probeTimeout = 200 * time.Millisecond
	ln, err := sockd.Listen(SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var held []net.Conn
	var mu sync.Mutex
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			c.Close()
		}
	}()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	started := false
	begin := time.Now()
	_, err = Connect(context.Background(), SocketPath(home), func() error { started = true; return nil }, time.Second)
	if !errors.Is(err, ErrUnresponsive) {
		t.Fatalf("Connect = %v, want ErrUnresponsive", err)
	}
	if started {
		t.Error("Connect started a second daemon while one held the socket")
	}
	if waited := time.Since(begin); waited > 2*time.Second {
		t.Errorf("Connect took %v, want about the probe timeout", waited)
	}
}
