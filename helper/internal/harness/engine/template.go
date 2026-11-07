package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
	"unicode/utf8"
)

// Templates render adapter strings such as "/model {{.id}}" with filters
// ago, tokens, truncate, default, json, replace and sha256. Output used in argv is one argv
// element; nothing reaches a shell.

var tmplCache sync.Map // string -> *template.Template

// noValue is what text/template prints for a missing map key.
const noValue = "<no value>"

// Now is the clock the ago filter reads; tests replace it.
var Now = time.Now

var funcs = template.FuncMap{
	"ago":      ago,
	"tokens":   tokens,
	"truncate": truncate,
	"default":  dflt,
	"json":     jsonFilter,
	"replace":  replace,
	"sha256":   sha256Hex,
	"object":   objectFilter,
}

// objectFilter re-encodes s, which must be a JSON object of string
// values (a model reference picked from a list), so text from the phone
// can never add fields to a request body.
func objectFilter(s any) (string, error) {
	str, ok := s.(string)
	if !ok {
		return "", fmt.Errorf("object: not a string")
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(str), &m); err != nil || len(m) == 0 || len(m) > 8 {
		return "", fmt.Errorf("object: not a JSON object of strings")
	}
	b, err := json.Marshal(m)
	return string(b), err
}

// Render expands one template with data. A string without "{{" is
// returned as is.
func Render(src string, data map[string]any) (string, error) {
	if !strings.Contains(src, "{{") {
		return src, nil
	}
	var t *template.Template
	if v, ok := tmplCache.Load(src); ok {
		t = v.(*template.Template)
	} else {
		var err error
		t, err = template.New("").Funcs(funcs).Option("missingkey=zero").Parse(src)
		if err != nil {
			return "", fmt.Errorf("template: %w", err)
		}
		tmplCache.Store(src, t)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("template: %w", err)
	}
	return strings.ReplaceAll(b.String(), noValue, ""), nil
}

// RenderJSON renders every string inside a JSON value.
func RenderJSON(raw json.RawMessage, data map[string]any) (json.RawMessage, error) {
	if len(raw) == 0 || !bytes.Contains(raw, []byte("{{")) {
		return raw, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	v, err := renderValue(v, data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func renderValue(v any, data map[string]any) (any, error) {
	switch x := v.(type) {
	case string:
		return Render(x, data)
	case []any:
		for i := range x {
			r, err := renderValue(x[i], data)
			if err != nil {
				return nil, err
			}
			x[i] = r
		}
	case map[string]any:
		for k, val := range x {
			r, err := renderValue(val, data)
			if err != nil {
				return nil, err
			}
			x[k] = r
		}
	}
	return v, nil
}

// Truthy reads a rendered condition: empty, "false", "0" and "null" are
// false.
func Truthy(s string) bool {
	switch strings.TrimSpace(s) {
	case "", "false", "0", "null", noValue:
		return false
	}
	return true
}

func ago(v any) string {
	t, ok := toTime(v)
	if !ok {
		return ""
	}
	d := Now().Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func toTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, true
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(layout, x); err == nil {
				return t, true
			}
		}
		if n, err := strconv.ParseFloat(x, 64); err == nil {
			return unixTime(n), true
		}
	case float64:
		return unixTime(x), true
	case int:
		return unixTime(float64(x)), true
	case json.Number:
		if n, err := x.Float64(); err == nil {
			return unixTime(n), true
		}
	}
	return time.Time{}, false
}

// unixTime reads seconds or, when the number is too large for seconds,
// milliseconds.
func unixTime(n float64) time.Time {
	if n > 1e12 {
		return time.UnixMilli(int64(n))
	}
	return time.Unix(int64(n), 0)
}

func tokens(v any) string {
	n, ok := toFloat(v)
	if !ok {
		return ""
	}
	switch {
	case n >= 1e6:
		return trimZero(n/1e6) + "M"
	case n >= 1e3:
		return trimZero(n/1e3) + "K"
	}
	return strconv.Itoa(int(n))
}

func trimZero(f float64) string {
	if f == math.Trunc(f) {
		return strconv.Itoa(int(f))
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

func truncate(n int, v any) string {
	s := toString(v)
	if utf8.RuneCountInString(s) <= n || n < 1 {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func dflt(d, v any) any {
	if s := toString(v); strings.TrimSpace(s) == "" {
		return d
	}
	return v
}

func jsonFilter(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool, int, int64:
		return fmt.Sprint(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
