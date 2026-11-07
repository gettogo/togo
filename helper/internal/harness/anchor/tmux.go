package anchor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux"
)

// maxHistoryLines bounds a history search.
const maxHistoryLines = 50000

// TmuxPane reads panes through the togo tmux server.
type TmuxPane struct{ Client *tmux.Client }

func (p TmuxPane) run(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"-u"}, p.Client.Socket...)
	if p.Client.Config != "" {
		full = append(full, "-f", p.Client.Config)
	}
	cmd := exec.CommandContext(ctx, p.Client.Bin, append(full, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", args[0], err)
	}
	return string(out), nil
}

func target(id string) (string, error) {
	if _, err := session.ValidateID(id); err != nil {
		return "", err
	}
	return "=" + id + ":", nil
}

// Position reads history size, cursor row and history limit.
func (p TmuxPane) Position(ctx context.Context, id string) (Position, error) {
	t, err := target(id)
	if err != nil {
		return Position{}, err
	}
	out, err := p.run(ctx, "display-message", "-p", "-t", t, "#{history_size} #{cursor_y} #{history_limit}")
	if err != nil {
		return Position{}, err
	}
	return ParsePosition(out)
}

// ParsePosition reads "history cursor limit".
func ParsePosition(out string) (Position, error) {
	f := strings.Fields(out)
	if len(f) != 3 {
		return Position{}, errors.New("anchor: unexpected position output")
	}
	var n [3]int64
	for i, s := range f {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v < 0 {
			return Position{}, errors.New("anchor: unexpected position output")
		}
		n[i] = v
	}
	return Position{History: n[0], CursorY: n[1], Limit: n[2]}, nil
}

// Lines captures lines from..to relative to the top of the screen.
func (p TmuxPane) Lines(ctx context.Context, id string, from, to int64) ([]string, error) {
	t, err := target(id)
	if err != nil {
		return nil, err
	}
	out, err := p.run(ctx, "capture-pane", "-p", "-J", "-t", t,
		"-S", strconv.FormatInt(from, 10), "-E", strconv.FormatInt(to, 10))
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n"), nil
}

// History captures the whole history and screen.
func (p TmuxPane) History(ctx context.Context, id string) ([]string, error) {
	t, err := target(id)
	if err != nil {
		return nil, err
	}
	out, err := p.run(ctx, "capture-pane", "-p", "-t", t, "-S", "-", "-E", "-")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) > maxHistoryLines {
		// Keep absolute indices: blank out what is not searched.
		for i := range lines[:len(lines)-maxHistoryLines] {
			lines[i] = ""
		}
	}
	return lines, nil
}

// Screen captures the visible screen as text, for screen-based checks.
func (p TmuxPane) Screen(ctx context.Context, id string) (string, error) {
	t, err := target(id)
	if err != nil {
		return "", err
	}
	return p.run(ctx, "capture-pane", "-p", "-t", t)
}
