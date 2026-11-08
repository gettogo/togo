// Package redact strips secrets from anything a harness adapter read before
// it leaves the helper. Three passes run in order: jq paths an adapter names
// are deleted; object keys that name a secret lose their values; string
// values shaped like known keys and tokens are replaced with a marker.
package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/itchyny/gojq"
)

// Marker replaces a secret found inside a string.
const Marker = "[redacted]"

// secretKey matches object keys whose values are secrets.
var secretKey = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd|authorization|bearer|cookie|credential|private[_-]?key|access[_-]?key|session[_-]?key|oauth)`)

// usageKey matches keys that name token usage (input_tokens, tokenUsage,
// total_output_tokens, token_count); their values are usage figures, not
// secrets, when Counts says so.
var usageKey = regexp.MustCompile(`(?i)(tokens|token_?count|tokens_[a-z_]+|token_?usage|tokens_?used)$`)

// headersKey names an object of HTTP headers (MCP server configs, custom
// providers); its values often carry credentials, so only names are kept.
var headersKey = regexp.MustCompile(`(?i)^(headers|http_?headers|env_?http_?headers)$`)

// bareKey is a field named just "key", which holds a provider's API key in
// the payloads harnesses send.
var bareKey = regexp.MustCompile(`(?i)^key$`)

// valuePatterns are the key and token shapes replaced inside strings.
var valuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----(?s:.*?)(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)`),
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{16,}`),
	regexp.MustCompile(`xox[abposr]-[A-Za-z0-9-]{8,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`),
	regexp.MustCompile(`ya29\.[0-9A-Za-z_-]{10,}`),
}

// bearerValue keeps the scheme word and hides the credential.
var bearerValue = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/-]{12,}=*`)

// envAssign hides the value of NAME=value where NAME says it is a secret,
// as env and set print it.
var envAssign = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:API_KEY|APIKEY|SECRET|TOKEN|PASSWORD|PASSWD|PRIVATE_KEY|ACCESS_KEY|CREDENTIAL|COOKIE|_AUTH\b|AUTH_|_KEY\b)[A-Z0-9_]*)=("[^"\n]*"|'[^'\n]*'|[^\s"'&]+)`)

// textSecretName is a field name, in free text, whose value is a secret:
// the name ends in one of these words (apiKey, db_password, client_secret).
const textSecretName = `[A-Za-z0-9_.-]*(?:api[_-]?key|apikey|secret|token|password|passwd|private[_-]?key|access[_-]?key|credentials?)`

// flagValue hides the value after a CLI flag that names a secret
// (--api-key VALUE, --token=VALUE). A following flag is not a value.
var flagValue = regexp.MustCompile(`(?i)(\s--?(?:api[_-]?key|token|secret|password|passwd|auth[_-]?token|access[_-]?token|client[_-]?secret)(?:[ \t]+|=))("[^"\n]*"|'[^'\n]*'|[^\s"'-][^\s"']*)`)

// urlCreds hides the password in scheme://user:pass@host.
var urlCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s/:@"']+:)([^\s/@"']+)@`)

// queryParam hides OAuth and key values in URL queries and fragments.
var queryParam = regexp.MustCompile(`(?i)([?&#](?:code|access_token|refresh_token|id_token|client_secret|token|api_?key|key|password|secret)=)([^&#\s"']+)`)

// headerLine hides a credential header's whole value, at a line start or
// inside quotes (curl -H "x-api-key: …").
var headerLine = regexp.MustCompile(`(?im)((?:^[ \t]*|["'])(?:x-api-key|api-key|x-auth-token|x-goog-api-key|authorization|proxy-authorization|cookie|set-cookie)[ \t]*:[ \t]*)([^\n"']+)`)

// quotedKV hides "apiKey": "value" (JSON or Python-ish dicts in text).
var quotedKV = regexp.MustCompile(`(?i)(["']` + textSecretName + `["'][ \t]*:[ \t]*)("[^"\n]*"|'[^'\n]*'|[^\s,}]+)`)

// yamlKV hides key: value lines (YAML, .ini-ish dumps).
var yamlKV = regexp.MustCompile(`(?im)(^[ \t]*-?[ \t]*` + textSecretName + `[ \t]*:[ \t]*)([^\s#][^\n]*)$`)

// Redactor implements harness.Redactor. The zero value is ready to use.
type Redactor struct{}

// String replaces every key or token shape inside s.
func (Redactor) String(s string) string { return String(s) }

// JSON redacts a JSON document. A document that does not parse is replaced
// by null rather than passed on.
func (Redactor) JSON(raw json.RawMessage) json.RawMessage { return JSON(raw) }

