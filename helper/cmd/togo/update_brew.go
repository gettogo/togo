package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gettogo/togo/helper/internal/update"
)

// brewRun runs Homebrew with an argv array and extra environment. It
// returns Homebrew's stderr. Tests replace it.
var brewRun = func(ctx context.Context, brew string, args, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, brew, args...)
	cmd.Env = append(os.Environ(), env...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return errBuf.String(), err
}

// brewInstalledVersion reports the version of the togo that Homebrew
// installed under prefix. Tests replace it.
var brewInstalledVersion = func(ctx context.Context, prefix string) (string, error) {
	bin := filepath.Join(prefix, "opt", "togo", "bin", "togo")
	out, err := exec.CommandContext(ctx, bin, "version").Output()
	if err != nil {
		return "", fmt.Errorf("check the installed version: %w", err)
	}
	for _, f := range strings.Fields(string(out)) {
		if update.ValidVersion(f) {
			return f, nil
		}
	}
	return "", fmt.Errorf("check the installed version: unexpected output %q", strings.TrimSpace(string(out)))
}

// brewUpgrade runs `brew upgrade togo` for the Homebrew install under
// prefix and confirms that it reached target.
func brewUpgrade(ctx context.Context, prefix, target string, stdout io.Writer) error {
	brew := filepath.Join(prefix, "bin", "brew")
	if info, err := os.Stat(brew); err != nil || info.IsDir() {
		return fmt.Errorf("Homebrew not found at %s; run brew upgrade togo", brew)
	}
	env := []string{"NONINTERACTIVE=1", "HOMEBREW_NO_ENV_HINTS=1", "HOMEBREW_NO_INSTALL_CLEANUP=1"}
	if stderr, err := brewRun(ctx, brew, []string{"upgrade", "gettogo/tap/togo"}, env); err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			return fmt.Errorf("brew upgrade togo failed: %w", err)
		}
		return fmt.Errorf("brew upgrade togo failed: %s", msg)
	}
	got, err := brewInstalledVersion(ctx, prefix)
	if err != nil {
		return err
	}
	if got != target {
		return errors.New("Homebrew doesn't have togo " + target + " yet. Try again later.")
	}
	fmt.Fprintf(stdout, "Installed togo %s with Homebrew\n", target)
	return nil
}
