// Package tailnet reads this machine's Tailscale state from the tailscale
// command line tool: whether it is installed and connected, its MagicDNS
// name and its tailnet addresses. Tailscale being absent is a normal answer,
// never an error.
package tailnet

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AppBinary is the command line tool inside the Mac App Store and standalone
// Tailscale apps. Its file name is capitalised, unlike the Homebrew one.
const AppBinary = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"

// Timeout bounds one `tailscale status` call, so a wedged daemon never holds
// up host.info or doctor.
const Timeout = 2 * time.Second

// Status is what one `tailscale status --json` said about this machine.
type Status struct {
	// Running is true when the backend is connected to a tailnet.
	Running bool
	// State is the backend state as Tailscale names it, such as Running,
	// Stopped or NeedsLogin.
	State string
	// DNSName is the MagicDNS name without the trailing dot; empty when
	// MagicDNS is off.
	DNSName string
	// IPs are the machine's tailnet addresses, IPv4 first as Tailscale lists them.
	IPs []string
}

// Runner executes bin with args and returns its standard output.
type Runner func(ctx context.Context, bin string, args ...string) ([]byte, error)

// Locate returns the tailscale tool: the first executable named tailscale in
// the absolute directories of path, then in fallbacks, then the app bundle's
// binary. Empty when none is found.
func Locate(path string, fallbacks []string) string {
	dirs := append(filepath.SplitList(path), fallbacks...)
	for _, dir := range dirs {
		if !filepath.IsAbs(dir) {
			continue
		}
		if p := filepath.Join(dir, "tailscale"); executable(p) {
			return p
		}
	}
	if executable(AppBinary) {
		return AppBinary
	}
	return ""
}

func executable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// Query runs `tailscale status --json` with Timeout. An error means the tool
// did not answer, such as when the daemon is not running.
func Query(ctx context.Context, bin string, run Runner) (Status, error) {
	if run == nil {
		run = execRun
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	out, err := run(ctx, bin, "status", "--json", "--peers=false")
	if err != nil && len(out) == 0 {
		return Status{}, err
	}
	return Parse(out)
}

// Parse reads the parts of `tailscale status --json` Togo uses.
func Parse(out []byte) (Status, error) {
	var raw struct {
		BackendState string
		Self         *struct {
			DNSName      string
			TailscaleIPs []string
		}
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Status{}, err
	}
	st := Status{State: raw.BackendState, Running: raw.BackendState == "Running"}
	if raw.Self != nil && st.Running {
		st.DNSName = strings.TrimSuffix(raw.Self.DNSName, ".")
		st.IPs = append(st.IPs, raw.Self.TailscaleIPs...)
	}
	return st, nil
}

func execRun(ctx context.Context, bin string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, bin, args...).Output()
}
