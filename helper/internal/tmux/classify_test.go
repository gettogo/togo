package tmux

import (
	"errors"
	"testing"
)

func TestClassifyStderr(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		// wantIs is the sentinel the error must wrap; nil plus wantGeneric
		// means an error that wraps neither sentinel.
		wantIs      error
		wantGeneric bool
	}{
		{name: "no server running", stderr: "no server running on /tmp/x", wantIs: errNoServer},
		{name: "missing socket file", stderr: "error connecting to /tmp/x (No such file or directory)", wantIs: errNoServer},
		{name: "no such file without connect error", stderr: "open /x: No such file or directory", wantGeneric: true},
		{name: "server exited unexpectedly", stderr: "server exited unexpectedly", wantIs: errNoServer},
		{name: "lost server", stderr: "lost server", wantIs: errNoServer},
		{name: "permission denied", stderr: "error connecting to /tmp/x (Permission denied)", wantGeneric: true},
		{name: "can't find session", stderr: "can't find session: nope", wantIs: ErrNoSession},
		{name: "no such session", stderr: "no such session: =x", wantIs: ErrNoSession},
		{name: "empty", stderr: "", wantGeneric: true},
		{name: "unknown", stderr: "some unexpected failure", wantGeneric: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyStderr(tc.stderr)
			if err == nil {
				t.Fatalf("classifyStderr(%q) = nil, want an error", tc.stderr)
			}
			if tc.wantIs != nil {
				if !errors.Is(err, tc.wantIs) {
					t.Errorf("classifyStderr(%q) = %v, want errors.Is(err, %v)", tc.stderr, err, tc.wantIs)
				}
				return
			}
			if errors.Is(err, errNoServer) || errors.Is(err, ErrNoSession) {
				t.Errorf("classifyStderr(%q) = %v, want a generic error, not errNoServer or ErrNoSession", tc.stderr, err)
			}
		})
	}
}
