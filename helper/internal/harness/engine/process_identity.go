package engine

import "syscall"

// A pid alone is not ownership: the kernel may give it to another process.
type processIdentity struct {
	PID   int    `json:"pid"`
	Birth string `json:"birth"`
}

func identifyProcess(pid int) (processIdentity, error) {
	birth, err := processBirth(pid)
	return processIdentity{PID: pid, Birth: birth}, err
}

func (p processIdentity) alive() bool {
	if p.PID <= 1 || p.Birth == "" {
		return false
	}
	birth, err := processBirth(p.PID)
	return err == nil && birth == p.Birth
}

func (p processIdentity) ownsGroup() bool {
	group, err := syscall.Getpgid(p.PID)
	return err == nil && group == p.PID && p.alive()
}
