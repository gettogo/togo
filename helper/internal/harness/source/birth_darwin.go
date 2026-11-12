package source

import (
	"os"
	"syscall"
	"time"
)

func birthTime(st os.FileInfo) time.Time {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return time.Unix(s.Birthtimespec.Sec, s.Birthtimespec.Nsec)
	}
	return time.Time{}
}
