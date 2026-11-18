package redact

import (
	"encoding/json"
	"sync"
	"testing"
)

// A decoded value can be shared: the engine keeps a source's last answer
// and hands the same value to every request that reads it. Two requests at
// once (the app asking for command completions on two screens, or again
// after a reconnect) must be able to prepare it for gojq together. Writing
// into the shared maps, even the same values, is a fatal error in Go that
// takes the whole helper down.
func TestNormalizeLeavesItsInputAlone(t *testing.T) {
	shared := map[string]any{
		"plain":  map[string]any{"a": "x", "list": []any{"y", map[string]any{"z": true}}},
		"number": map[string]any{"n": json.Number("3"), "list": []any{json.Number("2.5"), "s"}},
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				Normalize(shared)
			}
		}()
	}
	wg.Wait()
	if _, ok := shared["number"].(map[string]any)["n"].(json.Number); !ok {
		t.Fatal("the input was changed")
	}
	got := Normalize(shared).(map[string]any)["number"].(map[string]any)
	if got["n"] != 3 || got["list"].([]any)[0] != 2.5 || got["list"].([]any)[1] != "s" {
		t.Fatalf("normalized = %#v", got)
	}
	// A value with nothing to change comes back as it is, not copied.
	plain := shared["plain"].(map[string]any)
	if out := Normalize(plain).(map[string]any); len(out) != 2 || out["a"] != "x" {
		t.Fatalf("plain = %#v", out)
	}
}
