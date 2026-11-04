package ports

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// listeners joins the LISTEN rows of /proc/net/tcp and tcp6 with the socket
// inodes held in each pid's fd directory. Processes that exit mid-scan or
// belong to another user are skipped.
func listeners(ctx context.Context, pids []int) ([]Listener, error) {
	var socks []procSocket
	for _, name := range []string{"tcp", "tcp6"} {
		data, err := os.ReadFile("/proc/net/" + name)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read /proc/net/%s: %w", name, err)
		}
		socks = append(socks, parseProcNetTCP(string(data))...)
	}
	if len(socks) == 0 {
		return nil, nil
	}
	byInode := make(map[uint64]procSocket, len(socks))
	for _, s := range socks {
		byInode[s.Inode] = s
	}
	var found []Listener
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		inodes := socketInodes(pid)
		if len(inodes) == 0 {
			continue
		}
		command := processName(pid)
		for _, in := range inodes {
			if s, ok := byInode[in]; ok {
				found = append(found, Listener{Port: s.Port, Address: s.Address, PID: pid, Command: command})
			}
		}
	}
	return found, nil
}

func socketInodes(pid int) []uint64 {
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var inodes []uint64
	for _, e := range entries {
		link, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil || !strings.HasPrefix(link, "socket:[") {
			continue
		}
		if n, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]"), 10, 64); err == nil {
			inodes = append(inodes, n)
		}
	}
	return inodes
}

func processName(pid int) string {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
