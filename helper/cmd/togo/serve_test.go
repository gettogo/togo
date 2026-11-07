package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
)

func frames(t *testing.T, reqs ...string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	for _, r := range reqs {
		if err := rpc.WriteFrame(&buf, []byte(r)); err != nil {
			t.Fatal(err)
		}
	}
	return &buf
}

const helloRequest = `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":1,"client":{"name":"test","version":"0"},"capabilities":[]}}`

func TestServeHello(t *testing.T) {
	t.Setenv("TOGO_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"serve", "--stdio"}, nil, frames(t, helloRequest), &out, &errOut)
	if code != 0 || errOut.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	payload, err := rpc.ReadFrame(&out)
	if err != nil {
		t.Fatalf("reading reply: %v", err)
	}
	var reply struct {
		Result struct {
			Protocol int `json:"protocol"`
			Helper   struct {
				Version string `json:"version"`
			} `json:"helper"`
			Capabilities []string `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(payload, &reply); err != nil {
		t.Fatalf("reply %s: %v", payload, err)
	}
	if want := []string{"session.v1", "agents.v1", "fs.v1", "capture.v1"}; !slices.Equal(reply.Result.Capabilities, want) {
		t.Errorf("capabilities = %q, want %q", reply.Result.Capabilities, want)
	}
	if reply.Result.Protocol != 1 || reply.Result.Helper.Version == "" {
		t.Errorf("result = %+v, want protocol 1 and a version", reply.Result)
	}
	if rest := out.Len(); rest != 0 {
		t.Errorf("%d unexpected bytes after the reply", rest)
	}
}

func TestServeWritesNothingToLogWithoutFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TOGO_HOME", home)
	var out, errOut bytes.Buffer
	runContext(context.Background(), []string{"serve", "--stdio"}, nil, frames(t, helloRequest), &out, &errOut)
	if _, err := os.Stat(filepath.Join(home, "log")); !os.IsNotExist(err) {
		t.Errorf("log directory exists without --log (stat error: %v)", err)
	}
}

func TestServeNoStdioFlag(t *testing.T) {
	t.Setenv("TOGO_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"serve"}, nil, frames(t), &out, &errOut)
	if code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "Usage") {
		t.Errorf("code=%d stdout=%q stderr=%q, want exit 2 and usage", code, out.String(), errOut.String())
	}
}

func TestServeLogFileMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TOGO_HOME", home)
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"serve", "--stdio", "--log"}, nil, frames(t, helloRequest), &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	dirInfo, err := os.Stat(filepath.Join(home, "log"))
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(filepath.Join(home, "log", "serve.log"))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 || fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("modes dir=%v file=%v, want 0700 and 0600", dirInfo.Mode().Perm(), fileInfo.Mode().Perm())
	}
}

// blockedReader never returns from Read, like a pipe whose writer is idle.
type blockedReader struct{ never chan struct{} }

func (r blockedReader) Read([]byte) (int, error) {
	<-r.never
	return 0, io.EOF
}

func TestServeReturnsWhenContextEndsWhileReadIsBlocked(t *testing.T) {
	t.Setenv("TOGO_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var out, errOut bytes.Buffer
	go func() {
		done <- runContext(ctx, []string{"serve", "--stdio"}, nil, blockedReader{never: make(chan struct{})}, &out, &errOut)
	}()
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("code=%d, want 0", code)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not return within 1s of cancellation")
	}
}

func TestServeSharesRecent(t *testing.T) {
	useTestHost(t)
	// A folder in home: temporary folders outside it are never recorded.
	home := realPath(t, t.TempDir())
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "proj")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	create := `{"jsonrpc":"2.0","id":2,"method":"session.create","params":{"agent":"shell","cwd":` + strconv.Quote(dir) + `}}`
	recent := `{"jsonrpc":"2.0","id":3,"method":"fs.recentDirs"}`
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"serve", "--stdio"}, nil, frames(t, helloRequest, create, recent), &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	var last []byte
	for i := 0; i < 3; i++ {
		p, err := rpc.ReadFrame(&out)
		if err != nil {
			t.Fatalf("reply %d: %v", i+1, err)
		}
		last = p
	}
	var reply struct {
		Result struct {
			Dirs []string `json:"dirs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(last, &reply); err != nil {
		t.Fatalf("reply %s: %v", last, err)
	}
	if len(reply.Result.Dirs) == 0 || reply.Result.Dirs[0] != dir {
		t.Errorf("fs.recentDirs = %q, want %s first", reply.Result.Dirs, dir)
	}
}
