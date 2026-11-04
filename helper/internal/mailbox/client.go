package mailbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// StatusReplaced is the close code the relay sends when a newer connection
// for the same host takes over. Reconnecting at once would make two helpers
// with the same keys evict each other in a tight loop, so it backs off.
const StatusReplaced websocket.StatusCode = 4000

// ByPhone is the "by" value passed to Answer for mailbox answers.
const ByPhone = "phone"

// DialFunc opens a WebSocket. websocket.Dial fits; tests inject their own.
type DialFunc func(ctx context.Context, url string, opts *websocket.DialOptions) (*websocket.Conn, *http.Response, error)

// AnswerFunc hands a verified answer to the approvals store.
type AnswerFunc func(ctx context.Context, requestID, decision, by string) error

// Config configures Run. Zero durations take the defaults noted.
type Config struct {
	Keys    Keys
	Devices func() []Device // read on every rescan and every message
	Options OptionsFunc
	Answer  AnswerFunc
	Dial    DialFunc         // default websocket.Dial
	Now     func() time.Time // default time.Now
	Logf    func(format string, args ...any)

	MinBackoff     time.Duration // default 1 s
	MaxBackoff     time.Duration // default 30 s
	PingInterval   time.Duration // default 30 s
	RescanInterval time.Duration // how often the device list is reread; default 5 s
}

