package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/mailbox"
	"github.com/gettogo/togo/helper/internal/state"
)

func init() {
	register("service", func(ctx context.Context, args []string, _ map[string]string, _ io.Reader, stdout, stderr io.Writer) int {
		return runService(ctx, args, stdout, stderr)
	})
	addUsage("  service install|uninstall|status              run the daemon at login as a user service")
}

// serviceRunner runs launchctl or systemctl. Tests replace it so they never
// reach the real service manager.
var serviceRunner daemon.Runner = func(ctx context.Context, argv []string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(out.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func runService(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return usageError(stderr, fmt.Errorf("service needs one of install, uninstall, status"))
	}
	if !slices.Contains([]string{"install", "uninstall", "status"}, args[0]) {
		return usageError(stderr, fmt.Errorf("unknown service action %q", args[0]))
	}
	svc, err := userService()
	if err != nil {
		return fail(stderr, err)
	}
	switch args[0] {
	case "install":
		if err := svc.Install(ctx); err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stdout, "Installed %s\n", svc.Path())
	case "uninstall":
		if err := uninstallService(ctx, svc, stderr); err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintln(stdout, "Uninstalled")
	case "status":
		st, err := svc.Status(ctx)
		if err != nil {
			return fail(stderr, err)
		}
		switch {
		case !st.Installed:
			fmt.Fprintln(stdout, "Not installed")
		case st.Loaded:
			fmt.Fprintf(stdout, "Installed and running from %s\n", st.Path)
		default:
			fmt.Fprintf(stdout, "Installed but not running: %s\n", st.Path)
		}
	}
	return exitOK
}

func userService() (daemon.Service, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return daemon.Service{}, err
	}
	togoHome, err := state.Home()
	if err != nil {
		return daemon.Service{}, err
	}
	exe, err := executable()
	if err != nil {
		return daemon.Service{}, err
	}
	return daemon.Service{
		Manager:  daemon.DefaultManager,
		Home:     home,
		TogoHome: togoHome,
		Exe:      exe,
		UID:      os.Getuid(),
		Run:      serviceRunner,
	}, nil
}

// uninstallService stops and removes the service. When a service is
// installed it first writes the stop marker, which tells the daemon the stop
// is on purpose, so it withdraws the silence alert it left with the relay
// instead of letting the relay report the machine as gone quiet. If the
// uninstall fails the daemon may still be running, so the marker is removed
// again: a later OS shutdown must leave the alert armed.
func uninstallService(ctx context.Context, svc daemon.Service, stderr io.Writer) error {
	marker := filepath.Join(svc.TogoHome, mailbox.StopMarkerName)
	wrote := false
	if _, err := os.Lstat(svc.Path()); err == nil {
		if err := state.WriteFile(marker, nil); err != nil {
			fmt.Fprintf(stderr, "togo: %v\n", err)
		} else {
			wrote = true
		}
	}
	err := svc.Uninstall(ctx)
	if err != nil && wrote {
		_ = os.Remove(marker)
	}
	return err
}
