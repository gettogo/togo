package ports

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// processTable maps each parent pid to its direct children.
type processTable map[int][]int

// readProcessTable runs ps, which takes the same flags on macOS and Linux.
func readProcessTable(ctx context.Context) (processTable, error) {
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parsePS(string(out)), nil
}

// parsePS reads "pid ppid" lines, skipping any that do not parse.
func parsePS(out string) processTable {
	table := processTable{}
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

// descendants returns roots and every process below them. A visited set
// keeps a recycled pid from looping the walk.
func descendants(table processTable, roots []int) []int {
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
