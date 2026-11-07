// Package httpsrc reads a harness's local HTTP API: one-shot requests for
// screens and actions (kind http) and a server-sent event stream for feeds
// (kind sse). Requests only ever go to the loopback endpoint the helper
// assigned to the session (OpenCode's --port), never to an address from an
// adapter file, and every answer is scrubbed of secrets before it is
// returned.
package httpsrc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func init() {
	harness.RegisterRunner(HTTP{})
	harness.RegisterRunner(SSE{})
}

// Limits.
const (
	MaxBody     = 2 << 20 // one response body
	MaxEvent    = 1 << 20 // one server-sent event's data
	BackoffMin  = 500 * time.Millisecond
	BackoffMax  = 10 * time.Second
	DialTimeout = 3 * time.Second
)

// VarPort is the Env.Vars key holding the assigned port when Endpoint is
// empty.
const VarPort = "port"

// Errors.
var (
	ErrNotLoopback = errors.New("httpsrc: endpoint is not a loopback address")
	ErrBadPath     = errors.New("httpsrc: path must be a plain absolute path")
	ErrTooLarge    = errors.New("httpsrc: response is larger than the cap")
)

// StatusError is a non-2xx answer. It carries no body text.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return "httpsrc: status " + strconv.Itoa(e.Code) }

// Base returns the session's endpoint as a base URL, checked to be plain
// http on a loopback address with a port.
func Base(env harness.Env) (*url.URL, error) {
	ep := env.Endpoint
	if ep == "" && env.Vars[VarPort] != "" {
		ep = "127.0.0.1:" + env.Vars[VarPort]
	}
	if ep == "" {
		return nil, errors.New("httpsrc: session has no endpoint")
	}
	if !strings.Contains(ep, "://") {
		ep = "http://" + ep
	}
	u, err := url.Parse(ep)
	if err != nil || u.Scheme != "http" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrNotLoopback
	}
	host, port := u.Hostname(), u.Port()
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return nil, ErrNotLoopback
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, ErrNotLoopback
		}
	} else {
		host = "127.0.0.1"
	}
	return &url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}, nil
}

// Join resolves a rendered path (with an optional query) against base. The
// path must start with one slash and may not carry a scheme, host, user,
// fragment, dot segments or control characters.
func Join(base *url.URL, path string) (*url.URL, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\#") {
		return nil, ErrBadPath
	}
	for _, r := range path {
		if unicode.IsControl(r) || r == ' ' {
			return nil, ErrBadPath
		}
	}
	ref, err := url.Parse(path)
	if err != nil || ref.Scheme != "" || ref.Host != "" || ref.User != nil {
		return nil, ErrBadPath
	}
	for _, seg := range strings.Split(ref.Path, "/") {
		if seg == "." || seg == ".." {
			return nil, ErrBadPath
		}
	}
	u := *base
	u.Path = ref.Path
	u.RawPath = ref.RawPath
	u.RawQuery = ref.RawQuery
	return &u, nil
}

// loopbackOnly refuses any connection to an address that is not loopback,
// whatever name resolution or a redirect produced.
func loopbackOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return ErrNotLoopback
	}
	return nil
}

// Client returns the HTTP client every request uses: no proxy, loopback
// dials only, redirects not followed.
func Client(timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: DialTimeout, Control: loopbackOnly}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         d.DialContext,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     30 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

var client = Client(0)

var methods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// HTTP is the runner for kind http: one request, the scrubbed JSON answer.
type HTTP struct{}

func (HTTP) Kind() string { return spec.KindHTTP }

func (HTTP) Watch(context.Context, harness.SourceSpec, harness.Env, func(harness.Record)) error {
	return harness.ErrNotStreaming
}

// Fetch sends the request. A JSON answer comes back scrubbed; an empty one
// is null; any other text is returned as a scrubbed JSON string.
func (HTTP) Fetch(ctx context.Context, s harness.SourceSpec, env harness.Env) (json.RawMessage, error) {
	var b spec.HTTP
	if err := json.Unmarshal(s.Raw, &b); err != nil {
		return nil, fmt.Errorf("httpsrc: source %s: %w", s.Name, err)
	}
	return Do(ctx, env, b)
}

// Do runs one request against the session's endpoint.
func Do(ctx context.Context, env harness.Env, b spec.HTTP) (json.RawMessage, error) {
	method := strings.ToUpper(b.Method)
	if method == "" {
		method = "GET"
	}
	if !methods[method] {
		return nil, fmt.Errorf("httpsrc: method %q is not allowed", b.Method)
	}
	base, err := Base(env)
	if err != nil {
		return nil, err
	}
	u, err := Join(base, b.Path)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if strings.TrimSpace(b.Body) != "" {
		if !json.Valid([]byte(b.Body)) {
			return nil, errors.New("httpsrc: body is not valid JSON")
		}
		body = strings.NewReader(b.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if env.Auth != "" {
		req.Header.Set("Authorization", env.Auth)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxBody {
		return nil, ErrTooLarge
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &StatusError{Code: resp.StatusCode}
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return json.RawMessage("null"), nil
	}
	if json.Valid(raw) {
		return Scrub(raw), nil
	}
	text, _ := json.Marshal(ScrubString(string(raw)))
	return text, nil
}
