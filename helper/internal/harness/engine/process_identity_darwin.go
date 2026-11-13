package engine

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

func processBirth(pid int) (string, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	const zombie = 5 // SZOMB in sys/proc.h: waiting to be collected by its parent.
	if int(info.Proc.P_pid) != pid || info.Proc.P_stat == 0 || info.Proc.P_stat == zombie {
		return "", errors.New("process is no longer running")
	}
	return fmt.Sprintf("%d:%d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec), nil
}
