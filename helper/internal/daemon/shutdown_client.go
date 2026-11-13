package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
	"github.com/gettogo/togo/helper/internal/wire"
)

// ShutdownWatch keeps the shutdown connection to the original daemon. A
// replacement may already hold the same lock before a caller can poll it.
type ShutdownWatch struct {
	Result wire.ShutdownResult
	conn   net.Conn
}

// BeginShutdown sends local.shutdown and retains its acknowledged connection.
// The caller must Close the watch, including when it elects not to wait.
func BeginShutdown(ctx context.Context, socket string, p wire.ShutdownParams) (*ShutdownWatch, error) {
	c, err := sockd.Dial(ctx, socket)
	if err != nil {
		return nil, err
	}
	result, err := shutdownReply(c, p)
	if err != nil {
		c.Close()
		return nil, err
	}
	return &ShutdownWatch{Result: result, conn: c}, nil
}

// Close releases the connection when the caller is done waiting.
func (w *ShutdownWatch) Close() error { return w.conn.Close() }

// Wait returns when the original server closes its connection. Run closes its
// listener before closing clients, so a later successful dial reaches a replacement.
// Notifications on this connection do not constitute shutdown completion.
func (w *ShutdownWatch) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := w.conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = w.conn.SetReadDeadline(time.Now()) })
	defer stop()
	_, err := io.Copy(io.Discard, w.conn)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func shutdownReply(c net.Conn, p wire.ShutdownParams) (wire.ShutdownResult, error) {
	if err := c.SetDeadline(time.Now().Add(probeTimeout)); err != nil {
		return wire.ShutdownResult{}, err
	}
	params, err := json.Marshal(p)
	if err != nil {
		return wire.ShutdownResult{}, err
	}
	call := `{"jsonrpc":"2.0","id":2,"method":"` + wire.MethodLocalShutdown + `","params":` + string(params) + `}`
	for _, frame := range []string{probeHello, call} {
		if err := rpc.WriteFrame(c, []byte(frame)); err != nil {
			return wire.ShutdownResult{}, fmt.Errorf("shutdown request: %w", err)
		}
	}
	for {
		payload, err := rpc.ReadFrame(c)
		if err != nil {
			return wire.ShutdownResult{}, fmt.Errorf("shutdown request: %w", err)
		}
		var reply struct {
			ID     int                  `json:"id"`
			Result *wire.ShutdownResult `json:"result"`
			Error  *rpc.Error           `json:"error"`
		}
		if err := json.Unmarshal(payload, &reply); err != nil {
			return wire.ShutdownResult{}, fmt.Errorf("shutdown request: %w", err)
		}
		if reply.ID != 2 {
			continue
		}
		if reply.Error != nil {
			return wire.ShutdownResult{}, fmt.Errorf("shutdown request: %s", reply.Error.Message)
		}
		if reply.Result == nil {
			return wire.ShutdownResult{}, errors.New("shutdown request: empty reply")
		}
		return *reply.Result, nil
	}
}
