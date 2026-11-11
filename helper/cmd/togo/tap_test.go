package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
)

func tapHome(t *testing.T) (home, sock string) {
	t.Helper()
	home, err := os.MkdirTemp("", "tg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	if err := os.Mkdir(filepath.Join(home, "run"), 0o700); err != nil {
		t.Fatal(err)
	}
	return home, filepath.Join(home, "run", "togo.sock")
}

func TestTapDrainsStdinWithoutDaemon(t *testing.T) {
	home, _ := tapHome(t)
	big := bytes.Repeat([]byte("data \a\n"), 200000)
	var errOut bytes.Buffer
	start := time.Now()
	code := runTap(context.Background(), []string{"s_abcdefghij"}, map[string]string{"TOGO_HOME": home}, bytes.NewReader(big), &errOut)
	if code != 0 {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v", d)
	}
}

func TestTapRejectsBadID(t *testing.T) {
	var errOut bytes.Buffer
	if code := runTap(context.Background(), []string{"s_abc;rm"}, nil, strings.NewReader(""), &errOut); code != exitUsage {
		t.Fatalf("code %d", code)
	}
	if code := runTap(context.Background(), nil, nil, strings.NewReader(""), &errOut); code != exitUsage {
		t.Fatalf("code %d", code)
	}
}

func TestTapSendsSignalsToDaemon(t *testing.T) {
	home, sock := tapHome(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	methods := make(chan map[string]any, 16)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			raw, err := rpc.ReadFrame(conn)
			if err != nil {
				return
			}
			var req map[string]any
			_ = json.Unmarshal(raw, &req)
			resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
			_ = rpc.WriteFrame(conn, resp)
			if req["method"] != "hello" {
				methods <- req
			}
		}
	}()
	in := io.MultiReader(strings.NewReader("hi\a\x1b]9;Done\x1b\\"))
	var errOut bytes.Buffer
	if code := runTap(context.Background(), []string{"s_abcdefghij"}, map[string]string{"TOGO_HOME": home}, in, &errOut); code != 0 {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	var kinds []string
	for len(kinds) < 3 {
		select {
		case req := <-methods:
			if req["method"] != "local.signal" {
				t.Fatalf("method %v", req["method"])
			}
			p := req["params"].(map[string]any)
			if p["sessionId"] != "s_abcdefghij" {
				t.Fatalf("params %v", p)
			}
			kinds = append(kinds, p["kind"].(string))
		case <-time.After(5 * time.Second):
			t.Fatalf("got only %v", kinds)
		}
	}
	if strings.Join(kinds, ",") != "bell,osc9,output" && strings.Join(kinds, ",") != "output,bell,osc9" {
		t.Fatalf("kinds %v", kinds)
	}
}

// The tap publishes how many output bytes it has seen, for anchors.
func TestTapCountsBytes(t *testing.T) {
	home, _ := tapHome(t)
	data := bytes.Repeat([]byte("abc\n"), 5000)
	if code := runTap(context.Background(), []string{"s_abcdefghij"}, map[string]string{"TOGO_HOME": home}, bytes.NewReader(data), io.Discard); code != 0 {
		t.Fatalf("code %d", code)
	}
	b, err := os.ReadFile(filepath.Join(home, "run", "tap", "s_abcdefghij.off"))
	if err != nil || len(b) != 16 {
		t.Fatalf("counter file %x %v", b, err)
	}
	var total uint64
	for _, c := range b[8:] {
		total = total<<8 | uint64(c)
	}
	if total != uint64(len(data)) {
		t.Fatalf("total %d, want %d", total, len(data))
	}
}

// Output is reported at most once a second, and the last change of a
// burst is reported too. Seen live on Cline 3.0.74: the key that answered
// its question was reported with the dialog still drawn, the dialog closed
// and the reply came within the same second, and Cline drew nothing more;
// the watcher never read the screen again, so the answered question's
// prompt stayed pending and the session read "needs you".
func TestOutputSignalsReportTheEndOfABurst(t *testing.T) {
	got := make(chan time.Time, 16)
	o := &outputSignals{every: 50 * time.Millisecond, emit: func() { got <- time.Now() }}
	defer o.stop()
	start := time.Now()
	for range 5 { // a burst inside one interval
		o.note(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-got: // the burst's first output, at once
	case <-time.After(time.Second):
		t.Fatal("no signal for the first output")
	}
	select {
	case at := <-got:
		if at.Sub(start) < 40*time.Millisecond {
			t.Fatalf("trailing signal after %v, want one interval", at.Sub(start))
		}
	case <-time.After(time.Second):
		t.Fatal("no signal for the end of the burst")
	}
	select {
	case <-got:
		t.Fatal("a third signal for one burst")
	case <-time.After(150 * time.Millisecond):
	}
	// A lone output after a quiet spell is one signal.
	o.note(time.Now())
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("no signal after quiet")
	}
	select {
	case <-got:
		t.Fatal("a lone output gave two signals")
	case <-time.After(150 * time.Millisecond):
	}
}

// Once the pipe closed, no signal goes out (the queue is closed).
func TestOutputSignalsStopCancelsTheTrailingSignal(t *testing.T) {
	n := make(chan struct{}, 4)
	o := &outputSignals{every: 30 * time.Millisecond, emit: func() { n <- struct{}{} }}
	o.note(time.Now())
	o.note(time.Now())
	<-n
	o.stop()
	select {
	case <-n:
		t.Fatal("signal after stop")
	case <-time.After(100 * time.Millisecond):
	}
}
