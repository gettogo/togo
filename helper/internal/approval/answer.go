package approval

import (
	"context"
	"errors"
	"fmt"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// ErrUnknown is returned by Answer for a request id the store never had or
// has forgotten.
var ErrUnknown = errors.New("approval: unknown request")

// Conflict is the data of a -32006 answer refusal.
type Conflict struct {
	Reason string `json:"reason"`
	By     string `json:"by,omitempty"`
}

func conflict(reason, by string) error {
	return &api.CodeError{Code: rpc.CodeConflict, Data: Conflict{Reason: reason, By: by}}
}

func invalid(msg string) error { return &session.InvalidParamsError{Msg: msg} }

// Answer applies a decision to a pending approval. by names the answerer
// (wire.ByPhone) and becomes the approval.closed resolution.
//
// For a prompt on screen the pane is captured again and the keys are
// resolved from that capture, all while holding the pane's lock: if the
// prompt is gone or changed, the desk got there first, so nothing is typed
// and the approval closes as gone. Errors are *api.CodeError (-32006),
// *session.InvalidParamsError, ErrUnknown, or a tmux failure.
func (s *Store) Answer(ctx context.Context, requestID, raw, by string) error {
	if by != wire.ByPhone {
		return fmt.Errorf("approval: answerer %q is not the phone", by)
	}
	d, err := wire.ParseDecision(raw)
	if err != nil {
		return invalid(err.Error())
	}
	s.mu.Lock()
	e, ok := s.entries[requestID]
	s.mu.Unlock()
	if !ok {
		return ErrUnknown
	}
	// Asks never touch a pane, so s.mu alone orders their answers.
	if e.a.SessionID != nil && e.a.Source != wire.SourceAsk {
		mu := s.lockFor(*e.a.SessionID)
		mu.Lock()
		defer mu.Unlock()
	}

	s.mu.Lock()
	if err := s.checkOpenLocked(e); err != nil {
		s.mu.Unlock()
		return err
	}
	if e.a.Answerable != nil && !*e.a.Answerable {
		s.mu.Unlock()
		return invalid("this request can only be answered in the terminal")
	}
	opt, err := pick(e.a, d)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if e.a.Source == wire.SourceAsk {
		e.ask <- answerFor(e.a, d, opt)
		e.ask = nil
		s.closeLocked(e, by)
		s.mu.Unlock()
		return nil
	}
	a, fp := e.a, e.fingerprint
	s.mu.Unlock()

	if err := s.typeAnswer(ctx, a, fp, d, opt); err != nil {
		if errors.Is(err, errPromptGone) {
			s.closeIfPending(e, wire.ResolutionGone)
			return conflict(wire.ConflictPromptGone, "")
		}
		return err
	}
	s.closeIfPending(e, by)
	return nil
}

// checkOpenLocked reports why e can no longer be answered, closing it as
// expired when its time has passed. Callers hold s.mu.
func (s *Store) checkOpenLocked(e *entry) error {
	if e.state == statePending && e.a.ExpiresAt != nil && !s.cfg.Clock().Before(*e.a.ExpiresAt) {
		s.closeLocked(e, wire.ResolutionExpired)
	}
	if e.state == statePending {
		return nil
	}
	switch e.resolution {
	case wire.ResolutionPhone:
		return conflict(wire.ConflictAnswered, wire.ByPhone)
	case wire.ResolutionDesk:
		return conflict(wire.ConflictAnswered, wire.ByDesk)
	case wire.ResolutionExpired:
		return conflict(wire.ConflictExpired, "")
	}
	return conflict(wire.ConflictPromptGone, "")
}

func (s *Store) closeIfPending(e *entry, resolution string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.state == statePending {
		s.closeLocked(e, resolution)
	}
}

// pick returns the option d selects, or nil for a deny with no deny option
// (answered with Escape) and for text. approve never picks approve_always:
// a one-tap answer must not grant a lasting permission.
func pick(a wire.Approval, d wire.Decision) (*wire.Option, error) {
	find := func(match func(wire.Option) bool) *wire.Option {
		for i := range a.Options {
			if match(a.Options[i]) {
				return &a.Options[i]
			}
		}
		return nil
	}
	switch d.Kind {
	case wire.DecisionApprove:
		if o := find(func(o wire.Option) bool { return o.Role == wire.RoleApprove }); o != nil {
			return o, nil
		}
		return nil, invalid("approval offers no approve option")
	case wire.DecisionDeny:
		return find(func(o wire.Option) bool { return o.Role == wire.RoleDeny }), nil
	case wire.DecisionOption:
		if o := find(func(o wire.Option) bool { return o.N == d.N }); o != nil {
			return o, nil
		}
		return nil, invalid(fmt.Sprintf("approval offers no option %d", d.N))
	}
	if !a.AllowText {
		return nil, invalid("approval does not accept text")
	}
	return nil, nil
}

// answerFor is what an ask receives: the chosen option's label, the text,
// or "deny" when the user declined an ask with no deny option.
func answerFor(a wire.Approval, d wire.Decision, opt *wire.Option) string {
	switch {
	case opt != nil:
		return opt.Label
	case d.Kind == wire.DecisionText:
		return d.Text
	}
	return wire.DecisionDeny
}

var errPromptGone = errors.New("approval: prompt no longer on screen")

// typeAnswer captures the pane, confirms the approval's prompt is still the
// one on screen, and sends the keys for the decision. The keys are resolved
// from this capture because arrow-menu moves depend on where the cursor is
// now. Callers hold the pane lock.
func (s *Store) typeAnswer(ctx context.Context, a wire.Approval, fingerprint string, d wire.Decision, opt *wire.Option) error {
	if fingerprint == "" {
		return errPromptGone
	}
	tgt := target(*a.SessionID)
	screen, err := s.cfg.Tmux.CapturePane(ctx, tgt)
	if err != nil {
		return fmt.Errorf("capture pane: %w", err)
	}
	if opt != nil {
		keys, ok := detect.KeysFor(screen, fingerprint, opt.Label)
		if !ok || len(keys) == 0 {
			return errPromptGone
		}
		return s.send(ctx, tgt, false, keys...)
	}
	if p, ok := detect.Detect(screen); !ok || p.Fingerprint != fingerprint {
		return errPromptGone
	}
	if d.Kind == wire.DecisionText {
		if err := s.send(ctx, tgt, true, d.Text); err != nil {
			return err
		}
		return s.send(ctx, tgt, false, "Enter")
	}
	return s.send(ctx, tgt, false, "Escape")
}

func (s *Store) send(ctx context.Context, tgt string, literal bool, keys ...string) error {
	if err := s.cfg.Tmux.SendKeys(ctx, tgt, literal, keys...); err != nil {
		return fmt.Errorf("send keys: %w", err)
	}
	return nil
}
