package harness

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
)

// App-facing methods, served with Capability.
const (
	MethodDescribe  = "harness.describe"
	MethodOpen      = "harness.open"
	MethodClose     = "harness.close"
	MethodHistory   = "harness.history"
	MethodScreen    = "harness.screen"
	MethodAction    = "harness.action"
	MethodSend      = "harness.send"
	MethodInterrupt = "harness.interrupt"
	MethodComplete  = "harness.complete"
	MethodLocate    = "harness.locate"
)

// Concurrent lists the methods that block on a harness (verify windows,
// one-shot CLI reads), so the daemon runs them off the read loop.
var Concurrent = []string{MethodScreen, MethodAction, MethodSend, MethodInterrupt, MethodComplete}

// Boundary limits.
const (
	DefaultLimit = 200
	MaxLimit     = 500
	MaxQuery     = 256 // runes in a completion query or screen query value
	MaxQueryKeys = 8
	maxIDRunes   = 128
)

// Methods returns the harness.* methods for api.API.Ext.
func Methods(e Engine) map[string]api.Method {
	return map[string]api.Method{
		MethodDescribe: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				ID string `json:"id"`
			}
			if err := decodeSession(raw, &p, &p.ID); err != nil {
				return nil, err
			}
			return wrap(e.Describe(ctx, p.ID))
		},
		MethodOpen: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				ID    string `json:"id"`
				After int64  `json:"after"`
				Limit int    `json:"limit"`
			}
			if err := decodeSession(raw, &p, &p.ID); err != nil {
				return nil, err
			}
			if p.After < 0 {
				return nil, invalid("after must not be negative")
			}
			sub := ConnSubscriber(rpc.ConnFrom(ctx))
			res, err := e.Open(ctx, sub, p.ID, p.After, clampLimit(p.Limit))
			if err != nil {
				return nil, mapErr(err)
			}
			out := openResponse{OpenResult: res}
			if r, ok := e.(Releaser); ok {
				out.release = func() { r.Release(sub, p.ID) }
			}
			return out, nil
		},
		MethodClose: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				ID string `json:"id"`
			}
			if err := decodeSession(raw, &p, &p.ID); err != nil {
				return nil, err
			}
			return struct{}{}, mapErr(e.Close(ctx, ConnSubscriber(rpc.ConnFrom(ctx)), p.ID))
		},
		MethodHistory: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				ID     string `json:"id"`
				Before int64  `json:"before"`
				Limit  int    `json:"limit"`
			}
			if err := decodeSession(raw, &p, &p.ID); err != nil {
				return nil, err
			}
			if p.Before < 1 {
				return nil, invalid("before must be at least 1")
			}
			return wrap(e.History(ctx, p.ID, p.Before, clampLimit(p.Limit)))
		},
		MethodScreen: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				ID     string            `json:"id"`
				Screen string            `json:"screen"`
				Query  map[string]string `json:"query"`
			}
			if err := decodeSession(raw, &p, &p.ID); err != nil {
				return nil, err
			}
			if err := checkName("screen", p.Screen); err != nil {
				return nil, err
			}
			if err := checkQuery(p.Query); err != nil {
				return nil, err
			}
			return wrap(e.Screen(ctx, p.ID, p.Screen, p.Query))
		},
		MethodAction: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p ActionRequest
			if err := decodeSession(raw, &p, &p.Session); err != nil {
				return nil, err
			}
			if err := checkName("action", p.Action); err != nil {
				return nil, err
			}
			if utf8.RuneCountInString(p.Target) > maxIDRunes {
				return nil, invalid("target is too long")
			}
			if p.Target != "" && !targetRE.MatchString(p.Target) {
				// Targets reach argv, typed commands and request bodies: ids only.
				return nil, invalid("target is not an id")
			}
			return wrap(e.Act(ctx, p))
		},
		MethodSend: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p SendRequest
			if err := decodeSession(raw, &p, &p.Session); err != nil {
				return nil, err
			}
			if err := ValidateImagePaths(p.Images); err != nil {
				return nil, invalid(err.Error())
			}
			// A prompt of pictures alone has no text.
			if p.Text != "" || len(p.Images) == 0 {
				if err := session.ValidateSendText(p.Text); err != nil {
					return nil, err
				}
			}
			if p.Mode == "" {
				p.Mode = SendQueue
			}
			if p.Mode != SendNow && p.Mode != SendQueue {
				return nil, invalid("mode must be now or queue")
			}
			return wrap(e.Send(ctx, p))
		},
		MethodInterrupt: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				ID string `json:"id"`
			}
			if err := decodeSession(raw, &p, &p.ID); err != nil {
				return nil, err
			}
			return wrap(e.Interrupt(ctx, p.ID))
		},
		MethodComplete: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p CompleteRequest
			if err := decodeSession(raw, &p, &p.Session); err != nil {
				return nil, err
			}
			if p.Kind != CompleteCommand && p.Kind != CompleteFile {
				return nil, invalid("kind must be command or file")
			}
			if utf8.RuneCountInString(p.Query) > MaxQuery || !utf8.ValidString(p.Query) {
				return nil, invalid("query is too long")
			}
			p.Limit = min(clampLimit(p.Limit), 50)
			return wrap(e.Complete(ctx, p))
		},
		MethodLocate: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				ID     string `json:"id"`
				Anchor Anchor `json:"anchor"`
			}
			if err := decodeSession(raw, &p, &p.ID); err != nil {
				return nil, err
			}
			if p.Anchor.Line < 0 || p.Anchor.Bytes < 0 || utf8.RuneCountInString(p.Anchor.Needle) > MaxNeedle {
				return nil, invalid("anchor is out of range")
			}
			return wrap(e.Locate(ctx, p.ID, p.Anchor))
		},
	}
}

