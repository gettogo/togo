// Package approval keeps the questions agents are waiting on and answers
// them from the phone in race mode: the agent's own prompt stays live at
// the desk, and a phone answer is typed into the pane only after a fresh
// capture shows the same prompt still on screen. Whichever side answers
// first wins; a late phone answer is refused and types nothing.
package approval

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/wire"
)

// DefaultTTL is how long an approval stays answerable from the phone.
const DefaultTTL = 600 * time.Second

// Tmux is the part of *tmux.Client the store uses.
type Tmux interface {
	ListSessions(ctx context.Context) ([]tmux.SessionInfo, error)
	CapturePane(ctx context.Context, target string) (string, error)
	SendKeys(ctx context.Context, target string, literal bool, keys ...string) error
}

// Config configures a Store.
type Config struct {
	Tmux Tmux
	// Pub receives approval.open and approval.closed. It is called with the
	// store's mutex held, so it must not call back into the store (for
	// example Pending to derive needs_you); hand such work off instead.
	Pub   wire.Publisher
	Clock func() time.Time
	// TTL is how long an approval stays open; zero means DefaultTTL.
	TTL time.Duration
	// NewID returns request ids; nil means wire.NewRequestID.
	NewID func() string
}

// lifecycle is where an approval stands. Closed entries are kept until expiry
// so a late answer learns who won instead of seeing "unknown".
type lifecycle int

const (
	statePending lifecycle = iota
	stateClosed
)

type entry struct {
	a           wire.Approval
	state       lifecycle
	resolution  string // wire.Resolution*, set when closed
	fingerprint string // empty for ask approvals and hooks with no prompt on screen
	ask         chan string
}

// Store holds approvals. It implements the watch package's PromptSink.
type Store struct {
	cfg Config

	mu      sync.Mutex
	entries map[string]*entry
	// paneLocks serialise everything that reads a pane and then acts on
	// what it saw (an answer typing keys, the watcher closing a prompt),
	// so a capture, its check and the keys sent are one step per pane.
	paneLocks map[string]*sync.Mutex
}

// New returns an empty Store.
func New(cfg Config) *Store {
	cfg.TTL = cmp.Or(cfg.TTL, DefaultTTL)
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.NewID == nil {
		cfg.NewID = wire.NewRequestID
	}
	return &Store{cfg: cfg, entries: map[string]*entry{}, paneLocks: map[string]*sync.Mutex{}}
}

// lockFor returns the mutex serialising work on one session's pane.
func (s *Store) lockFor(key string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.paneLocks[key]
	if !ok {
		m = &sync.Mutex{}
		s.paneLocks[key] = m
	}
	return m
}

// Pending returns the open approvals, oldest first, for sessionID or for
// every session when sessionID is empty.
func (s *Store) Pending(sessionID string) []wire.Approval {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []wire.Approval{}
	for _, e := range s.entries {
		if e.state != statePending {
			continue
		}
		if sessionID != "" && (e.a.SessionID == nil || *e.a.SessionID != sessionID) {
			continue
		}
		out = append(out, e.a)
	}
	slices.SortFunc(out, func(a, b wire.Approval) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.RequestID, b.RequestID))
	})
	return out
}

// PromptSeen records a prompt the watcher found on a session's screen. The
// same prompt seen again is ignored. A pending approval for an earlier
// prompt is closed as answered at the desk, since its prompt was replaced;
// a hook approval opened before its prompt was drawn adopts this one.
func (s *Store) PromptSeen(ctx context.Context, sessionID string, p detect.Prompt) {
	mu := s.lockFor(sessionID)
	mu.Lock()
	defer mu.Unlock()
	s.mu.Lock()
	cur := s.pendingForSession(sessionID)
	switch {
	case cur != nil && cur.fingerprint == p.Fingerprint:
		s.mu.Unlock()
		return
	case cur != nil && cur.fingerprint == "" && cur.a.Source == wire.SourceHook:
		cur.fingerprint = p.Fingerprint
		cur.a.Options = p.Options
		cur.a.Snippet = p.Snippet
		if cur.a.Subject == nil {
			withSubject(&cur.a, p.Subject)
		}
		s.cfg.Pub.Publish(wire.EventApprovalOpen, cur.a.SessionID, wire.ApprovalOpenEvent{Approval: cur.a})
		s.mu.Unlock()
		return
	case cur != nil:
		s.closeLocked(cur, wire.ResolutionDesk)
	}
	s.mu.Unlock()
	a := s.base(ctx, sessionID, wire.SourceScreen)
	a.Title, a.Detail, a.Snippet, a.Options = p.Title, p.Detail, p.Snippet, p.Options
	withSubject(&a, p.Subject)
	s.open(a, p.Fingerprint, nil)
}

