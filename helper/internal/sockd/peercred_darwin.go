package sockd

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// fdPeerUID reads the effective uid of the process on the other end of a
// connected unix socket.
func fdPeerUID(fd int) (int, error) {
	cred, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, fmt.Errorf("getsockopt LOCAL_PEERCRED: %w", err)
	}
	return int(cred.Uid), nil
}
