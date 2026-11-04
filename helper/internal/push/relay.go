package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type relayPush struct {
	Token        string        `json:"token"`
	Env          string        `json:"env"`
	Ciphertext   string        `json:"ciphertext,omitempty"`
	CollapseID   string        `json:"collapseId,omitempty"`
	Kind         string        `json:"kind"`
	LiveActivity *liveActivity `json:"liveActivity,omitempty"`
}

type liveActivity struct {
	Event        string    `json:"event"`
	ContentState LiveState `json:"contentState"`
}

// errStatus is a relay answer that is not 202. Retryable marks server errors.
type errStatus struct {
	code      int
	reason    string
	retryable bool
}

func (e *errStatus) Error() string {
	if e.reason != "" {
		return fmt.Sprintf("relay %d %s", e.code, e.reason)
	}
	return fmt.Sprintf("relay %d", e.code)
}

// post sends body with bounded retries for network and 5xx failures. A 410
// marks the device dead; a 429 pauses that token for retryAfter.
func (s *Sender) post(ctx context.Context, d Device, body relayPush) error {
	if until, ok := s.paused(body.Token); ok {
		return fmt.Errorf("relay rate limited for %s", until.Sub(s.now()).Round(time.Second))
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	backoff := firstBackoff
	for attempt := 1; ; attempt++ {
		err = s.postOnce(ctx, d.RelayURL+"/v1/push", data, d, body.Token)
		var st *errStatus
		retry := err != nil && ctx.Err() == nil && (!errors.As(err, &st) || st.retryable)
		if !retry || attempt == maxAttempts {
			return err
		}
		if err := s.sleep(ctx, backoff); err != nil {
			return err
		}
		backoff *= 2
	}
}

func (s *Sender) postOnce(ctx context.Context, url string, data []byte, d Device, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return errors.New("bad relay url")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.Client.Do(req)
	if err != nil {
		// The error text holds only the URL and the transport failure.
		return fmt.Errorf("relay unreachable: %w", err)
	}
	defer resp.Body.Close()
	var answer struct {
		Error      string `json:"error"`
		Field      string `json:"field"`
		Reason     string `json:"reason"`
		RetryAfter int    `json:"retryAfter"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&answer)
	switch code := resp.StatusCode; {
	case code == http.StatusAccepted:
		return nil
	case code == http.StatusGone:
		if err := s.Devices.MarkDead(d.DeviceID, token); err != nil {
			s.logf("push: %v", err)
		}
		return &errStatus{code: code, reason: "unregistered"}
	case code == http.StatusTooManyRequests:
		wait := time.Duration(answer.RetryAfter) * time.Second
		if v, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && wait == 0 {
			wait = time.Duration(v) * time.Second
		}
		s.pause(token, wait)
		return &errStatus{code: code, reason: "rate_limited"}
	case code == http.StatusBadRequest:
		return &errStatus{code: code, reason: printable(answer.Field)}
	default:
		return &errStatus{code: code, reason: printable(answer.Reason), retryable: code >= 500}
	}
}

func (s *Sender) paused(token string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.pausedUntil[token]
	if ok && !s.now().Before(until) {
		delete(s.pausedUntil, token)
		return time.Time{}, false
	}
	return until, ok
}

func (s *Sender) pause(token string, d time.Duration) {
	d = min(max(d, time.Second), maxRetryAfter)
	s.mu.Lock()
	s.pausedUntil[token] = s.now().Add(d)
	s.mu.Unlock()
}

func (s *Sender) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// printable keeps relay-supplied reasons to short identifiers before they
// reach a log.
func printable(s string) string {
	if len(s) > 64 {
		s = s[:64]
	}
	for _, r := range s {
		if !(r == '_' || r == '-' || r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return ""
		}
	}
	return s
}
