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
	// why names the first thing refused, from fixed wording only: never
	// what the message held.
	why := ""
	note := func(reason string) {
		if why == "" {
			why = reason
		}
	}
	for _, err := range errs {
		note(err.Error())
	}
	for _, m := range messages {
		replies, err := r.cache.Apply(m)
		if err != nil {
			var fatal *cacheFailure
			if errors.As(err, &fatal) {
				return err
			}
			r.invalid++
			note(err.Error())
			continue
		}
		if len(replies) > 1 {
			var first struct {
				Reason string `json:"msg"`
			}
			if json.Unmarshal(replies[0].Body, &first) == nil {
				note(fmt.Sprintf("%d operations rejected, first: %s", len(replies), first.Reason))
			}
			replies = []Message{event(map[string]any{"ev": "error", "msg": fmt.Sprintf("%d operations were rejected", len(replies))})}
		}
		for _, reply := range replies {
			var head struct {
				Event  string `json:"ev"`
				Reason string `json:"msg"`
			}
			if reply.Verb == 'e' && json.Unmarshal(reply.Body, &head) == nil && head.Event == "error" {
				r.invalid++
				note("rejected: " + head.Reason)
			}
			if err := r.send(reply); err != nil {
				return err
			}
		}
	}
	if r.invalid > before && before == 0 && r.logf != nil {
		r.logf("surface: ignored invalid protocol input (%s)", why)
	}
	if r.invalid > maxProtocolErrorsPerSecond {
		return errors.New("surface: too many invalid protocol messages")
	}
	return nil
}