// PromptGone records that the prompt on a session's screen went away, which
// with no phone answer in flight means the desk answered it.
func (s *Store) PromptGone(ctx context.Context, sessionID string) {
	mu := s.lockFor(sessionID)
	mu.Lock()
	defer mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur := s.pendingForSession(sessionID); cur != nil {
		s.closeLocked(cur, wire.ResolutionDesk)
	}
}

// SessionGone closes the pending approvals of a session that no longer
// exists, including a hook approval whose prompt was never drawn, which
// PromptGone does not reach. Asks stay open: their caller is not the pane
// and still waits for an answer or its own timeout.
func (s *Store) SessionGone(_ context.Context, sessionID string) {
	mu := s.lockFor(sessionID)
	mu.Lock()
	defer mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for cur := s.pendingForSession(sessionID); cur != nil; cur = s.pendingForSession(sessionID) {
		s.closeLocked(cur, wire.ResolutionGone)
	}
}

// OpenFromHook opens an approval for an agent permission hook. The title
// and detail come from the hook; the options come from the screen when a
// prompt is already visible, otherwise from the prompt the watcher reports
// next. The hook's subject wins over one read off the screen: it comes from
// the tool call itself rather than from what the TUI drew.
func (s *Store) OpenFromHook(ctx context.Context, sessionID string, ev wire.HookEvent) wire.Approval {
	mu := s.lockFor(sessionID)
	mu.Lock()
	defer mu.Unlock()
	a := s.base(ctx, sessionID, wire.SourceHook)
	a.Agent = cmp.Or(ev.Agent, a.Agent)
	a.Title = truncate(cmp.Or(ev.Data.Message, ev.Data.Tool, "Permission requested"), wire.MaxTitle)
	if d := cmp.Or(ev.Data.Command, ev.Data.Tool); d != "" {
		d = truncate(d, wire.MaxDetail)
		a.Detail = &d
	}
	var fp string
	subject := ev.Data.Subject
	if sessionID != "" {
		if screen, err := s.cfg.Tmux.CapturePane(ctx, target(sessionID)); err == nil {
			if p, ok := detect.Detect(screen); ok {
				fp, a.Options, a.Snippet = p.Fingerprint, p.Options, p.Snippet
				if subject == nil {
					subject = p.Subject
				}
			}
		}
	}
	withSubject(&a, subject)
	s.mu.Lock()
	if cur := s.pendingForSession(sessionID); cur != nil {
		if fp != "" && cur.fingerprint == fp {
			// The watcher already opened this prompt; the hook only adds
			// what it knows about the request.
			cur.a.Source, cur.a.Agent, cur.a.Title, cur.a.Detail = a.Source, a.Agent, a.Title, a.Detail
			cur.a.Subject = a.Subject
			s.cfg.Pub.Publish(wire.EventApprovalOpen, cur.a.SessionID, wire.ApprovalOpenEvent{Approval: cur.a})
			s.mu.Unlock()
			return cur.a
		}
		s.closeLocked(cur, wire.ResolutionDesk)
	}
	s.mu.Unlock()
	return s.open(a, fp, nil)
}

// OpenAsk opens an approval for `togo ask`. Nothing is typed when it is
// answered: the decision is sent once on the returned channel, which is
// closed without a value when the approval closes any other way.
func (s *Store) OpenAsk(ctx context.Context, p wire.AskParams) (wire.Approval, <-chan string) {
	var a wire.Approval
	if p.SessionID != nil {
		a = s.base(ctx, *p.SessionID, wire.SourceAsk)
	} else {
		a = s.base(ctx, "", wire.SourceAsk)
	}
	a.Title = truncate(p.Title, wire.MaxTitle)
	a.AllowText = p.AllowText
	roles := AskRoles(p.Options, p.Approve, p.Deny)
	for i, label := range p.Options {
		a.Options = append(a.Options, wire.Option{N: i + 1, Label: truncate(label, wire.MaxOptionLabel), Role: roles[i]})
	}
	ch := make(chan string, 1)
	return s.open(a, "", ch), ch
}

