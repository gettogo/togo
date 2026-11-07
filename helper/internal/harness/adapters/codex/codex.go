// Package codex is the Codex harness adapter: its adapter file, the
// codex.appserver mapper (the app-server's thread, turn and item
// notifications, read as a second client of the TUI's server) and the
// codex.hooks mapper (hook payloads, used when the app-server feed is not
// running), and the answer shapes for the app-server's server requests.
package codex

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source/rpc"
)

//go:embed codex.yaml
var adapterFile []byte

// Mapper names.
const (
	MapperAppServer = "codex.appserver"
	MapperHooks     = "codex.hooks"
)

func init() {
	harness.RegisterAdapterFile("codex", adapterFile)
	harness.RegisterMapper(MapperAppServer, func(env harness.Env) harness.Mapper { return NewAppServer(env) })
	harness.RegisterMapper(MapperHooks, func(env harness.Env) harness.Mapper { return NewHooks(env) })
	registerAnswers()
	_ = rpc.Default // the rpc runner registers itself
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }

// live records which sessions have a working app-server feed, so the hooks
// mapper of the same session stays quiet instead of drawing every turn
// twice. Bounded: the oldest entries go first.
var live = struct {
	sync.Mutex
	at map[string]time.Time
}{at: map[string]time.Time{}}

const maxLive = 512

func markLive(session string, at time.Time) {
	if session == "" {
		return
	}
	live.Lock()
	defer live.Unlock()
	if _, ok := live.at[session]; !ok && len(live.at) >= maxLive {
		oldest, when := "", time.Time{}
		for k, v := range live.at {
			if oldest == "" || v.Before(when) {
				oldest, when = k, v
			}
		}
		delete(live.at, oldest)
	}
	live.at[session] = at
}

func unmarkLive(session string) {
	live.Lock()
	defer live.Unlock()
	delete(live.at, session)
}

func isLive(session string) bool {
	live.Lock()
	defer live.Unlock()
	_, ok := live.at[session]
	return ok
}

// msg is a feed record from the rpc runner: a notification, a server
// request, or a call result as {"method", "params": {"result"}}.
type msg struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (m msg) isRequest() bool {
	t := strings.TrimSpace(string(m.ID))
	return t != "" && t != "null"
}

// truncate cuts s to n runes.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// tail keeps the last n bytes of s on a rune boundary.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// clip bounds item text.
func clip(s string) string {
	if len(s) <= harness.MaxItemText {
		return s
	}
	s = s[:harness.MaxItemText]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func ms(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

func secs(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0).UTC()
}

func orNow(t, fallback time.Time) time.Time {
	if t.IsZero() {
		return fallback
	}
	return t
}

func ptr[T any](v T) *T { return &v }
