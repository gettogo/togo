package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/daemon"
)

func TestStartDaemonVia(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		managed    bool
		runnerErr  error
		wantDetach bool
		wantArgv   []string
	}{
		{"unmanaged detaches", false, nil, true, nil},
		{"managed asks the manager", true, nil, false, []string{"launchctl", "kickstart", "gui/501/" + daemon.Label}},
		{"manager refusal falls back to detach", true, errors.New("refused"), true, []string{"launchctl", "kickstart", "gui/501/" + daemon.Label}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var argv []string
			var svc *daemon.Service
			if tc.managed {
				svc = &daemon.Service{Manager: daemon.Launchd, UID: 501, Run: func(_ context.Context, a []string) error {
					argv = slices.Clone(a)
					return tc.runnerErr
				}}
			}
			detached := false
			if err := startDaemonVia(ctx, "/h", svc, func(string) error { detached = true; return nil }); err != nil {
				t.Fatal(err)
			}
			if detached != tc.wantDetach {
				t.Errorf("detached = %v, want %v", detached, tc.wantDetach)
			}
			if !slices.Equal(argv, tc.wantArgv) {
				t.Errorf("argv = %v, want %v", argv, tc.wantArgv)
			}
		})
	}
}
