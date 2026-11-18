package tmux

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const maxControlLine = 1 << 20

// ActivePane returns the stable pane id of the named session's active pane.
func (c *Client) ActivePane(ctx context.Context, session string) (string, error) {
	out, err := c.run(ctx, "list-panes", "-t", "="+escapeArg(session)+":", "-F", "#{pane_active} #{pane_id}")
	if err != nil {
		return "", noSession(err)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if pane, ok := strings.CutPrefix(line, "1 "); ok && validPane(pane) {
			return pane, nil
		}
	}
	return "", ErrNoSession
}

// ObservePane reads raw output without replacing pipe-pane or changing client
// size. Ready runs after attach succeeds. Cancellation joins this tmux client;
// the session and every program in it continue running.
func (c *Client) ObservePane(ctx context.Context, session, pane string, ready func(), output func([]byte) error) error {
	if !validPane(pane) {
		return errors.New("tmux: invalid observed pane")
	}
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := append([]string{"-u"}, c.Socket...)
	if c.Config != "" {
		args = append(args, "-f", c.Config)
	}
	args = append(args, "-C", "attach-session", "-f", "ignore-size", "-t", "="+escapeArg(session)+":")
	cmd := exec.CommandContext(childCtx, c.Bin, args...)
	cmd.Env = outsideTmux(os.Environ())
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("tmux control start: %w", err)
	}
	defer func() {
		cancel()
		stdin.Close()
		_ = cmd.Wait()
	}()
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 32*1024), maxControlLine)
	armed := false
	for sc.Scan() {
		line := sc.Bytes()
		if !armed {
			if bytes.HasPrefix(line, []byte("%error ")) || bytes.HasPrefix(line, []byte("%exit")) {
				return errors.New("tmux: control attachment failed")
			}
			if bytes.HasPrefix(line, []byte("%end ")) {
				armed = true
				if ready != nil {
					ready()
				}
			}
		}
		data, matched, err := controlOutput(line, pane)
		if err != nil {
			return err
		}
		if matched && output != nil {
			if err := output(data); err != nil {
				return err
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("tmux control read: %w", err)
	}
	return ErrNoSession
}

func validPane(pane string) bool {
	if len(pane) < 2 || len(pane) > 20 || pane[0] != '%' {
		return false
	}
	_, err := strconv.ParseUint(pane[1:], 10, 64)
	return err == nil
}

func controlOutput(line []byte, pane string) ([]byte, bool, error) {
	var data []byte
	if rest, ok := bytes.CutPrefix(line, []byte("%output "+pane+" ")); ok {
		data = rest
	} else if rest, ok := bytes.CutPrefix(line, []byte("%extended-output "+pane+" ")); ok {
		_, data, ok = bytes.Cut(rest, []byte(": "))
		if !ok {
			return nil, true, errors.New("tmux: malformed extended output")
		}
	} else {
		return nil, false, nil
	}
	out := make([]byte, 0, len(data))
	for at := 0; at < len(data); at++ {
		if data[at] != '\\' {
			out = append(out, data[at])
			continue
		}
		if at+3 >= len(data) || data[at+1] < '0' || data[at+1] > '3' ||
			data[at+2] < '0' || data[at+2] > '7' || data[at+3] < '0' || data[at+3] > '7' {
			return nil, true, errors.New("tmux: malformed output escape")
		}
		out = append(out, (data[at+1]-'0')<<6|(data[at+2]-'0')<<3|(data[at+3]-'0'))
		at += 3
	}
	return out, true, nil
}
