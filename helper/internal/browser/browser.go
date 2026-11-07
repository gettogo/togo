// Package browser sends the web pages an agent opens to the phone. Sessions
// run with BROWSER and a PATH whose open and xdg-open are shims that call
// `togo open-url`; that command asks the daemon (local.openUrl) to publish
// a browser.open event, and opens the page on the Mac itself when no phone
// is listening. The phone can also ask for a page on the Mac
// (browser.openOnMac). Only http and https URLs ever leave this package.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Protocol names.
const (
	Capability      = "browser.v1"
	EventOpen       = "browser.open"
	MethodOpenOnMac = "browser.openOnMac"
	MethodLocalOpen = "local.openUrl"
)

// MaxURL bounds a URL in bytes. Sign-in links with long state parameters
// stay well under it.
const MaxURL = 4096

// SystemOpen is the real macOS open command the shims stand in for.
const SystemOpen = "/usr/bin/open"

// OpenEvent is the payload of a browser.open event.
type OpenEvent struct {
	URL string `json:"url"`
}

// LocalParams are the params of local.openUrl.
type LocalParams struct {
	SessionID *string `json:"sessionId"`
	URL       string  `json:"url"`
}

// LocalResult is the local.openUrl result. Delivered false means no phone
// was listening and the caller should open the page on the Mac.
type LocalResult struct {
	Delivered bool `json:"delivered"`
}

// OpenOnMacParams are the params of browser.openOnMac.
type OpenOnMacParams struct {
	URL string `json:"url"`
}

var errBadURL = errors.New("only http and https URLs with a host can be opened")

// Validate returns raw when it is an absolute http or https URL with a host
// and nothing a shell, a terminal or a log line could misread: no spaces,
// control characters or non-ASCII bytes, and at most MaxURL bytes.
func Validate(raw string) (string, error) {
	if raw == "" || len(raw) > MaxURL {
		return "", errBadURL
	}
	for i := 0; i < len(raw); i++ {
		if c := raw[i]; c <= 0x20 || c >= 0x7f {
			return "", errBadURL
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return "", errBadURL
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return "", errBadURL
	}
	if u.Hostname() == "" {
		return "", errBadURL
	}
	return raw, nil
}

// Service serves local.openUrl and browser.openOnMac.
type Service struct {
	Pub wire.Publisher
	// Attached reports whether an app is subscribed in the foreground.
	// Nil means never, so every page opens on the Mac.
	Attached func() bool
	// Open opens a validated URL on this machine. Nil runs SystemOpen on
	// macOS and xdg-open elsewhere.
	Open func(ctx context.Context, url string) error
	// Wait is how long local.openUrl waits for an app to attach before
	// answering not delivered. Zero means 3 s.
	Wait time.Duration
	// Poll is how often it checks while waiting. Zero means 200 ms.
	Poll time.Duration
}

// Methods returns the two methods for api.API.Ext.
func (s *Service) Methods() map[string]api.Method {
	return map[string]api.Method{
		MethodLocalOpen: s.localOpen,
		MethodOpenOnMac: s.openOnMac,
	}
}

// Concurrent lists the methods that block and must run off the read loop.
var Concurrent = []string{MethodLocalOpen}

func invalid(msg string) error { return &session.InvalidParamsError{Msg: msg} }

func (s *Service) localOpen(ctx context.Context, raw json.RawMessage) (any, error) {
	var p LocalParams
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	u, err := Validate(p.URL)
	if err != nil {
		return nil, invalid(err.Error())
	}
	var sid *string
	if p.SessionID != nil {
		managed, err := session.ValidateID(*p.SessionID)
		if err != nil {
			return nil, err
		}
		if managed {
			id := *p.SessionID
			sid = &id
		}
	}
	if !s.waitAttached(ctx) {
		return LocalResult{Delivered: false}, nil
	}
	s.Pub.Publish(EventOpen, sid, OpenEvent{URL: u})
	return LocalResult{Delivered: true}, nil
}

// waitAttached reports whether an app is attached now or attaches within
// Wait.
func (s *Service) waitAttached(ctx context.Context) bool {
	if s.Attached == nil || s.Pub == nil {
		return false
	}
	if s.Attached() {
		return true
	}
	wait, poll := s.Wait, s.Poll
	if wait == 0 {
		wait = 3 * time.Second
	}
	if poll == 0 {
		poll = 200 * time.Millisecond
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return s.Attached()
		case <-tick.C:
			if s.Attached() {
				return true
			}
		}
	}
}

func (s *Service) openOnMac(ctx context.Context, raw json.RawMessage) (any, error) {
	var p OpenOnMacParams
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	u, err := Validate(p.URL)
	if err != nil {
		return nil, invalid(err.Error())
	}
	open := s.Open
	if open == nil {
		open = SystemOpenURL
	}
	if err := open(ctx, u); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

// SystemOpenURL opens a URL Validate accepted with the system's opener,
// without a shell. The URL starts with http, so it can never read as a flag.
func SystemOpenURL(ctx context.Context, u string) error {
	if _, err := Validate(u); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	name := SystemOpen
	if runtime.GOOS != "darwin" {
		name = "xdg-open"
	}
	return exec.CommandContext(ctx, name, u).Run()
}
