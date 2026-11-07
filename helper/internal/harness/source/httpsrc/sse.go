package httpsrc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Event is one server-sent event.
type Event struct {
	ID    string
	Event string
	Data  string
	// Retry is the reconnect delay the server asked for, or 0.
	Retry time.Duration
}

// Parser reads server-sent events from a stream: data lines joined with
// newlines, comment lines skipped, CRLF and CR line ends accepted, an
// event dispatched at each blank line.
type Parser struct {
	sc *bufio.Scanner
	// LastID is the id of the last event that set one, for Last-Event-ID.
	LastID string
	// Retry is the last reconnect delay the server asked for, kept even
	// when the event that carried it had no data.
	Retry time.Duration
}

// NewParser reads from r.
func NewParser(r io.Reader) *Parser {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), MaxEvent)
	sc.Split(scanLines)
	return &Parser{sc: sc}
}

// Next returns the next event with data, or an error at the end of the
// stream.
func (p *Parser) Next() (Event, error) {
	var ev Event
	var data []string
	hasData := false
	for p.sc.Scan() {
		line := p.sc.Text()
		if line == "" {
			if hasData {
				ev.Data = strings.Join(data, "\n")
				return ev, nil
			}
			ev = Event{}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "data":
			data = append(data, value)
			hasData = true
		case "event":
			ev.Event = value
		case "id":
			if !strings.ContainsRune(value, 0) {
				ev.ID = value
				p.LastID = value
			}
		case "retry":
			if ms, err := strconv.Atoi(value); err == nil && ms >= 0 {
				ev.Retry = time.Duration(ms) * time.Millisecond
				p.Retry = ev.Retry
			}
		}
		if len(data) > 0 && totalLen(data) > MaxEvent {
			return Event{}, ErrTooLarge
		}
	}
	if err := p.sc.Err(); err != nil {
		return Event{}, err
	}
	return Event{}, io.EOF
}

func totalLen(ss []string) int {
	n := 0
	for _, s := range ss {
		n += len(s) + 1
	}
	return n
}

// scanLines splits on LF, CRLF or a lone CR.
func scanLines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		if data[i] == '\r' {
			if i+1 < len(data) {
				if data[i+1] == '\n' {
					return i + 2, data[:i], nil
				}
				return i + 1, data[:i], nil
			}
			if !atEOF {
				return 0, nil, nil // a CR at the end may be half of a CRLF
			}
		}
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// SSE is the runner for kind sse: a feed that streams events from the
// endpoint's path and reconnects with backoff, resuming with Last-Event-ID.
type SSE struct{}

func (SSE) Kind() string { return spec.KindSSE }

func (SSE) Fetch(context.Context, harness.SourceSpec, harness.Env) (json.RawMessage, error) {
	return nil, fmt.Errorf("httpsrc: sse is a feed, not a source")
}

var streamClient = Client(0)

// Watch emits one record per event until ctx ends. A record's Data is the
// event's data when it is JSON (scrubbed), else a JSON object {"event",
// "data"} with the scrubbed text.
func (SSE) Watch(ctx context.Context, s harness.SourceSpec, env harness.Env, emit func(harness.Record)) error {
	var b spec.HTTP
	if err := json.Unmarshal(s.Raw, &b); err != nil {
		return fmt.Errorf("httpsrc: feed %s: %w", s.Name, err)
	}
	base, err := Base(env)
	if err != nil {
		return err
	}
	u, err := Join(base, b.Path)
	if err != nil {
		return err
	}
	delay := BackoffMin
	lastID := ""
	connected := false
	for {
		started := time.Now()
		retry, err := stream(ctx, u.String(), env.Auth, &lastID, s.Name, emit, &connected)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = err // the loop retries; the engine tracks feed health
		if retry > 0 {
			delay = min(max(retry, BackoffMin), BackoffMax)
		} else if time.Since(started) > BackoffMax {
			delay = BackoffMin
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, BackoffMax)
	}
}

// Reconnected is the record type a feed emits when its stream came back
// after a drop: events sent meanwhile were missed, so a mapper should
// close what it holds open (questions, permissions) rather than trust it.
const Reconnected = "stream.reconnected"

func stream(ctx context.Context, url, auth string, lastID *string, name string, emit func(harness.Record), connected *bool) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Cache-Control", "no-cache")
	if *lastID != "" {
		req.Header.Set("Last-Event-ID", *lastID)
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, &StatusError{Code: resp.StatusCode}
	}
	if *connected {
		emit(harness.Record{Source: name, At: time.Now(), Data: json.RawMessage(`{"type":"` + Reconnected + `"}`)})
	}
	*connected = true
	p := NewParser(resp.Body)
	var retry time.Duration
	for {
		ev, err := p.Next()
		if p.LastID != "" {
			*lastID = p.LastID
		}
		if p.Retry > 0 {
			retry = p.Retry
		}
		if err != nil {
			return retry, err
		}
		if ev.Retry > 0 {
			retry = ev.Retry
		}
		emit(harness.Record{Source: name, At: time.Now(), Data: eventData(ev)})
	}
}

func eventData(ev Event) json.RawMessage {
	d := []byte(ev.Data)
	if json.Valid(d) {
		return Scrub(d)
	}
	out, _ := json.Marshal(map[string]string{"event": ev.Event, "data": ScrubString(ev.Data)})
	return out
}
