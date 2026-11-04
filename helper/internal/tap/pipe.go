package tap

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux"
)

// PipeCommand returns the shell command tmux runs to feed a pane's output to
// `togo tap`. tmux hands pipe-pane's argument to a shell, so the path is
// single-quoted and the id is restricted to the session id alphabet; both
// are checked here because the string is the only place user-influenced text
// reaches a shell.
func PipeCommand(togoPath, sessionID string) (string, error) {
	if _, err := session.ValidateID(sessionID); err != nil {
		return "", err
	}
	if !filepath.IsAbs(togoPath) {
		return "", fmt.Errorf("tap: helper path %q is not absolute", togoPath)
	}
	if strings.ContainsRune(togoPath, 0) {
		return "", fmt.Errorf("tap: helper path contains a NUL byte")
	}
	quoted := "'" + strings.ReplaceAll(togoPath, "'", `'\''`) + "'"
	return "exec " + quoted + " tap " + sessionID, nil
}

// Attach pipes the session's pane output to `togo tap`.
func Attach(ctx context.Context, c *tmux.Client, sessionID, togoPath string) error {
	cmd, err := PipeCommand(togoPath, sessionID)
	if err != nil {
		return err
	}
	return c.PipePane(ctx, "="+sessionID+":", cmd)
}

// Detach stops piping the session's pane output.
func Detach(ctx context.Context, c *tmux.Client, sessionID string) error {
	if _, err := session.ValidateID(sessionID); err != nil {
		return err
	}
	return c.PipePane(ctx, "="+sessionID+":", "")
}