func decodeSession(raw json.RawMessage, v any, id *string) error {
	if err := api.Decode(raw, v); err != nil {
		return err
	}
	_, err := session.ValidateID(*id)
	return err
}

func checkName(field, s string) error {
	if s == "" || utf8.RuneCountInString(s) > 64 {
		return invalid(field + " must be 1-64 characters")
	}
	for _, r := range s {
		ok := r == '_' || r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return invalid(field + " has a character outside [A-Za-z0-9_.-]")
		}
	}
	return nil
}

func checkQuery(q map[string]string) error {
	if len(q) > MaxQueryKeys {
		return invalid("query has too many keys")
	}
	for k, v := range q {
		if err := checkName("query key", k); err != nil {
			return err
		}
		if utf8.RuneCountInString(v) > MaxQuery || !utf8.ValidString(v) {
			return invalid("query value is too long")
		}
	}
	return nil
}

// targetRE is what an action target may hold: item, row and request ids,
// which harnesses make themselves (omp model selectors hold "/" and ":",
// OpenCode's are compact JSON). No whitespace or control characters, and no
// leading "-" so a target never reads as a flag; argv templates also refuse
// a rendered flag, and request bodies take ids through json or object.
var targetRE = regexp.MustCompile(`^[^-\s\p{Cc}][^\s\p{Cc}]*$`)

func clampLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultLimit
	case n > MaxLimit:
		return MaxLimit
	}
	return n
}

func invalid(msg string) error { return &session.InvalidParamsError{Msg: msg} }

func wrap[T any](v T, err error) (any, error) {
	if err != nil {
		return nil, mapErr(err)
	}
	return v, nil
}

// mapErr turns engine errors into protocol errors; others pass through for
// api's own mapping.
func mapErr(err error) error {
	var fb *FallbackError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNoAdapter):
		return &api.CodeError{Code: rpc.CodeRefused, Data: map[string]string{"reason": "no_adapter"}}
	case errors.As(err, &fb):
		return &api.CodeError{Code: rpc.CodeRefused, Data: map[string]string{"reason": "fallback", "detail": fb.Reason}}
	case errors.Is(err, ErrBusy):
		return &api.CodeError{Code: rpc.CodeConflict, Data: map[string]string{"reason": "busy"}}
	case errors.Is(err, ErrUnknownScreen):
		return invalid("unknown screen")
	case errors.Is(err, ErrUnknownAction):
		return invalid("unknown action")
	}
	return err
}

// Releaser is implemented by engines that hold a subscriber's
// notifications until its harness.open response is written.
type Releaser interface {
	Release(sub Subscriber, session string)
}

// openResponse encodes an OpenResult and, while rpc holds the connection's
// write lock for the response, lets the session's notifications flow, so
// none reaches the wire ahead of the response.
type openResponse struct {
	OpenResult
	release func()
}

func (o openResponse) MarshalJSON() ([]byte, error) {
	if o.release != nil {
		o.release()
	}
	return json.Marshal(o.OpenResult)
}

// ConnSubscriber adapts an rpc.Conn to Subscriber. A nil conn (a call that
// did not come over a stream) yields a subscriber that is already done.
func ConnSubscriber(c *rpc.Conn) Subscriber { return connSub{c} }

type connSub struct{ c *rpc.Conn }

func (s connSub) Notify(n Notification) error {
	if s.c == nil {
		return errors.New("harness: no connection")
	}
	return s.c.Notify(NotificationMethod, n)
}

func (s connSub) Done() <-chan struct{} {
	if s.c == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.c.Done()
}
