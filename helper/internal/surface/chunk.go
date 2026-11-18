package surface

import (
	"errors"
	"time"
)

var errChunkInterrupted = errors.New("surface: chunk sequence interrupted")
var errChunkExpired = errors.New("surface: chunk sequence timed out")

type chunkSequence struct {
	id       string
	verb     byte
	params   map[string]string
	body     []byte
	updated  time.Time
	skipping bool
}

func (c *chunkSequence) expire(now time.Time) error {
	if c.id == "" || now.Sub(c.updated) < ChunkTimeout {
		return nil
	}
	skipping := c.skipping
	*c = chunkSequence{}
	if skipping {
		return nil
	}
	return errChunkExpired
}

func (c *chunkSequence) push(m Message, now time.Time) (*Message, error) {
	id, chunked := m.Params["c"]
	more := m.Params["m"] == "1"
	var interrupted error
	if c.id != "" && (id != c.id || m.Verb != c.verb) {
		if !c.skipping {
			interrupted = errChunkInterrupted
		}
		*c = chunkSequence{}
	}
	if !chunked {
		return &m, interrupted
	}
	if id == "" || len(id) > 128 || (m.Params["m"] != "" && !more) {
		return nil, ErrMalformed
	}
	if c.id == "" {
		*c = chunkSequence{id: id, verb: m.Verb, params: m.Params}
	}
	c.updated = now
	if c.skipping {
		if !more {
			*c = chunkSequence{}
		}
		return nil, interrupted
	}
	if len(c.body)+len(m.Body) > MaxMessageBytes {
		c.body = nil
		c.skipping = true
		if !more {
			*c = chunkSequence{}
		}
		return nil, ErrLimit
	}
	c.body = append(c.body, m.Body...)
	if more {
		return nil, interrupted
	}
	result := &Message{Verb: c.verb, Params: c.params, Body: c.body}
	delete(result.Params, "c")
	delete(result.Params, "m")
	*c = chunkSequence{}
	return result, interrupted
}
