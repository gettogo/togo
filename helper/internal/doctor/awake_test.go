package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/awake"
)

func TestAwakeCheck(t *testing.T) {
	for _, c := range []struct {
		name   string
		st     awake.Status
		live   bool
		status Status
		detail string
		fix    string
	}{
		{"plugged idle", awake.Status{Mode: awake.ModePlugged, Reason: awake.ReasonIdle, PlatformSupported: true}, true,
			StatusOK, "plugged: awake while sessions work, lid closed only on AC; not holding now", ""},
		{"plugged holding", awake.Status{Mode: awake.ModePlugged, Held: true, Reason: awake.ReasonActive, PlatformSupported: true}, true,
			StatusOK, "plugged: awake while sessions work, lid closed only on AC; holding now", ""},
		{"battery ready", awake.Status{Mode: awake.ModeBattery, Reason: awake.ReasonIdle, PlatformSupported: true}, true,
			StatusOK, "battery: awake while sessions work, lid closed on AC or battery; not holding now", ""},
		{"battery not set up", awake.Status{Mode: awake.ModeBattery, Held: true, Reason: awake.ReasonNeedsAdmin, NeedsAdmin: true, PlatformSupported: true}, true,
			StatusWarn, "battery mode is not set up; a closed lid on battery still sleeps", "togo awake battery"},
		{"failed", awake.Status{Mode: awake.ModePlugged, Reason: awake.ReasonFailed, Detail: "Access denied", PlatformSupported: true}, true,
			StatusWarn, "plugged: cannot hold the machine awake: Access denied", ""},
		{"off", awake.Status{Mode: awake.ModeOff, Reason: awake.ReasonOff, PlatformSupported: true}, true,
			StatusNote, "off: sleeps as usual while sessions work", "togo awake plugged"},
		{"unsupported", awake.Status{Mode: awake.ModePlugged, Reason: awake.ReasonUnsupported}, true,
			StatusNote, "keeping awake is not supported on this system", ""},
		{"no daemon", awake.Status{Mode: awake.ModePlugged}, false,
			StatusOK, "plugged: awake while sessions work, lid closed only on AC; daemon not running", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := AwakeCheck(c.st, c.live)
			if got.Name != "awake" || got.Status != c.status || got.Detail != c.detail || got.Fix != c.fix {
				t.Errorf("got %+v\nwant %s %q fix %q", got, c.status, c.detail, c.fix)
			}
		})
	}
}

func TestRunAppendsAwake(t *testing.T) {
	e := Env{GOOS: "linux", Run: fakeRun(nil), Awake: func(context.Context) Check { return Check{Name: "awake"} }}
	checks := Run(context.Background(), e)
	if last := checks[len(checks)-1]; !strings.EqualFold(last.Name, "awake") {
		t.Fatalf("last check %+v, want awake", last)
	}
}
