package httpsrc

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestScrubKeysAndSetList(t *testing.T) {
	in := `{"key":"abc","apiKey":"","options":{"apiKey":"plain-value","baseURL":"http://x"},
		"headers":{"X-Custom":"v1"},"tokens":{"input":5},"inputTokens":7,"apiKeyRequired":true,
		"password":["a"],"name":"ok"}`
	var got map[string]any
	if err := json.Unmarshal(Scrub(json.RawMessage(in)), &got); err != nil {
		t.Fatal(err)
	}
	if got["key"] != nil || got["apiKey"] != nil || got["password"] != nil {
		t.Fatalf("secret values kept: %v", got)
	}
	set := got[SetList].([]any)
	if len(set) != 3 || set[0] != "headers" || set[1] != "key" || set[2] != "password" {
		t.Fatalf("_set = %v", set) // apiKey was empty, so it is not listed
	}
	opts := got["options"].(map[string]any)
	if opts["apiKey"] != nil || opts["baseURL"] != "http://x" || opts[SetList].([]any)[0] != "apiKey" {
		t.Fatalf("options = %v", opts)
	}
	if got["headers"].(map[string]any)["X-Custom"] != nil {
		t.Fatal("header value kept")
	}
	if got["tokens"].(map[string]any)["input"] != json.Number("5") && got["tokens"].(map[string]any)["input"] != float64(5) {
		t.Fatalf("token counts dropped: %v", got["tokens"])
	}
	if got["apiKeyRequired"] != true || got["name"] != "ok" {
		t.Fatalf("flags or names dropped: %v", got)
	}
}

func TestScrubStringPatterns(t *testing.T) {
	for _, s := range []string{
		"sk-abcdefghijklmnop1234", "sk-ant-api03-xyz", "ghp_abcdefghijklmnop1234", "gho_abcdefghijklmnop1234",
		"github_pat_abcdefghijklmnop_1234", "xoxb-1234567890-abc", "AKIAABCDEFGHIJKLMNOP",
		"AIza" + strings.Repeat("a", 35), "eyJhbGciOi.eyJzdWIiOi.sig",
		"Bearer abcdefghijklmnop", "-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----",
	} {
		out := ScrubString("before " + s + " after")
		if strings.Contains(out, s) || !strings.Contains(out, Marker) {
			t.Errorf("%q not redacted: %q", s, out)
		}
	}
	if ScrubString("plain text 123") != "plain text 123" {
		t.Error("plain text changed")
	}
}

func TestScrubInvalidJSONIsNull(t *testing.T) {
	if string(Scrub(json.RawMessage(`{"a":`))) != "null" {
		t.Fatal("broken JSON must become null")
	}
}

// The recorded OpenCode providers answer (fake keys) loses every key, and
// the providers that had one say so in _set.
func TestScrubOpenCodeProviders(t *testing.T) {
	raw, err := os.ReadFile("testdata/providers.json")
	if err != nil {
		t.Fatal(err)
	}
	out := string(Scrub(raw))
	for _, leak := range []string{"sk-togofake", "sk-test", "moonshot-secret-value", "local-secret"} {
		if strings.Contains(out, leak) {
			t.Fatalf("leaked %q", leak)
		}
	}
	var got struct {
		Providers []struct {
			ID      string         `json:"id"`
			Key     *string        `json:"key"`
			Set     []string       `json:"_set"`
			Options map[string]any `json:"options"`
		} `json:"providers"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range got.Providers {
		seen[p.ID] = true
		if p.Key != nil {
			t.Errorf("%s: key kept", p.ID)
		}
		switch p.ID {
		case "moonshotai":
			if len(p.Set) != 1 || p.Set[0] != "key" {
				t.Errorf("moonshotai _set = %v", p.Set)
			}
		case "togofake", "local":
			if p.Options["apiKey"] != nil || p.Options[SetList] == nil {
				t.Errorf("%s options = %v", p.ID, p.Options)
			}
		}
	}
	if !seen["moonshotai"] || !seen["togofake"] || !seen["local"] {
		t.Fatalf("providers missing: %v", seen)
	}
}

func TestScrubKeepsTokenCountsOnly(t *testing.T) {
	got := string(Scrub([]byte(`{"tokens":{"input":3,"cache":{"read":1}},"tokenUsage":{"total":{"inputTokens":5}},"access_tokens":["opaque"],"key":"plain"}`)))
	for _, want := range []string{`"tokens":{"cache":{"read":1},"input":3}`, `"tokenUsage":{"total":{"inputTokens":5}}`, `"access_tokens":null`, `"key":null`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, "opaque") || strings.Contains(got, "plain") {
		t.Errorf("secret survived: %s", got)
	}
}
