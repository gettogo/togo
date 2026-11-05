package daemon

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/approval"
	"github.com/gettogo/togo/helper/internal/fsread"
	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/hub"
	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/mailbox"
	"github.com/gettogo/togo/helper/internal/ports"
	"github.com/gettogo/togo/helper/internal/push"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/upload"
	"github.com/gettogo/togo/helper/internal/watch"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Capabilities are what a daemon built by Compose serves.
var Capabilities = []string{
	"session.v1", "agents.v1", "fs.v1", "capture.v1",
	"events.v1", "approvals.v1", "mute.v1", "push.v1", "git.v1", "ports.v1", "upload.v1",
}

// Concurrent lists the methods the daemon runs off the connection's read
// loop because they block.
var Concurrent = local.Concurrent

// ComposeOptions configures Compose.
type ComposeOptions struct {
	// Home is TOGO_HOME.
	Home string
	// TogoPath is the absolute path of the helper binary the tap runs.
	TogoPath string
	// RelayClient posts pushes; nil means the sender's default client.
	RelayClient *http.Client
	// Getenv reads test-only switches; nil means os.Getenv.
	Getenv func(string) string
	Logf   func(format string, args ...any)
}

// Components are the services the daemon composes into its handler.
type Components struct {
	API       *api.API
	Hub       *hub.Hub
	Approvals *approval.Store
	Watch     *watch.Watcher
	Local     *local.Handlers
	Push      *push.Service
	Sender    *push.Sender
	Upload    *upload.Service

	logf      func(format string, args ...any)
	followups chan string
}

// Compose wires every service around base, whose Sessions must be set, and
// merges their methods into base.Ext.
func Compose(base *api.API, o ComposeOptions) (*Components, error) {
	svc := base.Sessions
	if svc == nil || svc.Tmux == nil {
		return nil, errors.New("compose: session service without tmux client")
	}
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	getenv := o.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	c := &Components{API: base, logf: logf, followups: make(chan string, 64)}
	c.Hub = hub.New(svc, nil)
	pub := newDoneDedupe(c.Hub, time.Now)
	c.Approvals = approval.New(approval.Config{Tmux: svc.Tmux, Pub: c.Hub})
	w, err := watch.New(watch.Config{
		Tmux: svc.Tmux, Sessions: svc, TogoPath: o.TogoPath, Home: o.Home,
		Status: c.Hub, Prompts: c.Approvals, Pub: pub,
	})
	if err != nil {
		return nil, err
	}
	c.Watch = w
	c.Push = &push.Service{Home: o.Home, Getenv: getenv}
	c.Sender = &push.Sender{
		Hub: c.Hub, Keys: c.Push.HostKeys, Devices: c.Push.Devices(),
		Session: c.sessionInfo, Client: o.RelayClient, Logf: logf,
	}
	c.Upload = &upload.Service{Home: o.Home, Sessions: svc, Logf: logf}
	c.Local = &local.Handlers{Hub: c.Hub, Approvals: c.Approvals, Pub: pub, Reach: c.reach}
	git := gitx.Git{}

	ext := map[string]api.Method{}
	for _, m := range []map[string]api.Method{
		c.Hub.Methods(),
		c.Approvals.Methods(),
		c.Watch.Methods(),
		c.Local.Methods(),
		c.Push.Methods(),
		(&gitx.Service{Sessions: svc, Git: git}).Methods(),
		(&fsread.Service{Sessions: svc, Git: git}).Methods(),
		(&ports.Service{Sessions: svc, Panes: svc.Tmux}).Methods(),
		c.Upload.Methods(),
	} {
		for name := range m {
			if _, dup := ext[name]; dup {
				return nil, fmt.Errorf("compose: method %s registered twice", name)
			}
		}
		maps.Copy(ext, m)
	}
	base.Ext = ext
	base.Logf = logf

	// Status follows approvals: a session is needs_you while one is open.
	// The hub cannot publish from inside its own consumer, so the session
	// id is handed to Run.
	c.Hub.OnEvent(func(ev wire.Event) {
		if ev.SessionID == nil || (ev.Kind != wire.EventApprovalOpen && ev.Kind != wire.EventApprovalClosed) {
			return
		}
		select {
		case c.followups <- *ev.SessionID:
		default:
		}
	})
	return c, nil
}

// Handler returns the rpc handler serving every component. Session
// changes made through it are published to subscribers, and every request
// renews its connection's foreground lease.
func (c *Components) Handler() rpc.Handler {
	return c.Hub.Renewing(&publishingHandler{c: c})
}

// Run drives the background services until ctx ends.
func (c *Components) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Go(func() { c.Watch.Run(ctx) })
	wg.Go(func() { c.Sender.Run(ctx) })
	wg.Go(func() { c.Upload.Run(ctx) })
	wg.Go(func() { c.runMailbox(ctx) })
	wg.Go(func() { c.runFollowups(ctx) })
	wg.Wait()
}

func (c *Components) runFollowups(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-c.followups:
			c.publishSession(ctx, id)
		}
	}
}

func (c *Components) publishSession(ctx context.Context, id string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sess, err := c.API.Sessions.Resolve(ctx, id)
	if err != nil {
		return
	}
	c.Hub.PublishSession(sess)
}

