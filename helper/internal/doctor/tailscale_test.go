package doctor

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

// tailscaleRun answers `status` with status and `debug prefs` with prefs.
func tailscaleRun(status, prefs string, statusErr error) func(context.Context, string, ...string) (string, error) {
	return func(_ context.Context, bin string, args ...string) (string, error) {
		if filepath.Base(bin) != "tailscale" || len(args) == 0 {
			return "", nil
		}
		if args[0] == "status" {
			return status, statusErr
		}
		return prefs, nil
	}
}

const runningStatus = `{"BackendState":"Running","Self":{"DNSName":"studio.tail1234.ts.net.","TailscaleIPs":["100.101.102.103"]}}`

func TestDoctorTailscaleStates(t *testing.T) {
	bin := t.TempDir()
	stub(t, bin, "tailscale")
	run := func(status string, err error) Check {
		e := Env{Path: bin, GOOS: "darwin", Run: tailscaleRun(status, `{}`, err)}
		return byName(t, Run(context.Background(), e), "tailscale")
	}
	if c := run(runningStatus, nil); c.Status != StatusOK || c.Detail != "connected as studio.tail1234.ts.net (100.101.102.103)" {
		t.Errorf("running = %+v", c)
	}
	if c := run(`{"BackendState":"Running","Self":{"DNSName":"","TailscaleIPs":["100.1.2.3"]}}`, nil); c.Status != StatusOK || !strings.Contains(c.Detail, "MagicDNS") {
		t.Errorf("no MagicDNS = %+v", c)
	}
	if c := run(`{"BackendState":"Stopped"}`, nil); c.Status != StatusNote || c.Fix != "tailscale up" || !strings.Contains(c.Detail, "Stopped") {
		t.Errorf("stopped = %+v", c)
	}
	if c := run("", errors.New("exit 1")); c.Status != StatusNote || !strings.Contains(c.Detail, "not running") {
		t.Errorf("no daemon = %+v", c)
	}
}

func TestDoctorTailscaleMissing(t *testing.T) {
	checks := Run(context.Background(), Env{Path: t.TempDir(), GOOS: "linux", Run: tailscaleRun("", "", nil)})
	c := byName(t, checks, "tailscale")
	if c.Status != StatusNote || c.Fix != tailscaleDownload || !strings.Contains(c.Detail, "away from home") {
		t.Errorf("missing = %+v", c)
	}
}

func TestDoctorTailscaleAppBinary(t *testing.T) {
	app := stub(t, filepath.Join(t.TempDir(), "MacOS"), "Tailscale")
	e := Env{Path: t.TempDir(), GOOS: "darwin", TailscaleApp: app, Run: func(_ context.Context, bin string, args ...string) (string, error) {
		if bin == app && args[0] == "status" {
			return runningStatus, nil
		}
		return "", nil
	}}
	if c := byName(t, Run(context.Background(), e), "tailscale"); c.Status != StatusOK || c.Path != app {
		t.Errorf("app = %+v", c)
	}
}

func TestDoctorTailscaleSSH(t *testing.T) {
	bin := t.TempDir()
	stub(t, bin, "tailscale")
	run := func(prefs string) []Check {
		return Run(context.Background(), Env{Path: bin, GOOS: "linux", Run: tailscaleRun(runningStatus, prefs, nil)})
	}
	if c := byName(t, run(`{"RunSSH":true}`), "tailscale ssh"); c.Status != StatusNote {
		t.Fatalf("tailscale ssh = %+v", c)
	}
	for _, c := range run(`{"RunSSH":false}`) {
		if c.Name == "tailscale ssh" {
			t.Fatal("tailscale ssh check present with SSH off")
		}
	}
}

func TestDoctorRoutes(t *testing.T) {
	routes := []wire.HostRoute{{Kind: "mdns", Host: "studio.local"}, {Kind: "tailscale", Host: "studio.tail1234.ts.net"}}
	e := Env{Path: t.TempDir(), GOOS: "linux", Run: tailscaleRun("", "", nil), Routes: func(context.Context) []wire.HostRoute { return routes }}
	if c := byName(t, Run(context.Background(), e), "routes"); c.Status != StatusOK || c.Detail != "studio.local, studio.tail1234.ts.net" {
		t.Errorf("routes = %+v", c)
	}
	if c := checkRoutes(nil); c.Status != StatusWarn {
		t.Errorf("empty = %+v", c)
	}
	for _, c := range Run(context.Background(), Env{Path: t.TempDir(), GOOS: "linux", Run: tailscaleRun("", "", nil)}) {
		if c.Name == "routes" {
			t.Error("routes line without a Routes source")
		}
	}
}