func (c *Config) defaults() {
	if c.Dial == nil {
		c.Dial = websocket.Dial
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
	if c.MinBackoff <= 0 {
		c.MinBackoff = time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Second
	}
	if c.MaxBackoff < c.MinBackoff {
		c.MaxBackoff = c.MinBackoff
	}
	if c.PingInterval <= 0 {
		c.PingInterval = 30 * time.Second
	}
	if c.RescanInterval <= 0 {
		c.RescanInterval = 5 * time.Second
	}
}

// Run keeps one mailbox connection per distinct relay URL among live paired
// devices, and none while no live device is paired. It returns nil when ctx
// ends, or an error at once if the keys are unusable.
func Run(ctx context.Context, cfg Config) error {
	cfg.defaults()
	if len(cfg.Keys.MailboxSecret) != 32 || cfg.Keys.HostID == "" {
		return errors.New("mailbox: host keys incomplete")
	}
	if cfg.Answer == nil || cfg.Devices == nil {
		return errors.New("mailbox: Answer and Devices are required")
	}
	r := &runner{cfg: cfg, v: NewVerifier(cfg.Keys, cfg.Devices, cfg.Options, cfg.Now)}
	return r.supervise(ctx)
}

type runner struct {
	cfg Config
	v   *Verifier
}

func (r *runner) supervise(ctx context.Context) error {
	var wg sync.WaitGroup
	live := map[string]context.CancelFunc{}
	defer func() {
		for _, cancel := range live {
			cancel()
		}
		wg.Wait()
	}()
	t := time.NewTicker(r.cfg.RescanInterval)
	defer t.Stop()
	for {
		want := relayURLs(r.cfg.Devices())
		for u, cancel := range live {
			if !want[u] {
				cancel()
				delete(live, u)
			}
		}
		for u := range want {
			if _, ok := live[u]; ok {
				continue
			}
			cctx, cancel := context.WithCancel(ctx)
			live[u] = cancel
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.relayLoop(cctx, u)
			}()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func relayURLs(devs []Device) map[string]bool {
	out := map[string]bool{}
	for _, d := range devs {
		if !d.Dead && d.RelayURL != "" {
			out[strings.TrimRight(d.RelayURL, "/")] = true
		}
	}
	return out
}

// relayLoop reconnects with jittered exponential backoff. The attempt count
// resets only after a connection stayed up for MaxBackoff and was not
// replaced, so a flapping or contested mailbox never reconnects fast.
func (r *runner) relayLoop(ctx context.Context, relayURL string) {
	attempt := 0
	for {
		up, code := r.session(ctx, relayURL)
		if ctx.Err() != nil {
			return
		}
		if code == StatusReplaced {
			r.cfg.Logf("mailbox: connection replaced by another client for this host")
		} else if up >= r.cfg.MaxBackoff {
			attempt = 0
		}
		d := backoff(attempt, r.cfg.MinBackoff, r.cfg.MaxBackoff, rand.Float64)
		attempt++
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
	}
}

// backoff returns a delay in [base/2, base] where base = min*2^attempt
// capped at max.
func backoff(attempt int, minD, maxD time.Duration, rnd func() float64) time.Duration {
	base := minD
	for i := 0; i < attempt && base < maxD; i++ {
		base *= 2
	}
	if base > maxD {
		base = maxD
	}
	half := base / 2
	return half + time.Duration(rnd()*float64(base-half))
}

// frame is a relay message. Client frames reuse it with only Type and ID.
type frame struct {
	Type       string `json:"type"`
	ID         string `json:"id,omitempty"`
	Ciphertext string `json:"ciphertext,omitempty"`
	ReceivedAt int64  `json:"receivedAt,omitempty"`
}

// session runs one connection until it fails. It reports how long the
// connection was up and the close code (-1 when there was none).
func (r *runner) session(ctx context.Context, relayURL string) (time.Duration, websocket.StatusCode) {
	url := relayURL + "/v1/mailbox/" + r.cfg.Keys.HostID + "/ws"
	h := http.Header{}
	h.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(r.cfg.Keys.MailboxSecret))
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, resp, err := r.cfg.Dial(dctx, url, &websocket.DialOptions{HTTPHeader: h})
	cancel()
	if err != nil {
		if resp != nil {
			r.cfg.Logf("mailbox: dial failed with HTTP %d", resp.StatusCode)
		} else {
			r.cfg.Logf("mailbox: dial failed")
		}
		return 0, -1
	}
	start := time.Now()
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 10)

	sctx, stop := context.WithCancel(ctx)
	defer stop()
	var lastRead atomic.Int64
	lastRead.Store(time.Now().UnixNano())
	go r.pinger(sctx, conn, &lastRead)

	for {
		_, data, err := conn.Read(sctx)
		if err != nil {
			code := websocket.CloseStatus(err)
			if ctx.Err() == nil {
				r.cfg.Logf("mailbox: connection closed (code %d)", code)
			}
			return time.Since(start), code
		}
		lastRead.Store(time.Now().UnixNano())
		var f frame
		if json.Unmarshal(data, &f) != nil || f.Type != "msg" || f.ID == "" {
			continue // pong or noise; nothing to ack
		}
		if r.handle(sctx, f) {
			ack, _ := json.Marshal(frame{Type: "ack", ID: f.ID})
			wctx, wcancel := context.WithTimeout(sctx, 10*time.Second)
			err := conn.Write(wctx, websocket.MessageText, ack)
			wcancel()
			if err != nil {
				return time.Since(start), websocket.CloseStatus(err)
			}
		}
	}
}

// pinger sends an application ping each interval (the relay answers it
// without waking the mailbox) and drops a connection that has been silent
// for three intervals.
func (r *runner) pinger(ctx context.Context, conn *websocket.Conn, lastRead *atomic.Int64) {
	t := time.NewTicker(r.cfg.PingInterval)
	defer t.Stop()
	ping := []byte(`{"type":"ping"}`)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if time.Since(time.Unix(0, lastRead.Load())) > 3*r.cfg.PingInterval {
			r.cfg.Logf("mailbox: relay silent, reconnecting")
			conn.CloseNow()
			return
		}
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := conn.Write(wctx, websocket.MessageText, ping)
		cancel()
		if err != nil {
			return
		}
	}
}

// handle verifies and applies one message and reports whether to ack it.
// Every answer is acked once it is applied or definitively refused; only a
// shutdown mid-handling leaves it for redelivery.
func (r *runner) handle(ctx context.Context, f frame) bool {
	tag := idHash(f.ID)
	a, err := r.v.verify(f.Ciphertext)
	if err != nil {
		r.cfg.Logf("mailbox: dropped %s: %v", tag, err)
		return true
	}
	if err := r.cfg.Answer(ctx, a.RequestID, a.Decision, ByPhone); err != nil {
		if ctx.Err() != nil {
			r.v.release(a.RequestID)
			return false
		}
		r.cfg.Logf("mailbox: answer %s not applied", tag)
		return true
	}
	r.cfg.Logf("mailbox: answer %s applied", tag)
	return true
}
