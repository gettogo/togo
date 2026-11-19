package approval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// A helper restart (`togo update`) must not lose a request the phone was
// told about: the store keeps its open screen and hook approvals in
// Config.Path ($TOGO_HOME/run/approvals.json, 0600, folder 0700) and opens
// them again, under the same ids, when it is built. An answer from a
// notification sent before the restart then types into the pane as before.
//
// Only an approval tied to a prompt already read off the screen is kept
// (it has the prompt's fingerprint). That is what keeps a restored request
// on its own prompt: it can never adopt another (only a hook approval with
// no fingerprint does, in PromptSeen), the watcher reading any other
// prompt closes it, recheck closes it when the screen shows another, and
// an answer is typed only after a capture shows its fingerprint. A hook
// approval whose prompt was not drawn yet had no buttons on the phone; the
// prompt, once drawn, opens a request of its own.
//
// Kept per approval: its id, session, agent, bib, source, title, detail,
// subject and options as the phone was sent them, whether it can be
// answered, its times, and the fingerprint of its prompt. Not kept: the
// screen snippet (read again from the screen), sign-in steps (read again
// as new requests, answered in the app), asks (their caller's connection
// ends with the helper) and anything the phone typed.
//
// Set and cleared: written whenever the open set changes (open, adopt,
// extend, close); an approval leaves the file when it is answered by the
// phone or at the desk, expires, its prompt leaves the screen, or its
// session is removed. On load, expired ones are dropped, and each one is
// checked against its pane: a prompt that is gone was answered at the desk
// while the helper was down. Agent exits and relaunches close approvals
// through PromptGone and SessionGone as before; turn ends, interrupts and
// reconnects do not touch them.
const (
	persistVersion = 1
	// persistMax bounds the file: approvals are one per session at a time.
	persistMax = 256
	// persistBytes bounds what is read back.
	persistBytes = 1 << 20
)

type persisted struct {
	V         int         `json:"v"`
	Approvals []persistOf `json:"approvals"`
}

type persistOf struct {
	Approval    wire.Approval `json:"approval"`
	Fingerprint string        `json:"fingerprint,omitempty"`
}

// keepable reports whether e is kept across a restart.
func keepable(e *entry) bool {
	return e.state == statePending && e.a.Source != wire.SourceAsk && e.a.SessionID != nil && e.a.SignIn == nil && e.fingerprint != ""
}

// saveLocked hands the open approvals to the writer, which writes them off
// the lock. Callers hold s.mu.
func (s *Store) saveLocked() {
	if s.cfg.Path == "" {
		return
	}
	out := persisted{V: persistVersion, Approvals: []persistOf{}}
	for _, e := range s.entries {
		if !keepable(e) || len(out.Approvals) == persistMax {
			continue
		}
		a := e.a
		a.Snippet = ""
		out.Approvals = append(out.Approvals, persistOf{Approval: a, Fingerprint: e.fingerprint})
	}
	var b []byte
	if len(out.Approvals) > 0 {
		var err error
		if b, err = json.Marshal(out); err != nil {
			s.logf("approval: the open requests could not be kept for a restart: %v", err)
			return
		}
	}
	s.saver.put(s.cfg.Path, b)
}

