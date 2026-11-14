package procs

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// Birth returns a live process's kernel start identity, preventing PID reuse
// from being mistaken for continued ownership of an endpoint or pane.
func Birth(pid int) (string, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	const zombie = 5
	if int(info.Proc.P_pid) != pid || info.Proc.P_stat == 0 || info.Proc.P_stat == zombie {
		return "", errors.New("process is no longer running")
	}
	return fmt.Sprintf("%d:%d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec), nil
}
