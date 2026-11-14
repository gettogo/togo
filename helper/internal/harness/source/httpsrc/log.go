package httpsrc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Bounds of one history read: the newest MaxLogEvents events are kept, and
// a log larger than MaxLogBytes is not read at all.
const (
	MaxLogEvents = 20000
	MaxLogBytes  = 32 << 20
)

// ErrLogTooLarge means the history is larger than MaxLogBytes.
var ErrLogTooLarge = errors.New("httpsrc: history is too large to read")

// ReadLog reads an SSE endpoint that sends past events and ends (or sends
// an event whose type is end). It returns the events' data in order,
// scrubbed as a feed's are, the newest MaxLogEvents of them.
func ReadLog(ctx context.Context, env harness.Env, path, end string) ([]json.RawMessage, error) {
	base, err := Base(env)
	if err != nil {
		return nil, err
	}
	u, err := Join(base, path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if env.Auth != "" {
		req.Header.Set("Authorization", env.Auth)
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Code: resp.StatusCode}
	}
	p := NewParser(resp.Body)
	var out []json.RawMessage
	total := 0
	for {
		ev, err := p.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		data := eventData(ev)
		if end != "" {
			var head struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(data, &head) == nil && head.Type == end {
				return out, nil
			}
		}
		if total += len(data); total > MaxLogBytes {
			return nil, ErrLogTooLarge
		}
		out = append(out, data)
		if len(out) > 2*MaxLogEvents {
			out = append(out[:0:0], out[len(out)-MaxLogEvents:]...)
		}
		if len(out) > MaxLogEvents && ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
}

// Newest trims events to the newest max.
func Newest(events []json.RawMessage, max int) []json.RawMessage {
	if len(events) > max {
		return events[len(events)-max:]
	}
	return events
}
