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

func TestStringTextShapes(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"quoted json", `"apiKey": "plainsecret1"`, `"apiKey": "[redacted]"`},
		{"quoted json single", `{'api_key': 'plainsecret1'}`, `{'api_key': '[redacted]'}`},
		{"yaml", "api_key: plainsecret1\nname: x", "api_key: [redacted]\nname: x"},
		{"yaml indented", "  password: plainsecret1", "  password: [redacted]"},
		{"flag space", "tool --api-key plainsecret1 --v", "tool --api-key [redacted] --v"},
		{"flag eq", "tool --token=plainsecret1", "tool --token=[redacted]"},
		{"flag password", "psql --password plainsecret1", "psql --password [redacted]"},
		{"env _KEY", "OPENAI_KEY=plainsecret1", "OPENAI_KEY=[redacted]"},
		{"env MISTRAL", "export MISTRAL_KEY=plainsecret1", "export MISTRAL_KEY=[redacted]"},
		{"env AUTH", "ANTHROPIC_AUTH_TOKEN=plainsecret1", "ANTHROPIC_AUTH_TOKEN=[redacted]"},
		{"env COOKIE", "SESSION_COOKIE=plainsecret1", "SESSION_COOKIE=[redacted]"},
		{"env CREDENTIAL", "GOOGLE_CREDENTIALS=plainsecret1", "GOOGLE_CREDENTIALS=[redacted]"},
		{"x-api-key", "x-api-key: plainsecret1", "x-api-key: [redacted]"},
		{"curl header", `curl -H "x-api-key: plainsecret1" u`, `curl -H "x-api-key: [redacted]" u`},
		{"cookie", "Cookie: session=plainsecret1; other=2", "Cookie: [redacted]"},
		{"set-cookie", "Set-Cookie: sid=plainsecret1; Path=/", "Set-Cookie: [redacted]"},
		{"auth token", "Authorization: token plainsecret1", "Authorization: [redacted]"},
		{"auth short bearer", "Authorization: Bearer abc", "Authorization: [redacted]"},
		{"oauth code", "http://localhost:1455/cb?code=plainsecret1&state=s", "http://localhost:1455/cb?code=[redacted]&state=s"},
		{"access_token", "#access_token=plainsecret1&x=1", "#access_token=[redacted]&x=1"},
		{"refresh_token", "?refresh_token=plainsecret1", "?refresh_token=[redacted]"},
		{"client_secret", "&client_secret=plainsecret1", "&client_secret=[redacted]"},
		{"url creds", "postgres://user:plainsecret1@db:5432/x", "postgres://user:[redacted]@db:5432/x"},
		{"google oauth", "ya29.a0AfH6SMplainsecret1", "[redacted]"},
		{"plain url", "https://github.com/a/b?tab=code", "https://github.com/a/b?tab=code"},
		{"plain email", "mail me at a@b.com", "mail me at a@b.com"},
		{"plain yaml", "name: build\nkey_count: 3", "name: build\nkey_count: 3"},
		{"usage tokens", "input_tokens: 1200", "input_tokens: 1200"},
		{"unicode code word", "the code=42 path", "the code=42 path"},
	}
	for _, c := range cases {
		if got := String(c.in); got != c.want {
			t.Errorf("%s: String(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// A form's fields are named by "key", and a condition names the field it
// depends on the same way. Those names survive; every other "key" is
// still taken for a provider key.
func TestFormFieldNamesKept(t *testing.T) {
	in := `{"type":"form.created","data":{"key":"sk-plain","form":{"id":"frm_1","sessionID":"ses_1","title":"t","key":"top",
	"metadata":{"key":"meta"},"fields":[
	{"key":"notify","type":"boolean"},
	{"key":"channel","type":"string","when":[{"key":"notify","op":"eq","value":true},{"key":"loose"}],"options":[{"value":"a","label":"A","key":"opt"}]},
	{"key":"sk-abcdefghijklmnop1234","type":"string"},
	{"key":"has space","type":"string"},
	{"key":"untyped"},
	{"key":7,"type":"string"}]}}}`
	got := string(JSON(json.RawMessage(in)))
	for _, want := range []string{`{"key":"notify","type":"boolean"}`, `"key":"channel"`, `{"key":"notify","op":"eq","value":true}`,
		`{"key":"[redacted]","type":"string"}`, `{"key":7,"type":"string"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	for _, gone := range []string{"sk-plain", "top", "meta", "loose", "opt", "sk-abcdefghijklmnop1234", "has space", "untyped"} {
		if strings.Contains(got, `"`+gone+`"`) {
			t.Errorf("%q kept in %s", gone, got)
		}
	}
}

// Only a form keeps them: the same fields under anything else do not.
func TestFieldNamesOutsideAFormStillStripped(t *testing.T) {
	for _, in := range []string{
		`{"id":"prv_1","fields":[{"key":"plainkey","type":"string"}]}`,
		`{"fields":[{"key":"plainkey","type":"string"}]}`,
		`{"id":"frm_1","fields":{"key":"plainkey","type":"string"}}`,
		`{"id":"frm_1","providers":[{"key":"plainkey","type":"string"}]}`,
		`{"id":7,"fields":[{"key":"plainkey","type":"string"}]}`,
		`[{"key":"plainkey","type":"string"}]`,
	} {
		if got := string(JSON(json.RawMessage(in))); strings.Contains(got, "plainkey") {
			t.Errorf("JSON(%s) = %s", in, got)
		}
	}
}

// Text is untouched by the form rule: prose, code and file names inside
// a form read as they do anywhere else.
func TestFormTextRedactedAsBefore(t *testing.T) {
	texts := []string{"task-management-service", "primary_key=True", "<div>", "100%!", "PROJ-1234", "key: value",
		"the key sk-abcdefghijklmnop1234 leaked", "API_TOKEN=abc123", "src/keys/store.go", "see q0 and q1"}
	for _, s := range texts {
		b, _ := json.Marshal(s)
		in := `{"id":"frm_1","title":` + string(b) + `,"fields":[{"key":"q0","type":"string","title":` + string(b) + `,"default":` + string(b) + `}]}`
		var out struct {
			Title  string
			Fields []struct{ Key, Title, Default string }
		}
		if err := json.Unmarshal(JSON(json.RawMessage(in)), &out); err != nil {
			t.Fatal(err)
		}
		want := String(s)
		if out.Title != want || out.Fields[0].Title != want || out.Fields[0].Default != want || out.Fields[0].Key != "q0" {
			t.Errorf("%q in a form became %+v, want %q", s, out, want)
		}
	}
}
