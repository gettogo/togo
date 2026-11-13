package mailbox

import (
	"bytes"
	"context"
	"os"
	"time"

	"github.com/coder/websocket"
)

// DisarmFrame withdraws the will stored with the relay.
const DisarmFrame = `{"type":"will","v":1,"pushes":[]}`

// StopMarkerName is the file in $TOGO_HOME that `togo service uninstall` and
// `togo uninstall` write before stopping the daemon. Its presence when the
// daemon shuts down means the stop was deliberate, so the will is withdrawn;
// a shutdown without it (an OS shutdown, a service manager restart) leaves
// the will armed.
const StopMarkerName = "stopping"

// disarmTimeout bounds the disarm sent while shutting down.
const disarmTimeout = 2 * time.Second

// WillSource supplies the sealed will for each relay. Will reports false
// while there is nothing to send yet. Changed returns a channel closed when
// the will is rebuilt; take it before calling Will.
type WillSource interface {
	Will(relayURL string) ([]byte, bool)
	Changed() <-chan struct{}
}

// sendWill writes the will for relayURL when it differs from what this
// connection last sent.
func (r *runner) sendWill(ctx context.Context, conn *websocket.Conn, relayURL string, last *[]byte) error {
	frame, ok := r.cfg.Will.Will(relayURL)
	if !ok || bytes.Equal(frame, *last) {
		return nil
	}
	// Its own deadline, not ctx: the websocket closes the connection when a
	// write's context ends, which would lose a disarm sent just after.
	if err := writeControl(ctx, conn, frame); err != nil {
		return err
	}
	*last = frame
	return nil
}

// stopping reports whether the stop marker exists.
func (r *runner) stopping() bool {
	if r.cfg.StopMarker == "" {
		return false
	}
	_, err := os.Stat(r.cfg.StopMarker)
	return err == nil
}

// disarmOnStop withdraws the will when the daemon is being stopped on
// purpose, or when this relay was dropped because no paired device uses it
// any more while the daemon keeps running. The connection's context has
// already ended, so the write gets its own short deadline.
func (r *runner) disarmOnStop(conn *websocket.Conn) {
	dropped := r.root != nil && r.root.Err() == nil
	if r.cfg.Will == nil || !(dropped || r.stopping()) {
		return
	}
	if r.beforeDisarm != nil {
		r.beforeDisarm()
	}
	ctx, cancel := context.WithTimeout(context.Background(), disarmTimeout)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(DisarmFrame)); err != nil {
		r.cfg.Logf("mailbox: disarm not sent")
		return
	}
	// Drain the preceding frame through the peer's transport before a
	// replacement can open. Pong confirms transport receipt, not storage by
	// the relay's application handler. Ping shares the disarm deadline;
	// the caller forcibly closes the socket on either success or failure.
	if err := conn.Ping(ctx); err != nil {
		r.cfg.Logf("mailbox: disarm transport did not drain")
	}
}

// Session cancellation must not close a socket under its final disarm.
// Each in-flight control write retains its existing independent deadline.
func writeControl(ctx context.Context, conn *websocket.Conn, frame []byte) error {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, frame)
}

// clearStopMarker removes a marker left by an earlier stop, so a later
// shutdown of this process keeps the will armed again.
func clearStopMarker(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}
