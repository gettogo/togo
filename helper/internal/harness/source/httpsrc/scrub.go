package httpsrc

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"

	"github.com/gettogo/togo/helper/internal/harness/redact"
)

// Secret stripping at the source, before anything else in the helper reads
// a harness API answer. The engine strips secrets again before encoding;
// this pass exists so a source that is known to return keys (OpenCode's
// /config/providers returns provider keys in plain text) never depends on
// a later stage.
//
// Rules: an object key that names a secret loses its value (null), and the
// key's name is added to the object's "_set" list when the value was not
// empty, so a settings screen can say "Set" without seeing the value. A
// "headers" object loses every value the same way. String values that look
// like keys or tokens are replaced by Marker.

// Marker replaces a key-shaped string.
const Marker = "[redacted]"

// SetList names the per-object list of secret keys that had a value.
const SetList = "_set"

var secretKey = regexp.MustCompile(`(?i)(^key$|api[_-]?key|secret|token|password|passwd|authorization|bearer|cookie|credential|private[_-]?key|access[_-]?key|session[_-]?key)`)

// countKey names token counters (inputTokens, tokenUsage, tokens), which
// stay when they hold only numbers (redact.Counts).
var countKey = regexp.MustCompile(`(?i)(tokens|token_?count|token_?usage|tokens_[a-z_]+|tokenlimit|max_?tokens)$`)

var headersKey = regexp.MustCompile(`(?i)^(headers|http_?headers|env_?http_?headers)$`)

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
	regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[A-Za-z0-9._~+/-]{12,}=*`),
}

// Scrub strips secrets from a JSON document. A document that does not
// parse is returned as null: nothing unread leaves the helper.
func Scrub(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return json.RawMessage("null")
	}
	out, err := json.Marshal(ScrubValue(v))
	if err != nil {
		return json.RawMessage("null")
	}
	return out
}

// ScrubValue strips secrets from a decoded value in place.
func ScrubValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		var set []string
		for k, val := range x {
			if k == SetList {
				continue
			}
			switch {
			case headersKey.MatchString(k):
				if hdr, ok := val.(map[string]any); ok {
					x[k] = blankAll(hdr)
					if len(hdr) > 0 {
						set = append(set, k)
					}
					continue
				}
				x[k] = ScrubValue(val)
			case secretKey.MatchString(k) && !(countKey.MatchString(k) && redact.Counts(val)) && !scalar(val):
				if present(val) {
					set = append(set, k)
				}
				x[k] = nil
			default:
				x[k] = ScrubValue(val)
			}
		}
		if len(set) > 0 {
			sort.Strings(set)
			list := make([]any, 0, len(set))
			for _, s := range set {
				list = append(list, s)
			}
			x[SetList] = list
		}
		return x
	case []any:
		for i := range x {
			x[i] = ScrubValue(x[i])
		}
		return x
	case string:
		return ScrubString(x)
	}
	return v
}

// ScrubString replaces key-shaped parts of s.
func ScrubString(s string) string {
	if s == "" {
		return s
	}
	for _, re := range valuePatterns {
		s = re.ReplaceAllString(s, Marker)
	}
	return s
}

// blankAll keeps a header object's names and drops every value.
func blankAll(m map[string]any) map[string]any {
	for k := range m {
		m[k] = nil
	}
	return m
}

// scalar is a number or a boolean: never a secret, often a counter or a
// flag that only names one (tokenLimit, apiKeyRequired).
func scalar(v any) bool {
	switch v.(type) {
	case json.Number, float64, bool:
		return true
	}
	return false
}

func present(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case map[string]any:
		return len(x) > 0
	case []any:
		return len(x) > 0
	case bool:
		return x
	}
	return true
}
