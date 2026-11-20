package opencode

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// How full the context window is, as OpenCode's own sidebar counts it
// (v1.19.6 packages/tui/src/feature-plugins/sidebar/context.tsx): the last
// assistant message with output tokens, its input + output + reasoning +
// cache read + cache write (events.sse: message.updated, tokens), over the
// context limit of that message's provider and model. The limits are in
// the provider list (/config/providers: providers[].models[id].limit
// .context), which the mapper reads away from the session's lock
// (harness.Waker). A model the list gives no window shows no percent.

// providersTimeout bounds one read of the provider list.
const providersTimeout = 10 * time.Second

// providersMaxBody bounds the provider list: several connected providers
// with large catalogues pass the default 2 MiB.
const providersMaxBody = 16 << 20

// providersTries reads of the provider list may fail in a row; then it is
// read again no sooner than providersRetry later.
const (
	providersTries = 3
	providersRetry = 5 * time.Minute
)

type tokenCounts struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Cache     struct {
		Read  int64 `json:"read"`
		Write int64 `json:"write"`
	} `json:"cache"`
}

func (t *tokenCounts) sum() int64 {
	if t == nil {
		return 0
	}
	return t.Input + t.Output + t.Reasoning + t.Cache.Read + t.Cache.Write
}

// contextState is the main conversation's latest count and the windows
// read so far. It belongs to the mapper and is guarded by its lock.
type contextState struct {
	used    int64
	model   string // "provider/model" of the counted message
	id      string
	windows map[string]int64 // "provider/model" -> window; 0: none given
	asked   map[string]bool  // models a read of the list was made for
	asking  bool
	fails   int       // failed reads in a row
	retryAt time.Time // after providersTries failures, the next read
	pending bool      // Woken has a percent to publish
	wake    func()
}

// Wake keeps the engine's wake for the provider list reads
// (harness.Waker).
func (e *Events) Wake(f func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ctx.wake = f
}

// Woken publishes the percent a read of the provider list made known.
func (e *Events) Woken() ([]harness.Item, []harness.Op) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.ctx.pending {
		return nil, nil
	}
	e.ctx.pending = false
	return nil, e.contextOps()
}

// resetContext forgets the count of the conversation left.
func (e *Events) resetContext() {
	e.ctx.used, e.ctx.model, e.ctx.id, e.ctx.pending = 0, "", "", false
}

// usage takes the tokens of an assistant message of the main conversation
// and returns the status op once the window is known. The last message
// with output counts, in OpenCode's message order (by id, which grows with
// time): a count is never added to another, so a replay counts nothing
// twice and a compaction lowers it.
func (e *Events) usage(sess, id, provider, model string, t *tokenCounts) []harness.Op {
	if sess != e.main || t == nil || t.Output <= 0 || provider == "" || model == "" {
		return nil
	}
	used := t.sum()
	if id < e.ctx.id {
		return nil
	}
	key := provider + "/" + model
	if used == e.ctx.used && key == e.ctx.model && id == e.ctx.id {
		return nil // the same message again
	}
	prev := e.ctx.model
	e.ctx.used, e.ctx.model, e.ctx.id = used, key, id
	if _, ok := e.ctx.windows[key]; !ok {
		e.askWindows(key)
		if prev != "" && prev != key {
			// Another model, its window not known yet: the last model's
			// percent says nothing about it.
			return []harness.Op{unknownOp()}
		}
		return nil
	}
	return e.contextOps()
}

// contextOps is the status op for the counted message. A model with no
// window clears a percent shown for the model before it.
func (e *Events) contextOps() []harness.Op {
	if e.ctx.used <= 0 {
		return nil
	}
	w, known := e.ctx.windows[e.ctx.model]
	if !known {
		return nil
	}
	if w <= 0 {
		return []harness.Op{unknownOp()}
	}
	pct := min(100, float64(e.ctx.used)*100/float64(w))
	pct = float64(int(pct*10+0.5)) / 10
	return []harness.Op{{Op: harness.OpStatus, Status: &harness.StatusBlock{ContextPercent: &pct}}}
}

func unknownOp() harness.Op {
	unknown := harness.ContextUnknown
	return harness.Op{Op: harness.OpStatus, Status: &harness.StatusBlock{ContextPercent: &unknown}}
}

// askWindows reads the provider list once per model it lacks, off the
// lock; the engine is woken when it is in.
func (e *Events) askWindows(key string) {
	if e.ctx.asking || e.ctx.asked[key] || e.env.Endpoint == "" ||
		e.ctx.fails >= providersTries && time.Now().Before(e.ctx.retryAt) {
		return
	}
	if e.ctx.asked == nil {
		e.ctx.asked = map[string]bool{}
	}
	e.ctx.asked[key], e.ctx.asking = true, true
	env := e.env
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), providersTimeout)
		defer cancel()
		out, err := httpsrc.DoLimit(ctx, env, spec.HTTP{Method: "GET", Path: "/config/providers"}, providersMaxBody)
		var windows map[string]int64
		if err == nil {
			windows, err = parseWindows(out)
		}
		e.mu.Lock()
		e.ctx.asking = false
		if err != nil {
			e.ctx.fails++
			delete(e.ctx.asked, key)
			if e.ctx.fails >= providersTries {
				e.ctx.retryAt = time.Now().Add(providersRetry)
				log.Printf("opencode: the model list could not be read %d times, the context gauge waits %v to try again: %v", e.ctx.fails, providersRetry, err)
			}
		} else {
			e.ctx.fails = 0
			if e.ctx.windows == nil {
				e.ctx.windows = map[string]int64{}
			}
			for k, w := range windows {
				e.ctx.windows[k] = w
			}
			if _, ok := e.ctx.windows[key]; !ok {
				e.ctx.windows[key] = 0 // not in the list: no window known
			}
			e.ctx.pending = e.ctx.model != ""
		}
		wake := e.ctx.wake
		e.mu.Unlock()
		if wake != nil {
			wake()
		}
	}()
}

// parseWindows reads each model's context window from /config/providers.
// Each provider and model is read on its own: one that cannot be read is
// left out and logged, the rest still count. A window given as a fraction
// is cut to a whole number.
func parseWindows(raw json.RawMessage) (map[string]int64, error) {
	var p struct {
		Providers []json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	out := map[string]int64{}
	bad := 0
	for _, rawProvider := range p.Providers {
		var pr struct {
			ID     string                     `json:"id"`
			Models map[string]json.RawMessage `json:"models"`
		}
		if json.Unmarshal(rawProvider, &pr) != nil || pr.ID == "" {
			bad++
			continue
		}
		for key, rawModel := range pr.Models {
			var m struct {
				ID    string `json:"id"`
				Limit *struct {
					Context *float64 `json:"context"`
				} `json:"limit"`
			}
			if json.Unmarshal(rawModel, &m) != nil {
				bad++
				continue
			}
			id := m.ID
			if id == "" {
				id = key
			}
			var w int64
			if m.Limit != nil && m.Limit.Context != nil && *m.Limit.Context > 0 && *m.Limit.Context < 1e12 {
				w = int64(*m.Limit.Context)
			}
			out[pr.ID+"/"+id] = w
		}
	}
	if bad > 0 {
		log.Printf("opencode: the model list had %d entries that could not be read; they were left out", bad)
	}
	return out, nil
}
