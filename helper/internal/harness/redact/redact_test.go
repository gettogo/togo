package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStringPatterns(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"openai", "key sk-abcdefghijklmnop1234 end", "key [redacted] end"},
		{"anthropic", "sk-ant-api03-AAAA_bbbb-cc", "[redacted]"},
		{"github classic", "ghp_abcdefghijklmnopqrstuvwxyz0123", "[redacted]"},
		{"github oauth", "gho_abcdefghijklmnopqrstuv", "[redacted]"},
		{"github fine", "github_pat_11ABCDEFG0123456789_abcdef", "[redacted]"},
		{"slack", "xoxb-1234-5678-abcdefgh", "[redacted]"},
		{"aws", "id AKIAABCDEFGHIJKLMNOP.", "id [redacted]."},
		{"google", "AIza" + strings.Repeat("x", 35), "[redacted]"},
		{"jwt", "token eyJhbGciOi.eyJzdWIiOi.sig-nature", "token [redacted]"},
		{"pem", "a\n-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----\nb", "a\n[redacted]\nb"},
		{"pem open", "-----BEGIN PRIVATE KEY-----\nMIIE...", "[redacted]"},
		{"bearer", "Authorization: Bearer abcdefghijklmnop.qrs", "Authorization: Bearer [redacted]"},
		{"env", "OPENAI_API_KEY=abc123 PATH=/bin", "OPENAI_API_KEY=[redacted] PATH=/bin"},
		{"env quoted", `GH_TOKEN="x y"`, "GH_TOKEN=[redacted]"},
		{"plain", "go test ./... passed", "go test ./... passed"},
		{"short sk", "sk-short", "sk-short"},
	}
	for _, c := range cases {
		if got := String(c.in); got != c.want {
			t.Errorf("%s: String(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestJSONKeysAndNesting(t *testing.T) {
	in := `{"model":"opus","api_key":"plain","nested":{"Authorization":"x","list":[{"password":"p","ok":"sk-abcdefghijklmnop12"}]},
	"usage":{"input_tokens":12,"total_output_tokens":4},"token":7,"cookie":{"a":1},"session_id":"abc","privateKey":"k",
	"tokenUsage":{"total":{"inputTokens":5}},"tokens":{"input":3},"key":"sk-x","keys":["model.name"]}`
	got := string(JSON(json.RawMessage(in)))
	for _, want := range []string{`"api_key":null`, `"Authorization":null`, `"password":null`, `"ok":"[redacted]"`,
		`"input_tokens":12`, `"total_output_tokens":4`, `"token":null`, `"cookie":null`, `"session_id":"abc"`,
		`"privateKey":null`, `"model":"opus"`, `"tokenUsage":{"total":{"inputTokens":5}}`, `"tokens":{"input":3}`,
		`"key":null`, `"keys":["model.name"]`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if string(JSON(json.RawMessage("{bad"))) != "null" {
		t.Error("malformed JSON must become null")
	}
}

func TestOpenCodeProvidersPayload(t *testing.T) {
	in := `{"providers":[{"id":"anthropic","key":"sk-test-123","options":{"apiKey":"sk-ant-zzz","headers":{"X":"y"},"baseURL":"https://api"}}],"default":{"anthropic":"claude-opus"}}`
	v, err := Decode([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	v, err = Paths(v, []string{".providers[].key", ".providers[].options.apiKey", ".providers[].options.headers", ".missing[].x"})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := Encode(Value(v))
	s := string(out)
	if strings.Contains(s, "sk-") || strings.Contains(s, "headers") || strings.Contains(s, `"key"`) {
		t.Errorf("secrets survived: %s", s)
	}
	if !strings.Contains(s, "baseURL") || !strings.Contains(s, "claude-opus") {
		t.Errorf("non-secrets lost: %s", s)
	}
	if _, err := Paths(v, []string{".[[["}); err == nil {
		t.Error("bad path compiled")
	}
}

func TestToolOutputTail(t *testing.T) {
	out := "$ env\nHOME=/Users/x\nANTHROPIC_API_KEY=sk-ant-api03-secretsecret\nGITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz\n"
	got := String(out)
	if strings.Contains(got, "secretsecret") || strings.Contains(got, "ghp_") {
		t.Errorf("tool output kept a secret: %q", got)
	}
	if !strings.Contains(got, "HOME=/Users/x") {
		t.Errorf("tool output lost text: %q", got)
	}
}

// Token-count objects survive for the context meters (Codex tokenUsage,
// OpenCode tokens); token-named keys holding strings or lists of strings
// do not, nor does a bare key field.
func TestTokenCountsKeptSecretsNot(t *testing.T) {
	in := `{
	"tokenUsage":{"total":{"inputTokens":5,"cachedInputTokens":1,"outputTokens":2},"last":{"inputTokens":1},"modelContextWindow":272000},
	"tokens":{"input":3,"output":4,"reasoning":0,"cache":{"read":1,"write":null}},
	"input_tokens":12,"max_tokens":null,"tokens_used":[1,2],
	"access_tokens":["opaque-session-value"],"refresh_tokens":"opaque","auth_tokens":{"github":"opaque"},
	"providers":[{"id":"anthropic","key":"plain-provider-key","models":{"m":{"limit":{"context":200000}}}}],
	"key":{"nested":"x"}}`
	got := string(JSON(json.RawMessage(in)))
	for _, want := range []string{
		`"tokenUsage":{"last":{"inputTokens":1},"modelContextWindow":272000,"total":{"cachedInputTokens":1,"inputTokens":5,"outputTokens":2}}`,
		`"tokens":{"cache":{"read":1,"write":null},"input":3,"output":4,"reasoning":0}`,
		`"input_tokens":12`, `"tokens_used":[1,2]`, `"max_tokens":null`,
		`"access_tokens":null`, `"refresh_tokens":null`, `"auth_tokens":null`,
		`"key":null`, `"context":200000`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, "opaque") || strings.Contains(got, "plain-provider-key") {
		t.Errorf("secret survived: %s", got)
	}
}

func TestCounts(t *testing.T) {
	for _, c := range []struct {
		v    any
		want bool
	}{
		{json.Number("3"), true}, {nil, true}, {true, true}, {"3", false},
		{map[string]any{"a": json.Number("1"), "b": []any{json.Number("2")}}, true},
		{map[string]any{"a": "x"}, false}, {[]any{json.Number("1"), "x"}, false},
	} {
		if got := Counts(c.v); got != c.want {
			t.Errorf("Counts(%#v) = %v", c.v, got)
		}
	}
}

func TestHeadersOAuthAndLowercaseEnv(t *testing.T) {
	in := `{"mcpServers":{"x":{"headers":{"X-Api":"plainsecret","Accept":"json"},"url":"https://x"}},
	"oauth":{"client":"plainsecret"},"refresh_token":"plainsecret","refreshSeconds":30,"apiKeyRequired":true,
	"out":"ghs_abcdefghijklmnopqrstu and my_api_key=plainsecret"}`
	got := string(JSON(json.RawMessage(in)))
	if strings.Contains(got, "plainsecret") || strings.Contains(got, "ghs_") {
		t.Errorf("secret survived: %s", got)
	}
	for _, want := range []string{`"X-Api":null`, `"Accept":null`, `"url":"https://x"`, `"refreshSeconds":30`, `"apiKeyRequired":true`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}