// CloseDesk closes a pending approval as answered at the desk.
func (s *Store) CloseDesk(requestID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[requestID]; ok && e.state == statePending {
		s.closeLocked(e, wire.ResolutionDesk)
	}
}

// Expire closes a pending approval as expired before its TTL: an ask whose
// caller stopped waiting can no longer act on an answer.
func (s *Store) Expire(requestID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[requestID]; ok && e.state == statePending {
		s.closeLocked(e, wire.ResolutionExpired)
	}
}

// base fills the fields every approval shares. Bib and agent are read from
// the session's tmux options; a session that cannot be listed keeps nulls.
func (s *Store) base(ctx context.Context, sessionID, source string) wire.Approval {
	now := wire.Stamp(s.cfg.Clock())
	exp := now.Add(s.cfg.TTL)
	a := wire.Approval{
		RequestID: s.cfg.NewID(),
		Agent:     "shell",
		Source:    source,
		CreatedAt: now,
		ExpiresAt: &exp,
		State:     wire.StatePending,
	}
	if sessionID == "" {
		return a
	}
	a.SessionID = &sessionID
	infos, err := s.cfg.Tmux.ListSessions(ctx)
	if err != nil {
		return a
	}
	for _, info := range infos {
		if info.Name == sessionID {
			if info.Bib != "" {
				bib := info.Bib
				a.Bib = &bib
			}
			a.Agent = cmp.Or(info.Agent, a.Agent)
		}
	}
	return a
}

func (s *Store) open(a wire.Approval, fingerprint string, ask chan string) wire.Approval {
	e := &entry{a: a, fingerprint: fingerprint, ask: ask}
	s.mu.Lock()
	s.entries[a.RequestID] = e
	time.AfterFunc(s.cfg.TTL, func() { s.expire(a.RequestID) })
	s.cfg.Pub.Publish(wire.EventApprovalOpen, a.SessionID, wire.ApprovalOpenEvent{Approval: a})
	s.mu.Unlock()
	return a
}

// expire closes a still-pending approval at its TTL and later forgets it,
// once a late answer no longer needs to learn the outcome.
func (s *Store) expire(requestID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[requestID]
	if !ok {
		return
	}
	if e.state == statePending {
		s.closeLocked(e, wire.ResolutionExpired)
		time.AfterFunc(s.cfg.TTL, func() { s.forget(requestID) })
		return
	}
	delete(s.entries, requestID)
}

func (s *Store) forget(requestID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, requestID)
}

// pendingForSession returns the session's pending screen or hook approval.
// Asks are left alone: they are not drawn on the pane. Callers hold s.mu.
func (s *Store) pendingForSession(sessionID string) *entry {
	for _, e := range s.entries {
		if e.state == statePending && e.a.Source != wire.SourceAsk &&
			e.a.SessionID != nil && *e.a.SessionID == sessionID {
			return e
		}
	}
	return nil
}

// closeLocked marks e closed and announces it. Publishing under s.mu keeps
// approval.closed after the approval.open it ends; the publisher must not
// call back into the store. Callers hold s.mu.
func (s *Store) closeLocked(e *entry, resolution string) {
	e.state = stateClosed
	e.resolution = resolution
	if e.ask != nil {
		close(e.ask)
		e.ask = nil
	}
	s.cfg.Pub.Publish(wire.EventApprovalClosed, e.a.SessionID, wire.ApprovalClosedEvent{
		RequestID: e.a.RequestID, Resolution: resolution,
	})
}

// withSubject sets a's subject and, for apps that predate subjects, its
// detail to the subject text. A nil or invalid subject changes nothing.
func withSubject(a *wire.Approval, s *wire.Subject) {
	if s == nil || s.Validate() != nil {
		return
	}
	v := *s
	d := truncate(v.Text, wire.MaxDetail)
	a.Subject, a.Detail = &v, &d
}

func target(sessionID string) string { return "=" + sessionID + ":" }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