// sessionInfo feeds the push sender. A failed lookup reports unknown,
// which the sender treats as unmuted.
func (c *Components) sessionInfo(id string) (push.SessionInfo, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := c.API.Sessions.Resolve(ctx, id)
	if err != nil {
		return push.SessionInfo{}, false
	}
	agent := sess.Agent
	return push.SessionInfo{Bib: sess.Bib, Agent: &agent, Muted: c.Hub.Muted(id)}, true
}

// reach reports whether a notification can reach the user: an app in the
// foreground (within its lease) sees it directly, otherwise a live paired device must exist
// and the session must not be muted.
func (c *Components) reach(sessionID *string) bool {
	if c.Hub.ForegroundSubscribers() > 0 {
		return true
	}
	if sessionID != nil && c.Hub.Muted(*sessionID) {
		return false
	}
	devs, err := c.Push.Devices().List()
	if err != nil {
		return false
	}
	for _, d := range devs {
		if !d.Dead {
			return true
		}
	}
	return false
}

// mailboxPoll is how often the daemon checks for a first paired device
// before it reads the host keys and starts the mailbox reader.
const mailboxPoll = 2 * time.Second

// runMailbox starts the mailbox reader once a device is paired, so a host
// that never pairs never creates keys or dials the relay.
func (c *Components) runMailbox(ctx context.Context) {
	t := time.NewTicker(mailboxPoll)
	defer t.Stop()
	for len(c.mailboxDevices()) == 0 {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
	k, err := c.Push.HostKeys()
	if err != nil {
		c.logf("mailbox: host keys: %v", err)
		return
	}
	err = mailbox.Run(ctx, mailbox.Config{
		Keys:    mailbox.Keys{HostID: k.HostID, Private: k.X25519Priv, MailboxSecret: k.MailboxSecret[:]},
		Devices: c.mailboxDevices,
		Options: c.pendingOptions,
		Answer:  c.Approvals.Answer,
		Logf:    c.logf,
	})
	if err != nil {
		c.logf("mailbox: %v", err)
	}
}

// mailboxDevices converts the pairing store for the reader. Unpaired
// devices are absent and dead ones are marked, so answers signed by either
// are refused.
func (c *Components) mailboxDevices() []mailbox.Device {
	devs, err := c.Push.Devices().List()
	if err != nil {
		c.logf("mailbox: devices: %v", err)
		return nil
	}
	out := make([]mailbox.Device, 0, len(devs))
	for _, d := range devs {
		key, err := base64.RawURLEncoding.DecodeString(d.ApprovalKey)
		if err != nil || len(key) != ed25519.PublicKeySize {
			continue
		}
		out = append(out, mailbox.Device{
			DeviceID: d.DeviceID, ApprovalKey: ed25519.PublicKey(key),
			RelayURL: d.RelayURL, Dead: d.Dead,
		})
	}
	return out
}

func (c *Components) pendingOptions(requestID string) ([]wire.Option, bool) {
	for _, a := range c.Approvals.Pending("") {
		if a.RequestID == requestID {
			return a.Options, true
		}
	}
	return nil, false
}

// publishingHandler publishes the session changes the built-in methods
// make, which the hub otherwise learns only from later activity.
type publishingHandler struct{ c *Components }

func (p *publishingHandler) Handle(ctx context.Context, method string, params json.RawMessage) (any, *rpc.Error) {
	result, rerr := p.c.API.Handle(ctx, method, params)
	if rerr != nil {
		return result, rerr
	}
	switch method {
	case "session.create", "session.rename":
		if sess, ok := result.(session.Session); ok {
			if method == "session.create" {
				// Attach the tap now rather than at the next reconcile, so
				// the agent's first screen is seen.
				go p.c.Watch.Reconcile(context.WithoutCancel(ctx))
			}
			return p.c.Hub.PublishSession(sess), nil
		}
	case "session.kill":
		// The watcher announces session.removed for a session it was
		// tapping when it finds it gone; reconciling now makes that
		// immediate without a second announcement.
		p.c.Watch.Reconcile(ctx)
	}
	return result, nil
}

// doneWindow is how close together two "done" notifications for one
// session must be to count as the same turn ending: the agent's Stop hook
// and the idle timer both report it.
const doneWindow = 30 * time.Second

// doneDedupe drops a second done notification for a session within
// doneWindow and passes every other event through.
type doneDedupe struct {
	pub wire.Publisher
	now func() time.Time

	mu   sync.Mutex
	last map[string]time.Time
}

func newDoneDedupe(pub wire.Publisher, now func() time.Time) *doneDedupe {
	return &doneDedupe{pub: pub, now: now, last: map[string]time.Time{}}
}

func (d *doneDedupe) Publish(kind string, sessionID *string, payload any) {
	if kind == wire.EventNotify && sessionID != nil && isDone(payload) {
		d.mu.Lock()
		now := d.now()
		prev, seen := d.last[*sessionID]
		if seen && now.Sub(prev) < doneWindow {
			d.mu.Unlock()
			return
		}
		d.last[*sessionID] = now
		d.mu.Unlock()
	}
	if kind == wire.EventSessionRemoved && sessionID != nil {
		d.mu.Lock()
		delete(d.last, *sessionID)
		d.mu.Unlock()
	}
	d.pub.Publish(kind, sessionID, payload)
}

func isDone(payload any) bool {
	switch p := payload.(type) {
	case wire.NotifyEvent:
		return p.Level == wire.LevelDone
	case *wire.NotifyEvent:
		return p != nil && p.Level == wire.LevelDone
	}
	return false
}
