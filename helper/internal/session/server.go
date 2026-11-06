package session

import (
	"context"
	"fmt"
)

// tmuxConf is the config of the togo tmux server, written by NewService.
const tmuxConf = `set -g mouse on
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

// KeepServer applies exit-empty off to a togo tmux server that is already
// running. tmux reads tmuxConf only when it starts a server, so a server
// started by an older helper would still exit with its last session, and
// the next start would offer that session as lost to a restart.
func (s *Service) KeepServer(ctx context.Context) error {
	if err := s.Tmux.SetServerOption(ctx, "exit-empty", "off"); err != nil {
		return fmt.Errorf("tmux exit-empty: %w", err)
	}
	return nil
}
