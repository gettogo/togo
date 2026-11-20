package enginetest

import (
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The stand-in for the server OpenCode 2.x runs for a session. The engine
// starts it as it starts the real one: `opencode serve --hostname
// 127.0.0.1 --port N` found on the login PATH, in its own process group,
// with the password in OPENCODE_PASSWORD (engine/server.go startServer).
// The rig's fake `opencode` hands `serve` to this test binary, which
// becomes the stand-in: a separate process that listens on the port,
// checks the password as the real server does (Basic auth, user
// "opencode"), and passes each request to the rig's APIServer in the
// test process, which holds the journal, the recorded calls and the fault
// switches. It outlives a helper restart, as the real server does, so a
// restarted engine finds it again by its record under TOGO_HOME/run.
//
// It ends on SIGTERM (the engine stopping it), when the rig removes its
// folder's upstream file, or when the test process that started it is
// gone.

// standInEnv names the rig folder the stand-in reads its upstream from.
const standInEnv = "TOGO_TEST_OPENCODE_STANDIN"

// StandIn runs the stand-in when this test binary was started as one, and
// then does not return. Every test package that runs a rig for OpenCode
// 2.x calls it first in TestMain.
func StandIn() {
	dir := os.Getenv(standInEnv)
	if dir == "" {
		return
	}
	os.Exit(runStandIn(dir, os.Args[1:]))
}

// upstreamFile holds the rig's APIServer address and the password it
// takes, one per line.
func upstreamFile(dir string) string { return filepath.Join(dir, "upstream") }

func runStandIn(dir string, args []string) int {
	port := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--port" {
			port = args[i+1]
		}
	}
	password := os.Getenv("OPENCODE_PASSWORD")
	b, err := os.ReadFile(upstreamFile(dir))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if err != nil || port == "" || password == "" || len(lines) != 2 {
		return 2
	}
	target, secret := lines[0], lines[1]
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return 3
	}
	_ = os.WriteFile(filepath.Join(dir, "standin-"+strconv.Itoa(os.Getpid())), []byte(port), 0o600)
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:"+password))
	up := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:"+secret))
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: target})
	proxy.FlushInterval = -1 // the event stream goes through as it is written
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) { w.WriteHeader(http.StatusBadGateway) }
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		r.Header.Set("Authorization", up)
		proxy.ServeHTTP(w, r)
	})
	go func() { _ = http.Serve(ln, h) }()
	parent := os.Getppid()
	for {
		time.Sleep(100 * time.Millisecond)
		if os.Getppid() != parent {
			return 0
		}
		if _, err := os.Stat(upstreamFile(dir)); err != nil {
			return 0
		}
	}
}
