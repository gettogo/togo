package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/gettogo/togo/helper/internal/tmux"
)

// tmuxConf is the config of the togo tmux server, written by NewService.
var tmuxConf = baseConf + mouseConf()

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

// mouseBinding is one root-table mouse binding. An empty Cmd unbinds Key.
type mouseBinding struct {
	Key string
	Cmd string
}

// mouseBindings returns the root-table mouse bindings with target as the
// pane the event is for.
//
// The app turns a swipe into wheel events or a one-finger left-button drag,
// and a two-finger tap into a right click. tmux's defaults assume a mouse
// and a person who can leave copy-mode: for a full-screen app on the
// alternate screen that did not ask for the mouse they enter copy-mode,
// which has no history there, and the pane freezes on "[n/0]" until Esc.
// A pane already in a mode, or whose program asked for the mouse, gets the
// event as is. On the alternate screen without mouse reporting the wheel
// becomes arrow keys (xterm's alternate scroll) and drags and clicks do
// nothing; the app has its own selection. Elsewhere the defaults stand: the
// wheel up and a drag enter copy-mode.
//
// The wheel bindings are needed on tmux 3.4 and older, whose default wheel
// binding lacks alternate_on (3.6a's has it; the drag and click defaults
// still lack it). The right click menu, whose entries kill and respawn the
// pane, is removed: a stray two-finger tap must not offer it.
//
// Only if-shell -F and plain commands are used, so the text parses on tmux
// 3.2. A binding is one command: a top-level ";" would end the line in
// tmux.conf, so the first click's select-pane (MouseDown1Pane) is not repeated.
func mouseBindings(target string) []mouseBinding {
	const pass = "#{||:#{pane_in_mode},#{mouse_any_flag}}"
	ifs := func(cond, yes, no string) string {
		return fmt.Sprintf(`if -F -t %s "%s" "%s" "%s"`, target, cond, yes, no)
	}
	alt := func(yes, no string) string { return ifs("#{alternate_on}", yes, no) }
	// quoted nests a command inside one level of double quotes.
	quoted := func(cmd string) string { return strings.NewReplacer(`"`, `'`).Replace(cmd) }
	arrow := func(key string) string { return fmt.Sprintf("send-keys -t %s -N 3 %s", target, key) }
	multi := func(word string) string {
		return fmt.Sprintf("copy-mode -H -t %[1]s ; send-keys -t %[1]s -X %s ; run-shell -d 0.3 ; send-keys -t %[1]s -X copy-pipe-and-cancel", target, word)
	}
	return []mouseBinding{
		{"WheelUpPane", ifs(pass, "send -M", quoted(alt(arrow("Up"), "copy-mode -e -t "+target)))},
		{"WheelDownPane", ifs(pass, "send -M", quoted(alt(arrow("Down"), "send -M")))},
		{"MouseDrag1Pane", ifs(pass, "send -M", quoted(alt("", "copy-mode -M -t "+target)))},
		{"DoubleClick1Pane", ifs(pass, "send -M", quoted(alt("", multi("select-word"))))},
		{"TripleClick1Pane", ifs(pass, "send -M", quoted(alt("", multi("select-line"))))},
		{"MouseDown3Pane", fmt.Sprintf(`if -F -t %s "#{mouse_any_flag}" "send -M"`, target)},
		{"M-MouseDown3Pane", ""},
		{"MouseDown3Status", ""},
		{"M-MouseDown3Status", ""},
		{"MouseDown3StatusLeft", ""},
		{"M-MouseDown3StatusLeft", ""},
	}
}

func mouseConf() string {
	var s string
	for _, b := range mouseBindings("=") {
		if b.Cmd == "" {
			s += "unbind -n " + b.Key + "\n"
			continue
		}
		s += "bind -n " + b.Key + " " + b.Cmd + "\n"
	}
	return s
}

// KeepServer applies exit-empty off and the mouse bindings to a togo tmux
// server that is already running. tmux reads tmuxConf only when it starts a
// server, so a server started by an older helper would still exit with its
// last session (the next start would offer that session as lost to a
// restart) and would freeze full-screen apps on a swipe or tap. Rebinding is
// idempotent.
func (s *Service) KeepServer(ctx context.Context) error {
	if err := s.Tmux.SetServerOption(ctx, "exit-empty", "off"); err != nil {
		return fmt.Errorf("tmux exit-empty: %w", err)
	}
	var bind []tmux.RootBinding
	for _, b := range mouseBindings("=") {
		bind = append(bind, tmux.RootBinding{Key: b.Key, Cmd: b.Cmd})
	}
	if err := s.Tmux.BindRoot(ctx, bind); err != nil {
		return fmt.Errorf("tmux mouse bindings: %w", err)
	}
	return nil
}
