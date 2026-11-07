package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/itchyny/gojq"

	"github.com/gettogo/togo/helper/internal/harness/redact"
)

// jqTimeout bounds one jq evaluation.
const jqTimeout = 2 * time.Second

// maxJQResults bounds the values one expression may yield.
const maxJQResults = 10000

var jqCache sync.Map // string -> *gojq.Code

// compileJQ compiles expr with the $state variable.
func compileJQ(expr string) (*gojq.Code, error) {
	if v, ok := jqCache.Load(expr); ok {
		return v.(*gojq.Code), nil
	}
	q, err := gojq.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("jq %q: %w", expr, err)
	}
	// Adapter files (including user overrides) never see the helper's
	// environment through env or $ENV.
	code, err := gojq.Compile(q, gojq.WithVariables([]string{"$state"}), gojq.WithEnvironLoader(func() []string { return nil }))
	if err != nil {
		return nil, fmt.Errorf("jq %q: %w", expr, err)
	}
	jqCache.Store(expr, code)
	return code, nil
}

// JQ runs expr over input (a decoded JSON value) with $state and returns
// every result.
func JQ(expr string, input any, state map[string]string) ([]any, error) {
	code, err := compileJQ(expr)
	if err != nil {
		return nil, err
	}
	st := make(map[string]any, len(state))
	for k, v := range state {
		st[k] = v
	}
	ctx, cancel := context.WithTimeout(context.Background(), jqTimeout)
	defer cancel()
	it := code.RunWithContext(ctx, redact.Normalize(input), st)
	var out []any
	for {
		v, ok := it.Next()
		if !ok {
			return out, nil
		}
		if e, isErr := v.(error); isErr {
			var halt *gojq.HaltError
			if errors.As(e, &halt) && halt.Value() == nil {
				return out, nil
			}
			return nil, e
		}
		out = append(out, v)
		if len(out) > maxJQResults {
			return nil, errors.New("jq: too many results")
		}
	}
}

// JQFirst returns the first result, or nil.
func JQFirst(expr string, input any, state map[string]string) (any, error) {
	out, err := JQ(expr, input, state)
	if err != nil || len(out) == 0 {
		return nil, err
	}
	return out[0], nil
}

// MapValue evaluates a screen map value against one element: a template
// when it holds "{{", else jq.
func MapValue(expr string, elem any, state map[string]string) (string, error) {
	if strings.Contains(expr, "{{") {
		data := map[string]any{"state": stateAny(state)}
		if m, ok := elem.(map[string]any); ok {
			for k, v := range m {
				data[k] = v
			}
		} else {
			data["value"] = elem
		}
		return Render(expr, data)
	}
	v, err := JQFirst(expr, elem, state)
	if err != nil {
		return "", err
	}
	return toString(v), nil
}

func stateAny(state map[string]string) map[string]any {
	out := make(map[string]any, len(state))
	for k, v := range state {
		out[k] = v
	}
	return out
}

// decode parses raw JSON for jq, or returns nil for empty input.
func decode(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	return redact.Decode(raw)
}
