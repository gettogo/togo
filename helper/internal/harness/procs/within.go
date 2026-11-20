package procs

import (
	"context"
	"errors"
)

// Within counts the processes in the session's tree whose program is one
// of commands (as MatchCommands reads them). It is an error when the tree
// or the processes cannot be listed: the answer is then unknown.
func (s Session) Within(ctx context.Context, commands []string) (int, error) {
	tree := s.Tree(ctx)
	if len(tree) == 0 {
		return 0, errors.New("procs: session tree unknown")
	}
	out, err := run(ctx, "ps", "-A", "-o", "pid=,command=")
	if err != nil {
		return 0, err
	}
	own := map[int]bool{}
	for _, p := range tree {
		own[p] = true
	}
	n := 0
	for _, pid := range MatchCommands(string(out), commands) {
		if own[pid] {
			n++
		}
	}
	return n, nil
}
