// Package opencode is the OpenCode harness adapter: its adapter file and
// the opencode.events mapper, which turns the TUI server's /event stream
// into conversation items and mission ops.
package opencode

import (
	_ "embed"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
)

//go:embed opencode.yaml
var adapterFile []byte

//go:embed opencode2.yaml
var adapterFile2 []byte

// MapperEvents is the mapper name the adapter file uses.
const MapperEvents = "opencode.events"

func init() {
	harness.RegisterAdapterFile("opencode", adapterFile)
	harness.RegisterMapper(MapperEvents, func(env harness.Env) harness.Mapper { return NewEvents(env) })
	// OpenCode 2.x: the engine picks this file or the 1.x one by version.
	harness.RegisterAdapterFile("opencode2", adapterFile2)
	harness.RegisterMapper(MapperEvents2, func(env harness.Env) harness.Mapper { return NewEvents2(env) })
	_ = httpsrc.HTTP{} // the http and sse runners register themselves
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }

// AdapterFile2 returns the embedded OpenCode 2.x adapter file.
func AdapterFile2() []byte { return adapterFile2 }

// partialEvery bounds streamed updates of one item.
const partialEvery = 250 * time.Millisecond

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

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

func orAt(t, at time.Time) time.Time {
	if t.IsZero() {
		return at
	}
	return t
}

func ptr[T any](v T) *T { return &v }

// rel makes p relative to root when it lies under it.
func rel(root, p string) string {
	if root == "" || !filepath.IsAbs(p) {
		return p
	}
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}

// modelRef is the id the model picker uses for a model: the session model
// switch's ModelRef as compact JSON with sorted keys, the form gojq's
// tojson writes.
func modelRef(provider, model string) string {
	b, _ := json.Marshal(map[string]string{"id": model, "providerID": provider})
	return string(b)
}
