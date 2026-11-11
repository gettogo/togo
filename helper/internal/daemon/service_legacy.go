package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// legacyLabels are launchd labels earlier builds installed the service
// under. Install and Uninstall retire them, so a Mac that ran an older
// build never ends up with two daemons started at login.
var legacyLabels = []string{"io.github.tvlha.togo"}

// retireLegacy stops and deletes every LaunchAgent left under a legacy
// label. It acts only on a label whose plist is still on disk, and it
// deletes the plist only after the job is stopped, so running it again,
// after a success or a failure, is safe and finishes the job.
func (s Service) retireLegacy(ctx context.Context) error {
	if s.Manager != Launchd {
		return nil
	}
	for _, label := range legacyLabels {
		path := s.launchAgentPath(label)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if s.launchdLoaded(ctx, label) {
			if err := s.Run(ctx, []string{"launchctl", "bootout", s.domain() + "/" + label}); err != nil {
				return fmt.Errorf("launchctl bootout %s: %w", label, err)
			}
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove old service file: %w", err)
		}
	}
	return nil
}

// legacyInstalled returns the first legacy label whose plist is on disk.
func (s Service) legacyInstalled() (string, bool) {
	if s.Manager != Launchd {
		return "", false
	}
	for _, label := range legacyLabels {
		if _, err := os.Lstat(s.launchAgentPath(label)); err == nil {
			return label, true
		}
	}
	return "", false
}

// HasFile reports whether a service file is on disk, under the current
// label or a legacy one.
func (s Service) HasFile() bool {
	if _, err := os.Lstat(s.Path()); err == nil {
		return true
	}
	_, ok := s.legacyInstalled()
	return ok
}
