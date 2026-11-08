package doctor

import (
	"context"
	"net"
	"time"
)

// sshListening dials the local sshd. Remote Login is off by default on
// macOS, and the phone then only sees "could not be reached".
func sshListening(ctx context.Context) bool {
	d := net.Dialer{Timeout: time.Second}
	c, err := d.DialContext(ctx, "tcp", "127.0.0.1:22")
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// checkRemoteLogin reports whether the phone can reach this machine over
// SSH at all.
func checkRemoteLogin(ctx context.Context, e Env) Check {
	c := Check{Name: "ssh"}
	if e.SSHListening(ctx) {
		c.Status, c.Detail = StatusOK, "SSH answers on port 22"
		return c
	}
	c.Status = StatusMissing
	if e.GOOS == "darwin" {
		c.Detail = "Remote Login is off, so the phone cannot connect"
		c.Fix = "Turn on System Settings › General › Sharing › Remote Login"
		return c
	}
	c.Detail = "Nothing answers on SSH port 22, so the phone cannot connect"
	c.Fix = "Install and start the OpenSSH server (sshd)"
	return c
}
