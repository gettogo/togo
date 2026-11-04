package daemon

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Label is the launchd job label and the reverse-DNS name of the service.
const Label = "dev.togoapp.togo"

const systemdUnit = "togo.service"

// Runner executes one service-manager command. Install and uninstall go
// through it so tests never reach the real launchctl or systemctl.
type Runner func(ctx context.Context, argv []string) error

// Manager names the per-user service manager a Service targets.
type Manager int

const (
	// Launchd is the macOS per-user LaunchAgent.
	Launchd Manager = iota
	// Systemd is a systemd user unit.
	Systemd
)

// Service installs `togo daemon` as a per-user service. Every path it writes
// is under Home; nothing needs root.
type Service struct {
	Manager Manager
	// Home is the user's home directory.
	Home string
	// TogoHome is passed to the daemon and holds its log.
	TogoHome string
	// Exe is the absolute path of the togo binary the service runs.
	Exe string
	UID int
	Run Runner
}

// Status reports whether the service file is present and whether the
// service manager has the job loaded.
type Status struct {
	Path      string
	Installed bool
	Loaded    bool
}

// Path returns the service file the manager reads.
func (s Service) Path() string {
	if s.Manager == Systemd {
		return filepath.Join(s.Home, ".config", "systemd", "user", systemdUnit)
	}
	return filepath.Join(s.Home, "Library", "LaunchAgents", Label+".plist")
}

func (s Service) logPath() string {
	return filepath.Join(s.TogoHome, "log", "daemon.log")
}

func (s Service) domain() string {
	return "gui/" + strconv.Itoa(s.UID)
}

// Install writes the service file and loads it. Running it again replaces
// the file and reloads the job, so a moved binary can be picked up.
func (s Service) Install(ctx context.Context) error {
	if !filepath.IsAbs(s.Exe) || !filepath.IsAbs(s.Home) || !filepath.IsAbs(s.TogoHome) {
		return errors.New("service install: binary, home and TOGO_HOME must be absolute paths")
	}
	if err := os.MkdirAll(filepath.Dir(s.logPath()), 0o700); err != nil {
		return fmt.Errorf("service log directory: %w", err)
	}
	content, err := s.render()
	if err != nil {
		return err
	}
	if s.Manager == Launchd && s.loaded(ctx) {
		// bootstrap refuses a label that is already loaded.
		if err := s.Run(ctx, []string{"launchctl", "bootout", s.domain() + "/" + Label}); err != nil {
			return fmt.Errorf("launchctl bootout: %w", err)
		}
	}
	if err := writeFile0644(s.Path(), content); err != nil {
		return err
	}
	if s.Manager == Systemd {
		if err := s.Run(ctx, []string{"systemctl", "--user", "daemon-reload"}); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %w", err)
		}
		if err := s.Run(ctx, []string{"systemctl", "--user", "enable", systemdUnit}); err != nil {
			return fmt.Errorf("systemctl enable: %w", err)
		}
		// restart rather than start, so a reinstall runs the new binary.
		if err := s.Run(ctx, []string{"systemctl", "--user", "restart", systemdUnit}); err != nil {
			return fmt.Errorf("systemctl restart: %w", err)
		}
		return nil
	}
	if err := s.Run(ctx, []string{"launchctl", "bootstrap", s.domain(), s.Path()}); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}
	return nil
}

// Uninstall stops the job and removes the service file. It touches no
// other file, and returns nil when the service was never installed.
func (s Service) Uninstall(ctx context.Context) error {
	if _, err := os.Lstat(s.Path()); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	switch {
	case s.Manager == Systemd:
		// An enabled unit that is not running still has its wants symlink,
		// so disable runs whatever the active state; it is idempotent.
		if err := s.Run(ctx, []string{"systemctl", "--user", "disable", "--now", systemdUnit}); err != nil {
			return fmt.Errorf("systemctl disable: %w", err)
		}
	case s.loaded(ctx):
		if err := s.Run(ctx, []string{"launchctl", "bootout", s.domain() + "/" + Label}); err != nil {
			return fmt.Errorf("launchctl bootout: %w", err)
		}
	}
	if err := os.Remove(s.Path()); err != nil {
		return fmt.Errorf("remove service file: %w", err)
	}
	if s.Manager == Systemd {
		if err := s.Run(ctx, []string{"systemctl", "--user", "daemon-reload"}); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %w", err)
		}
	}
	return nil
}

// Status reports the installed and loaded state without changing either.
func (s Service) Status(ctx context.Context) (Status, error) {
	st := Status{Path: s.Path()}
	_, err := os.Lstat(st.Path)
	switch {
	case err == nil:
		st.Installed = true
	case !errors.Is(err, os.ErrNotExist):
		return st, fmt.Errorf("service file: %w", err)
	}
	st.Loaded = s.loaded(ctx)
	return st, nil
}

// loaded asks the manager about the job; any failure, including the job
// being unknown, counts as not loaded.
func (s Service) loaded(ctx context.Context) bool {
	if s.Manager == Systemd {
		return s.Run(ctx, []string{"systemctl", "--user", "is-active", "--quiet", systemdUnit}) == nil
	}
	return s.Run(ctx, []string{"launchctl", "print", s.domain() + "/" + Label}) == nil
}

func (s Service) render() ([]byte, error) {
	if s.Manager == Systemd {
		return []byte(renderUnit(s.Exe, s.TogoHome)), nil
	}
	return renderPlist(s.Exe, s.TogoHome, s.logPath())
}

func renderPlist(exe, togoHome, logPath string) ([]byte, error) {
	esc := func(v string) (string, error) {
		var b bytes.Buffer
		if err := xml.EscapeText(&b, []byte(v)); err != nil {
			return "", fmt.Errorf("plist value: %w", err)
		}
		return b.String(), nil
	}
	var vals [3]string
	for i, v := range []string{exe, togoHome, logPath} {
		e, err := esc(v)
		if err != nil {
			return nil, err
		}
		vals[i] = e
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + vals[0] + `</string>
		<string>daemon</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>TOGO_HOME</key>
		<string>` + vals[1] + `</string>
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>StandardOutPath</key>
	<string>` + vals[2] + `</string>
	<key>StandardErrorPath</key>
	<string>` + vals[2] + `</string>
</dict>
</plist>
`), nil
}

func renderUnit(exe, togoHome string) string {
	return `[Unit]
Description=Togo helper daemon

[Service]
ExecStart=` + systemdQuote(exe) + ` daemon
Environment=` + systemdQuote("TOGO_HOME="+togoHome) + `
Restart=on-failure

[Install]
WantedBy=default.target
`
}

// systemdQuote double-quotes v for a unit file, where backslash and quote
// are escapes, % starts a specifier and $ starts a variable.
func systemdQuote(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)
	return `"` + r.Replace(v) + `"`
}

// writeFile0644 replaces path atomically with a world-readable file, the
// mode launchd and systemd expect for a user's service definitions.
func writeFile0644(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("service directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".togo-*")
	if err != nil {
		return fmt.Errorf("service file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("service file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("service file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("service file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("service file: %w", err)
	}
	return nil
}
