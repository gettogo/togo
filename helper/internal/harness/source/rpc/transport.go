package rpc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

// MaxMessage bounds one incoming message. Codex sends whole command
// outputs and turn histories in single messages.
const MaxMessage = 16 << 20

// SocketPath turns an endpoint ("unix:///path", "unix://path" or a plain
// absolute path) into a socket path. Endpoints come from the helper, never
// from an adapter file, but the path is still checked to be absolute and
// clean.
func SocketPath(endpoint string) (string, error) {
	p := strings.TrimPrefix(endpoint, "unix://")
	if p == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return "", fmt.Errorf("rpc: endpoint is not an absolute socket path")
	}
	return p, nil
}

// DialUnix opens a WebSocket over the unix socket at path, the transport
// `codex app-server --listen unix://PATH` serves: one JSON-RPC message per
// text frame.
func DialUnix(ctx context.Context, path string) (Transport, error) {
	if fi, err := os.Stat(path); err != nil {
		return nil, err
	} else if fi.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("rpc: %s is not a socket", filepath.Base(path))
	}
	hc := &http.Client{Transport: &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}}
	c, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: hc})
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(MaxMessage)
	return &wsTransport{c: c}, nil
}

type wsTransport struct{ c *websocket.Conn }

func (t *wsTransport) Read(ctx context.Context) ([]byte, error) {
	for {
		typ, b, err := t.c.Read(ctx)
		if err != nil {
			return nil, err
		}
		if typ == websocket.MessageText {
			return b, nil
		}
	}
}

func (t *wsTransport) Write(ctx context.Context, b []byte) error {
	return t.c.Write(ctx, websocket.MessageText, b)
}

func (t *wsTransport) Close() error { return t.c.Close(websocket.StatusNormalClosure, "") }

// Lines is a transport of newline-delimited JSON over a reader and a
// writer, as a child process speaks on stdio.
func Lines(r io.Reader, w io.Writer, closer func() error) Transport {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), MaxMessage)
	return &lineTransport{sc: sc, w: w, closer: closer}
}

type lineTransport struct {
	mu     sync.Mutex
	sc     *bufio.Scanner
	w      io.Writer
	closer func() error
}

func (t *lineTransport) Read(context.Context) ([]byte, error) {
	for t.sc.Scan() {
		b := t.sc.Bytes()
		if len(strings.TrimSpace(string(b))) == 0 {
			continue
		}
		out := make([]byte, len(b))
		copy(out, b)
		return out, nil
	}
	if err := t.sc.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func (t *lineTransport) Write(_ context.Context, b []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, err := t.w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func (t *lineTransport) Close() error {
	if t.closer == nil {
		return nil
	}
	return t.closer()
}

// errNoEndpoint is returned when a session has no helper-assigned endpoint.
var errNoEndpoint = errors.New("rpc: session has no endpoint")
