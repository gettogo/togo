// Package procs tells a harness source which processes belong to a session
// and which files they hold open, so a session follows what its own agent
// writes and not an agent the user started in the same folder.
package procs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Session is the process view of one tmux session. Panes lists the pane
// processes; everything below them is the session's.
type Session struct {
	Panes func(ctx context.Context) ([]int, error)
}

// Tree returns the pane processes and all their descendants, or nil when
// they cannot be listed.
func (s Session) Tree(ctx context.Context) []int {
	if s.Panes == nil {
		return nil
	}
	roots, err := s.Panes(ctx)
	if err != nil || len(roots) == 0 {
		return nil
	}
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	return Descendants(ParsePS(string(out)), roots)
}

// Holders returns, per path, the pids that have it open.
func (Session) Holders(ctx context.Context, paths []string) (map[string][]int, error) {
	return Holders(ctx, paths)
}

// ParsePS reads "pid ppid" lines into a parent -> children table.
func ParsePS(out string) map[int][]int {
	table := map[int][]int{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		table[ppid] = append(table[ppid], pid)
	}
	return table
}

// Descendants returns roots and every process below them.
func Descendants(table map[int][]int, roots []int) []int {
	seen := map[int]bool{}
	var order []int
	queue := append([]int(nil), roots...)
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		order = append(order, pid)
		queue = append(queue, table[pid]...)
	}
	return order
}

// Holders finds the processes (of this user) that have each path open:
// /proc/<pid>/fd on Linux, lsof elsewhere. Paths are compared after
// symlinks are resolved, so a /tmp or /var path matches what lsof prints.
func Holders(ctx context.Context, paths []string) (map[string][]int, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	real := map[string]string{} // resolved -> asked
	for _, p := range paths {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			r = p
		}
		real[r] = p
	}
	if _, err := os.Stat("/proc/self/fd"); err == nil {
		return procHolders(real)
	}
	return lsofHolders(ctx, real)
}

func procHolders(real map[string]string) (map[string][]int, error) {
	dirs, err := filepath.Glob("/proc/[0-9]*/fd/*")
	if err != nil {
		return nil, err
	}
	out := map[string][]int{}
	for _, d := range dirs {
		target, err := os.Readlink(d)
		if err != nil {
			continue
		}
		asked, ok := real[target]
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(strings.Split(d, "/")[2])
		if err != nil {
			continue
		}
		out[asked] = appendUnique(out[asked], pid)
	}
	return out, nil
}

func lsofHolders(ctx context.Context, real map[string]string) (map[string][]int, error) {
	args := []string{"-nP", "-Fpn", "--"}
	for r := range real {
		args = append(args, r)
	}
	out, err := exec.CommandContext(ctx, "lsof", args...).Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return nil, err
	}
	return ParseLsof(out, real), nil
}

// ParseLsof reads lsof -Fpn output: "p<pid>" then "n<name>" lines.
func ParseLsof(out []byte, real map[string]string) map[string][]int {
	res := map[string][]int{}
	pid := 0
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(string(line[1:]))
		case 'n':
			if asked, ok := real[string(line[1:])]; ok && pid > 0 {
				res[asked] = appendUnique(res[asked], pid)
			}
		}
	}
	return res
}

func appendUnique(s []int, v int) []int {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
