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

// How full the context window is. An assistant message, once a step ends,
// carries the step's tokens (events.sse: message.updated with tokens
// {total, input, output, reasoning, cache {read, write}}); their sum is
// what the conversation holds after the step, as OpenCode's own sidebar
// counts it. The model's window is in the provider list
// (/config/providers: providers[].models[id].limit.context), which the
// mapper reads away from the session's lock (harness.Waker). A model the
// list gives no window shows no percent.

// providersTimeout bounds one read of the provider list.
const providersTimeout = 10 * time.Second

// providersTries bounds the reads of the provider list that fail.
const providersTries = 3

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
	created int64
	id      string
	windows map[string]int64 // "provider/model" -> window; 0: none given
	asked   map[string]bool  // models a read of the list was made for
	asking  bool
	fails   int
	pending bool // Woken has a percent to publish
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
	e.ctx.used, e.ctx.model, e.ctx.created, e.ctx.id, e.ctx.pending = 0, "", 0, "", false
}

// usage takes the tokens of an assistant message of the main conversation
// and returns the status op when the percent is known. The latest message
// counts, by creation time: a count is never added to another, so a replay
// counts nothing twice and a compaction lowers it.
func (e *Events) usage(sess, id string, created int64, provider, model string, t *tokenCounts) []harness.Op {
	used := t.sum()
	if sess != e.main || used <= 0 || provider == "" || model == "" {
		return nil
	}
	if created < e.ctx.created || created == e.ctx.created && id < e.ctx.id {
		return nil
	}
	key := provider + "/" + model
	if used == e.ctx.used && key == e.ctx.model && id == e.ctx.id {
		return nil // the same message again
	}
	e.ctx.used, e.ctx.model, e.ctx.created, e.ctx.id = used, key, created, id
	if _, ok := e.ctx.windows[key]; !ok {
		e.askWindows(key)
		return nil
	}
	return e.contextOps()
}

func (e *Events) contextOps() []harness.Op {
	w := e.ctx.windows[e.ctx.model]
	if w <= 0 || e.ctx.used <= 0 {
		return nil
	}
	pct := min(100, float64(e.ctx.used)*100/float64(w))
	pct = float64(int(pct*10+0.5)) / 10
	return []harness.Op{{Op: harness.OpStatus, Status: &harness.StatusBlock{ContextPercent: &pct}}}
}

// askWindows reads the provider list once per model it lacks, off the
// lock; the engine is woken when it is in.
func (e *Events) askWindows(key string) {
	if e.ctx.asking || e.ctx.asked[key] || e.ctx.fails >= providersTries || e.env.Endpoint == "" {
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
		out, err := httpsrc.Do(ctx, env, spec.HTTP{Method: "GET", Path: "/config/providers"})
		var windows map[string]int64
		if err == nil {
			windows, err = parseWindows(out)
		}
		e.mu.Lock()
		e.ctx.asking = false
		if err != nil {
			e.ctx.fails++
			delete(e.ctx.asked, key)
			if e.ctx.fails == providersTries {
				log.Printf("opencode: the model list could not be read %d times, the context gauge stays empty: %v", providersTries, err)
			}
		} else {
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
func parseWindows(raw json.RawMessage) (map[string]int64, error) {
	var p struct {
		Providers []struct {
			ID     string `json:"id"`
			Models map[string]struct {
				ID    string `json:"id"`
				Limit struct {
					Context int64 `json:"context"`
				} `json:"limit"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, pr := range p.Providers {
		for key, m := range pr.Models {
			id := m.ID
			if id == "" {
				id = key
			}
			out[pr.ID+"/"+id] = max(m.Limit.Context, 0)
		}
	}
	return out, nil
}
