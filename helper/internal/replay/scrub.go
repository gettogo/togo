// Package replay records a session's helper event stream as a JSONL fixture
// the app's replay tests feed through its store, with secrets and the
// machine's identity scrubbed out.
package replay

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness/redact"
)

// Scrubber removes what identifies the machine or its user from recorded
// frames: secrets (the harness redactor's key and value rules), the home
// directory and other named paths, the user name, the host name and email
// addresses. The zero value only applies the redactor and the email rule.
type Scrubber struct {
	// Paths are replaced by their stand-ins, longest first, so a scratch
	// folder inside the home directory keeps its own name.
	Paths map[string]string
	// Words are whole words (user name, host name) replaced by stand-ins.
	Words map[string]string
	// wrapped matches sign-in URL values found elsewhere in the frame where a
	// terminal wrapped them across lines.
	wrapped []*regexp.Regexp
}

var email = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// oauthParam is a query parameter of a sign-in URL that belongs to one login
// attempt (PKCE challenge, state, code, nonce) or is a credential.
var oauthParam = regexp.MustCompile(`(?i)\b(code_challenge|code_verifier|state|code|nonce|client_secret|access_token|refresh_token|id_token|token)=([^&\s"'\[]+)`)

// Value scrubs a decoded JSON value in place and returns it.
func (s Scrubber) Value(v any) any {
	v = redact.Value(v)
	return s.walk(v)
}

func (s Scrubber) walk(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = s.walk(val)
		}
		return x
	case []any:
		for i := range x {
			x[i] = s.walk(x[i])
		}
		return x
	case string:
		return s.String(x)
	}
	return v
}

// String scrubs one string.
func (s Scrubber) String(text string) string {
	text = redact.String(text)
	paths := make([]string, 0, len(s.Paths))
	for p := range s.Paths {
		if p != "" {
			paths = append(paths, p)
		}
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	for _, p := range paths {
		text = strings.ReplaceAll(text, p, s.Paths[p])
	}
	text = email.ReplaceAllString(text, "user@example.com")
	for _, v := range s.wrapped {
		text = v.ReplaceAllString(text, "[redacted]")
	}
	text = oauthParam.ReplaceAllString(text, "$1=[redacted]")
	for w, stand := range s.Words {
		if len(w) < 3 {
			continue
		}
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + `\b`)
		text = re.ReplaceAllString(text, stand)
	}
	return text
}

// Frame scrubs one JSON frame. A frame that does not parse is dropped
// (nil), never passed on as it was.
func (s Scrubber) Frame(raw []byte) json.RawMessage {
	v, err := redact.Decode(raw)
	if err != nil {
		return nil
	}
	s.wrapped = nil
	for _, value := range oauthValues(v, nil) {
		parts := make([]string, 0, len(value))
		for _, r := range value {
			parts = append(parts, regexp.QuoteMeta(string(r)))
		}
		s.wrapped = append(s.wrapped, regexp.MustCompile(strings.Join(parts, `(?:\n\s*)?`)))
	}
	out, err := redact.Encode(s.Value(v))
	if err != nil {
		return nil
	}
	return out
}

// oauthValues collects the sign-in URL parameter values in every string of v.
func oauthValues(v any, out []string) []string {
	switch x := v.(type) {
	case map[string]any:
		for _, val := range x {
			out = oauthValues(val, out)
		}
	case []any:
		for _, val := range x {
			out = oauthValues(val, out)
		}
	case string:
		for _, m := range oauthParam.FindAllStringSubmatch(x, -1) {
			if len(m[2]) >= 6 {
				out = append(out, m[2])
			}
		}
	}
	return out
}

// Leaks reports the forbidden strings still present in text, for the
// recorder's last check before it writes a fixture.
func Leaks(text string, forbidden []string) []string {
	var hits []string
	lower := strings.ToLower(text)
	for _, f := range forbidden {
		if len(f) >= 3 && strings.Contains(lower, strings.ToLower(f)) {
			hits = append(hits, f)
		}
	}
	if email.MatchString(strings.ReplaceAll(text, "user@example.com", "")) {
		hits = append(hits, "email")
	}
	return hits
}
