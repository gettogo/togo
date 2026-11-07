package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/redact"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Source timeouts.
const (
	defaultTimeout = 5 * time.Second
	maxTimeout     = 15 * time.Second
	// staleFactor is how many cache periods a last good copy is served
	// after a failure.
	staleFactor = 10
)

// cache keeps source results per session, source and rendered block.
type cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	session string
	at      time.Time
	data    any
}

func (c *cache) get(key string) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	return e, ok
}

func (c *cache) put(key string, e cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]cacheEntry{}
	}
	c.entries[key] = e
}

func (c *cache) forget(session string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if e.session == session {
			delete(c.entries, k)
		}
	}
}

// fetch reads a source (or a feed's latest record) for a session, applies
// redaction and the source's select, and caches the result. stale is true
// when a failure was answered with the last good copy.
func (e *Engine) fetch(ctx context.Context, s *sess, name string, query map[string]string, fresh bool) (data any, stale bool, err error) {
	if _, isFeed := s.ad.Feeds[name]; isFeed {
		s.mu.Lock()
		raw, ok := s.last[name]
		s.mu.Unlock()
		if !ok {
			return nil, false, fmt.Errorf("feed %s has no data yet", name)
		}
		v, err := decode(raw)
		return v, false, err
	}
	src, ok := s.ad.Sources[name]
	if !ok {
		return nil, false, fmt.Errorf("unknown source %s", name)
	}
	args := map[string]any{}
	for k, v := range query {
		if reservedVars[k] {
			return nil, false, invalidArgs("a query key names a value only the helper sets")
		}
		args[k] = v
	}
	s.mu.Lock()
	data_ := s.templateDataLocked(args)
	state := s.stateLocked()
	s.mu.Unlock()
	raw, err := rawOf(src.Kinds)
	if err != nil {
		return nil, false, err
	}
	if notYet(raw, state) {
		// A session-scoped source before the harness named its session
		// (OpenCode's /session//todo before the first prompt): empty,
		// not a failure.
		return nil, false, nil
	}
	rendered, err := RenderJSON(raw, data_)
	if err != nil {
		return nil, false, err
	}
	key := s.id + "\x00" + name + "\x00" + string(rendered)
	if c, ok := e.cache.get(key); ok && !fresh && src.Cache > 0 && e.cfg.Now().Sub(c.at) < src.Cache {
		return c.data, false, nil
	}
	v, err := e.read(ctx, s, name, src, rendered, state)
	if err != nil {
		e.cfg.Logf("harness: %s source %s (%s): %v", s.ad.Adapter.Adapter, name, src.Stability, err)
		if c, ok := e.cache.get(key); ok && src.Cache > 0 && e.cfg.Now().Sub(c.at) < staleFactor*src.Cache {
			return c.data, true, nil
		}
		if src.Fallback != nil {
			fb := *src.Fallback
			raw, rerr := rawOf(fb.Kinds)
			if rerr == nil {
				if fr, rerr := RenderJSON(raw, data_); rerr == nil {
					if v, ferr := e.read(ctx, s, name, fb, fr, state); ferr == nil {
						return v, false, nil
					}
				}
			}
		}
		return nil, false, err
	}
	e.cache.put(key, cacheEntry{session: s.id, at: e.cfg.Now(), data: v})
	return v, false, nil
}

// notYet reports whether a source's templates need the harness's session
// id and it is not known yet.
func notYet(raw json.RawMessage, state map[string]string) bool {
	return state["harness_session"] == "" && bytes.Contains(raw, []byte("harness_session"))
}

// read runs one source block and post-processes its result.
func (e *Engine) read(ctx context.Context, s *sess, name string, src spec.Source, rendered json.RawMessage, state map[string]string) (any, error) {
	kind := kindOf(src.Kinds)
	timeout := src.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	timeout = min(timeout, maxTimeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r, ok := harness.LookupRunner(kind)
	if !ok {
		return nil, fmt.Errorf("no runner for %s", kind)
	}
	out, err := r.Fetch(ctx, harness.SourceSpec{Name: name, Kind: kind, Raw: rendered}, s.env)
	if err != nil {
		return nil, err
	}
	v, err := decode(out)
	if err != nil {
		return nil, errors.New("source result is not JSON")
	}
	if len(src.Redact) > 0 {
		if v, err = redact.Paths(v, src.Redact); err != nil {
			return nil, err
		}
	}
	v = redact.Value(v)
	if src.Select != "" {
		res, err := JQ(src.Select, v, state)
		if err != nil {
			return nil, err
		}
		switch len(res) {
		case 0:
			v = nil
		case 1:
			v = res[0]
		default:
			v = res
		}
	}
	return v, nil
}
