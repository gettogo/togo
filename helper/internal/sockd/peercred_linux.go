package sockd

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// fdPeerUID reads the uid of the process on the other end of a connected
// unix socket, as recorded at connect time.
func fdPeerUID(fd int) (int, error) {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, fmt.Errorf("getsockopt SO_PEERCRED: %w", err)
	}
	return int(cred.Uid), nil
}