// String replaces every key or token shape inside s.
func String(s string) string {
	if s == "" {
		return s
	}
	for _, re := range valuePatterns {
		s = re.ReplaceAllString(s, Marker)
	}
	s = bearerValue.ReplaceAllString(s, "$1 "+Marker)
	s = queryParam.ReplaceAllString(s, "$1"+Marker)
	s = envAssign.ReplaceAllString(s, "$1="+Marker)
	s = flagValue.ReplaceAllString(s, "$1"+Marker)
	s = urlCreds.ReplaceAllString(s, "$1"+Marker+"@")
	s = headerLine.ReplaceAllStringFunc(s, func(m string) string {
		g := headerLine.FindStringSubmatch(m)
		if strings.Contains(g[2], Marker) && !strings.EqualFold(strings.TrimSpace(strings.TrimRight(g[1], ": \t")), "cookie") {
			return m // a bearer value already hidden keeps its scheme word
		}
		return g[1] + Marker
	})
	s = replaceKV(quotedKV, s, 1)
	return replaceKV(yamlKV, s, 6)
}

// replaceKV hides the value group of re unless it is already hidden, a
// number, a boolean, null, or shorter than minLen (prose, not a secret).
func replaceKV(re *regexp.Regexp, s string, minLen int) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		g := re.FindStringSubmatch(m)
		v := g[2]
		q := ""
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			q, v = v[:1], v[1:len(v)-1]
		}
		if v == "" || strings.Contains(v, Marker) || plainFigure(v) || (q == "" && len(v) < minLen) {
			return m
		}
		return g[1] + q + Marker + q
	})
}

// plainFigure is a number or a JSON literal: never a secret value.
func plainFigure(v string) bool {
	switch strings.ToLower(v) {
	case "true", "false", "null", "~":
		return true
	}
	for _, r := range v {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

// JSON redacts a JSON document; one that does not parse becomes null.
func JSON(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw
	}
	v, err := Decode(raw)
	if err != nil {
		return json.RawMessage("null")
	}
	out, err := Encode(Value(v))
	if err != nil {
		return json.RawMessage("null")
	}
	return out
}

// Value redacts a decoded JSON value in place and returns it.
func Value(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if SecretKey(k) && !(usageKey.MatchString(k) && Counts(val)) && !figure(val) {
				x[k] = nil
				continue
			}
			if hdr, ok := val.(map[string]any); ok && headersKey.MatchString(k) {
				for h := range hdr {
					hdr[h] = nil
				}
				continue
			}
			if _, isString := val.(string); isString && bareKey.MatchString(k) {
				x[k] = nil
				continue
			}
			x[k] = Value(val)
		}
		return x
	case []any:
		for i := range x {
			x[i] = Value(x[i])
		}
		return x
	case string:
		return String(x)
	}
	return v
}

// Counts reports whether v holds only figures: numbers, booleans and
// nulls, alone or in objects and arrays. A key that names token usage
// (tokens, tokenUsage) keeps its value only then, so a token count
// survives and a token list or string does not.
func Counts(v any) bool {
	switch x := v.(type) {
	case nil, bool, json.Number, float64, int, int64:
		return true
	case map[string]any:
		for _, val := range x {
			if !Counts(val) {
				return false
			}
		}
		return true
	case []any:
		for _, val := range x {
			if !Counts(val) {
				return false
			}
		}
		return true
	}
	return false
}

// figure is a boolean: never a secret, often a flag that only names one
// (apiKeyRequired). Numbers are not spared: a numeric token is still one.
func figure(v any) bool {
	_, ok := v.(bool)
	return ok
}

// SecretKey reports whether an object key names a secret.
func SecretKey(k string) bool { return secretKey.MatchString(k) }

// Paths deletes the values at each jq path expression (".providers[].key")
// from v and returns the result. A path that does not compile is an error;
// one that matches nothing changes nothing.
func Paths(v any, paths []string) (any, error) {
	for _, p := range paths {
		q, err := gojq.Parse("delpaths([path(" + p + ")])")
		if err != nil {
			return nil, fmt.Errorf("redact path %q: %w", p, err)
		}
		code, err := gojq.Compile(q)
		if err != nil {
			return nil, fmt.Errorf("redact path %q: %w", p, err)
		}
		it := code.Run(normalize(v))
		out, ok := it.Next()
		if !ok {
			continue
		}
		if e, isErr := out.(error); isErr {
			// A path through a missing or mistyped value deletes nothing.
			if strings.Contains(e.Error(), "cannot iterate") || strings.Contains(e.Error(), "cannot index") {
				continue
			}
			return nil, fmt.Errorf("redact path %q: %w", p, e)
		}
		v = out
	}
	return v, nil
}

// Decode parses JSON keeping numbers exact.
func Decode(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// Encode renders v without HTML escaping.
func Encode(v any) (json.RawMessage, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// normalize turns json.Number into the float64 or int gojq accepts.
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = normalize(val)
		}
		return x
	case []any:
		for i := range x {
			x[i] = normalize(x[i])
		}
		return x
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return int(i)
		}
		f, _ := x.Float64()
		return f
	}
	return v
}

// Normalize prepares a decoded value for gojq.
func Normalize(v any) any { return normalize(v) }
