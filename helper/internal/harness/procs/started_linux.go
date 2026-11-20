package procs

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// clockTicks is USER_HZ, the unit of /proc/<pid>/stat's start time: 100 on
// every Linux architecture Go supports.
const clockTicks = 100

// Started returns when a live process started, by the wall clock: the
// boot time from /proc/stat plus the process's start in clock ticks.
func Started(pid int) (time.Time, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return time.Time{}, err
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return time.Time{}, errors.New("invalid process stat")
	}
	fields := strings.Fields(string(b[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return time.Time{}, errors.New("process is no longer running")
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	stat, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	for line := range strings.SplitSeq(string(stat), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			boot, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(boot, 0).Add(time.Duration(ticks) * time.Second / clockTicks), nil
		}
	}
	return time.Time{}, errors.New("no boot time in /proc/stat")
}