// writeKept replaces path with b, 0600 in a 0700 folder, by a synced
// rename; a nil b removes it.
func writeKept(path string, b []byte) error {
	if b == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if err == nil {
		err = f.Chmod(0o600)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// load reads the approvals kept by the helper before a restart. An
// unreadable file, or one of a newer format, is ignored and replaced.
func (s *Store) load() []persistOf {
	if s.cfg.Path == "" {
		return nil
	}
	f, err := os.OpenFile(s.cfg.Path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil
	}
	defer f.Close()
	var in persisted
	dec := json.NewDecoder(&limited{f: f, n: persistBytes})
	if dec.Decode(&in) != nil || in.V != persistVersion {
		s.logf("approval: ignoring an unreadable or newer file of open requests")
		return nil
	}
	now := s.cfg.Clock()
	var out []persistOf
	for _, p := range in.Approvals {
		a := p.Approval
		if a.RequestID == "" || a.SessionID == nil || a.ExpiresAt == nil || !now.Before(*a.ExpiresAt) ||
			a.Source == wire.SourceAsk || a.SignIn != nil || p.Fingerprint == "" || len(out) == persistMax {
			continue
		}
		a.State = wire.StatePending
		p.Approval = a
		out = append(out, p)
	}
	return out
}

// restore opens the kept approvals again, then checks each against its
// pane in the background.
func (s *Store) restore() {
	kept := s.load()
	s.mu.Lock()
	now := s.cfg.Clock()
	for _, p := range kept {
		if _, dup := s.entries[p.Approval.RequestID]; dup {
			continue
		}
		e := &entry{a: p.Approval, fingerprint: p.Fingerprint, restored: true}
		s.entries[e.a.RequestID] = e
		id := e.a.RequestID
		time.AfterFunc(e.a.ExpiresAt.Sub(now), func() { s.expire(id) })
		// Announced to the hub (its list, and the engine's on the
		// session's start) without a write each.
		s.pub.Publish(wire.EventApprovalOpen, e.a.SessionID, openEvent(e))
	}
	s.saveLocked()
	s.mu.Unlock()
	for _, p := range kept {
		go s.recheck(p.Approval.RequestID)
	}
}

// recheck closes a restored approval whose prompt left the screen, or was
// replaced, while the helper was down: the desk answered it. When its
// prompt still shows, the options are taken from the screen again, not
// from the file.
func (s *Store) recheck(requestID string) {
	s.mu.Lock()
	e, ok := s.entries[requestID]
	if !ok || e.state != statePending || e.a.SessionID == nil {
		s.mu.Unlock()
		return
	}
	sessionID, fingerprint := *e.a.SessionID, e.fingerprint
	s.mu.Unlock()
	mu := s.lockFor(sessionID)
	mu.Lock()
	defer mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	screen, err := s.cfg.Tmux.CapturePane(ctx, target(sessionID))
	resolution := ""
	switch p, shown := detect.Detect(screen); {
	case err != nil:
		resolution = wire.ResolutionGone
	case !shown || fingerprint == "" || fingerprint != p.Fingerprint:
		resolution = wire.ResolutionDesk
	default:
		s.mu.Lock()
		defer s.mu.Unlock()
		if e, ok = s.entries[requestID]; ok && e.state == statePending && !reflect.DeepEqual(e.a.Options, p.Options) {
			e.a.Options = p.Options
			e.a.Answerable = nil
			if p.ReadOnly {
				e.a.Answerable = notAnswerable()
			}
			s.cfg.Pub.Publish(wire.EventApprovalOpen, e.a.SessionID, openEvent(e))
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok = s.entries[requestID]; ok && e.state == statePending {
		s.closeLocked(e, resolution)
	}
}

// openEvent announces e; a request opened again after a restart is marked
// so the push sender does not ring for it a second time.
func openEvent(e *entry) wire.ApprovalOpenEvent {
	return wire.ApprovalOpenEvent{Approval: e.a, Restored: e.restored}
}

// limited reads at most n bytes.
type limited struct {
	f *os.File
	n int64
}

func (l *limited) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errTooLarge
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	k, err := l.f.Read(p)
	l.n -= int64(k)
	return k, err
}

var errTooLarge = errors.New("file too large")

// persistPub publishes and then writes the open approvals: every change
// to them is published, under s.mu.
type persistPub struct {
	inner wire.Publisher
	s     *Store
}

func (p persistPub) Publish(kind string, id *string, payload any) {
	p.inner.Publish(kind, id, payload)
	if kind == wire.EventApprovalOpen || kind == wire.EventApprovalClosed {
		p.s.saveLocked()
	}
}
