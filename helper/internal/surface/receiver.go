package surface

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const maxProtocolErrorsPerSecond = 32

// A malformed message must not hide a valid message from the same read. Disk
// failures remain fatal; bounded protocol errors keep the last good document.
type receiver struct {
	parser  Parser
	cache   *Cache
	send    func(Message) error
	logf    func(string, ...any)
	window  time.Time
	invalid int
}

func (r *receiver) feed(data []byte) error {
	now := time.Now()
	if now.Sub(r.window) >= time.Second {
		r.window = now
		r.invalid = 0
	}
	before := r.invalid
	messages, errs := r.parser.Feed(data, now)
	r.invalid += len(errs)
	for _, m := range messages {
		replies, err := r.cache.Apply(m)
		if err != nil {
			var fatal *cacheFailure
			if errors.As(err, &fatal) {
				return err
			}
			r.invalid++
			continue
		}
		if len(replies) > 1 {
			replies = []Message{event(map[string]any{"ev": "error", "msg": fmt.Sprintf("%d operations were rejected", len(replies))})}
		}
		for _, reply := range replies {
			var head struct {
				Event string `json:"ev"`
			}
			if reply.Verb == 'e' && json.Unmarshal(reply.Body, &head) == nil && head.Event == "error" {
				r.invalid++
			}
			if err := r.send(reply); err != nil {
				return err
			}
		}
	}
	if r.invalid > before && before == 0 && r.logf != nil {
		r.logf("surface: ignored invalid protocol input")
	}
	if r.invalid > maxProtocolErrorsPerSecond {
		return errors.New("surface: too many invalid protocol messages")
	}
	return nil
}
