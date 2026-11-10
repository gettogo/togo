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

// relayPush is the body of POST /v1/push. Target is empty for the phone
// app and targetWatch for a Watch.
type relayPush struct {
	Token        string        `json:"token"`
	Env          string        `json:"env"`
	Ciphertext   string        `json:"ciphertext,omitempty"`
	CollapseID   string        `json:"collapseId,omitempty"`
	Kind         string        `json:"kind"`
	Target       string        `json:"target,omitempty"`
	LiveActivity *liveActivity `json:"liveActivity,omitempty"`
}

// liveActivity is a Live Activity start, update or end. Attributes are
// set on start only, DismissalDate on end only; both dates are Unix
// seconds. Nothing here is sealed, so the content state carries only
// metadata, plus command text for devices that opted in.
type liveActivity struct {
	Event         string          `json:"event"`
	ContentState  LiveState       `json:"contentState"`
	Attributes    *liveAttributes `json:"attributes,omitempty"`
	DismissalDate *int64          `json:"dismissalDate,omitempty"`
	StaleDate     *int64          `json:"staleDate,omitempty"`
	// Urgent asks the relay for priority 10 on an update it would send at
	// 5 (routine progress): the one that clears an answered request, so
	// the lock screen stops asking at once.
	Urgent bool `json:"urgent,omitempty"`
	// Alert asks the relay to light the screen and sound for an update
	// that stands in for an alert push. The relay words it from the status.
	Alert bool `json:"alert,omitempty"`
}

// liveAttributes identify the activity a push-to-start creates.
type liveAttributes struct {
	HostID string `json:"hostId"`
}

// errStatus is a relay answer that is not 202. Retryable marks failures that
// may pass on a later try; deadToken marks a token APNs refused for good,
// which is forgotten (or its device marked dead) at once.
type errStatus struct {
	code      int
	reason    string
	retryable bool
	deadToken bool
}

// deadTokenReasons are the APNs reasons that condemn the token itself.
var deadTokenReasons = map[string]bool{
	"BadDeviceToken": true, "DeviceTokenNotForTopic": true, "Unregistered": true,
}

// permanentReasons are APNs reasons for a push that fails the same way every
// time. A relay from before APNs statuses were passed through answers them
// as 502, so they are told apart by name too.
var permanentReasons = map[string]bool{
	"BadCollapseId": true, "BadDeviceToken": true, "BadExpirationDate": true, "BadMessageId": true,
	"BadPriority": true, "BadTopic": true, "DeviceTokenNotForTopic": true, "DuplicateHeaders": true,
	"InvalidPushType": true, "MissingDeviceToken": true, "MissingTopic": true, "PayloadEmpty": true,
	"TopicDisallowed": true, "BadCertificate": true, "BadCertificateEnvironment": true, "Forbidden": true,
	"InvalidProviderToken": true, "MissingProviderToken": true, "UnrelatedKeyIdInToken": true,
	"BadPath": true, "MethodNotAllowed": true, "PayloadTooLarge": true, "Unregistered": true,
}

// retryableAnswer says whether a relay answer may pass on a later try: a
// server error or an expired provider token (the relay fetches a new one),
// never a reason APNs gives for a push that cannot go through.
func retryableAnswer(code int, reason string) bool {
	if permanentReasons[reason] {
		return false
	}
	return code >= 500 || reason == "ExpiredProviderToken"
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
	attempts := maxAttempts
	if s.relayIsDown(d.RelayURL) {
		// The relay failed every attempt moments ago (an outage, a captive
		// portal): one try, so the one push goroutine is not held ~33 s per
		// device per event while the queue fills and drops clears.
		attempts = 1
	}
	for attempt := 1; ; attempt++ {
		err = s.postOnce(ctx, d.RelayURL+"/v1/push", data, d, body.Token)
		s.tap("/v1/push", body.Kind, data, err)
		var st *errStatus
		retry := err != nil && ctx.Err() == nil && (!errors.As(err, &st) || st.retryable)
		if err == nil || !retry {
			s.markRelay(d.RelayURL, false)
		}
		if !retry || attempt >= attempts {
			if retry {
				s.markRelay(d.RelayURL, true)
			}
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
		s.markDead(d, token)
		return &errStatus{code: code, reason: "unregistered", deadToken: true}
	case code == http.StatusTooManyRequests:
		wait := time.Duration(answer.RetryAfter) * time.Second
		if v, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && wait == 0 {
			wait = time.Duration(v) * time.Second
		}
		s.pause(token, wait)
		return &errStatus{code: code, reason: "rate_limited"}
	case code == http.StatusBadRequest && answer.Error != "apns":
		// The relay refused the request itself, naming the field.
		return &errStatus{code: code, reason: printable(answer.Field)}
	default:
		// APNs's answer: its own status from this relay, 502 from an older one.
		reason := printable(answer.Reason)
		if deadTokenReasons[reason] {
			s.markDead(d, token)
			return &errStatus{code: code, reason: reason, deadToken: true}
		}
		return &errStatus{code: code, reason: reason, retryable: retryableAnswer(code, reason)}
	}
}

// markDead forgets a token APNs refused for good: a Live Activity token
// goes, an APNs token marks its device dead until it registers a new one.
func (s *Sender) markDead(d Device, token string) {
	if err := s.Devices.MarkDead(d.DeviceID, token); err != nil {
		s.logf("push: %v", err)
	}
}

func asStatus(err error, st **errStatus) bool { return errors.As(err, st) }

// relayDownFor is how long a relay that failed every attempt gets a
// single try per push.
const relayDownFor = time.Minute

func (s *Sender) relayIsDown(url string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.relayDown[url]
	return ok && s.now().Sub(at) < relayDownFor
}

func (s *Sender) markRelay(url string, down bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if down {
		s.relayDown[url] = s.now()
	} else {
		delete(s.relayDown, url)
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
