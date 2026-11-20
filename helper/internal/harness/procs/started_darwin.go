package procs

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

// Started returns when a live process started, by the wall clock.
func Started(pid int) (time.Time, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return time.Time{}, err
	}
	const zombie = 5
	if int(info.Proc.P_pid) != pid || info.Proc.P_stat == 0 || info.Proc.P_stat == zombie {
		return time.Time{}, errors.New("process is no longer running")
	}
	st := info.Proc.P_starttime
	return time.Unix(st.Sec, int64(st.Usec)*1000), nil
}
