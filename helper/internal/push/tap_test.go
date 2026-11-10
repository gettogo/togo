package push

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestTapRecordsHashesAndRelayRefusal(t *testing.T) {
	e := newEnv(t, 1)
	path := filepath.Join(t.TempDir(), "tap.jsonl")
	e.sender.Tap = FileTap(path)
	e.sender.Sleep = func(context.Context, time.Duration) error { return nil }
	e.relay.status.Store(http.StatusBadGateway)
	e.relay.answer.Store(`{"error":"apns","reason":"InternalServerError"}`)
	e.handle(approvalOpen("r_abcdefghij"))

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != maxAttempts {
		t.Fatalf("tap has %d lines, want one per attempt (%d)", len(lines), maxAttempts)
	}
	var r TapRecord
	if err := json.Unmarshal([]byte(lines[0]), &r); err != nil {
		t.Fatal(err)
	}
	if r.Path != "/v1/push" || r.Kind != relayAlert || r.Status != http.StatusBadGateway || r.Reason != "InternalServerError" {
		t.Fatalf("tap record %+v", r)
	}
	if len(r.Sha) != 6 || r.Len == 0 || !slices.Contains(r.Words, WordHash("ciphertext")) {
		t.Fatalf("tap record lacks body hashes: %+v", r)
	}
	for _, word := range []string{"npm", "test", "Run"} {
		if slices.Contains(r.Words, WordHash(word)) {
			t.Fatalf("relay body carried %q in the clear", word)
		}
	}
	if strings.Contains(string(raw), "ciphertext") || strings.Contains(string(raw), "npm") {
		t.Fatalf("tap holds text, not hashes: %s", raw)
	}
}

func TestNoTapWithoutPath(t *testing.T) {
	if FileTap("") != nil {
		t.Fatal("an empty path gave a tap")
	}
}
