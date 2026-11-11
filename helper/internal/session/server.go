package session

import (
	"context"
	"fmt"
)

// tmuxConf is the config of the togo tmux server, written by NewService.
var tmuxConf = baseConf + wheelConf()

const baseConf = `set -g mouse on
set -g set-clipboard on
set -s escape-time 0
set -s exit-empty off
set -g history-limit 50000
set -g default-terminal "tmux-256color"
set -g status off
set-environment -g COLORTERM truecolor
set -as terminal-features 'xterm*:RGB'
set -g allow-passthrough on
set -s extended-keys on
set -as terminal-features 'xterm*:extkeys'
`

// wheelBinding is one root-table mouse wheel binding.
type wheelBinding struct {
	Key string
	Cmd string
}

// wheelBindings returns the wheel bindings with target as the pane the event
// is for. The app sends wheel events for a vertical swipe. A pane already in
// a mode, or whose program asked for the mouse, gets the event as is. On the
// alternate screen without mouse reporting (a full-screen TUI that ignores
// the mouse) tmux's default would enter copy-mode, which has no history there
// and freezes the pane, so the wheel becomes arrow keys instead (xterm's
// alternate scroll). Elsewhere a wheel up enters copy-mode for scrollback.
// Only if-shell -F and plain commands are used, so the text parses on tmux
// 3.2.
func wheelBindings(target string) []wheelBinding {
	const passthrough = "#{||:#{pane_in_mode},#{mouse_any_flag}}"
	return []wheelBinding{
		{"WheelUpPane", fmt.Sprintf(
			`if -F -t %[1]s "%[2]s" "send -M" "if -F -t %[1]s '#{alternate_on}' 'send-keys -t %[1]s -N 3 Up' 'copy-mode -e -t %[1]s'"`,
			target, passthrough)},
		{"WheelDownPane", fmt.Sprintf(
			`if -F -t %[1]s "%[2]s" "send -M" "if -F -t %[1]s '#{alternate_on}' 'send-keys -t %[1]s -N 3 Down' 'send -M'"`,
			target, passthrough)},
	}
}

func wheelConf() string {
	var s string
	for _, b := range wheelBindings("=") {
		s += "bind -n " + b.Key + " " + b.Cmd + "\n"
	}
	return s
}

// KeepServer applies exit-empty off and the wheel bindings to a togo tmux
// server that is already running. tmux reads tmuxConf only when it starts a
// server, so a server started by an older helper would still exit with its
// last session (the next start would offer that session as lost to a
// restart) and would freeze full-screen apps on a swipe. Rebinding is
// idempotent.
func (s *Service) KeepServer(ctx context.Context) error {
	if err := s.Tmux.SetServerOption(ctx, "exit-empty", "off"); err != nil {
		return fmt.Errorf("tmux exit-empty: %w", err)
	}
	for _, b := range wheelBindings("=") {
		if err := s.Tmux.BindRoot(ctx, b.Key, b.Cmd); err != nil {
			return fmt.Errorf("tmux bind %s: %w", b.Key, err)
		}
	}
	return nil
}
