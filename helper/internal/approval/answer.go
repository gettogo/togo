package approval

import (
	"context"
	"errors"
	"fmt"
	"time"

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
		e.ask <- answerFor(e, d, opt)
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
		if errors.Is(err, errUnconfirmed) {
			// The request stays open: answering it again reads the screen
			// afresh, and the desk can still answer it.
			return &api.CodeError{Code: rpc.CodeRefused, Data: map[string]string{"reason": wire.RefusedUnconfirmed}}
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
func answerFor(e *entry, d wire.Decision, opt *wire.Option) string {
	switch {
	case opt != nil:
		if l, ok := e.labels[opt.N]; ok {
			return l
		}
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
	screen, err := s.firstCapture(ctx, tgt)
	if err != nil {
		return fmt.Errorf("capture pane: %w", err)
	}
	if opt != nil {
		keys, ok := detect.KeysFor(screen, fingerprint, opt.Label)
		if !ok || len(keys) == 0 {
			return errPromptGone
		}
		if n := len(keys); n > 1 && movesOnly(keys[:n-1]) {
			// Moves, then the key that confirms. Sent in one batch, a menu
			// can take the moves and drop the confirm (seen live on omp
			// 18.20: Deny stayed selected and the tool call kept waiting).
			// The moves go first; the confirm follows once the screen
			// shows the cursor on the option, and only then.
			if err := s.send(ctx, tgt, false, keys[:n-1]...); err != nil {
				return err
			}
			last, err := s.awaitSelected(ctx, tgt, fingerprint, opt.Label)
			if err != nil {
				return err
			}
			keys = last
		}
		if err := s.send(ctx, tgt, false, keys...); err != nil {
			return err
		}
		return s.awaitTaken(ctx, tgt, fingerprint)
	}
	p, ok := detect.Detect(screen)
	if !ok || p.Fingerprint != fingerprint {
		return errPromptGone
	}
	if d.Kind == wire.DecisionText && len(p.TextKeys) > 0 {
		// The option that opens the field first (Kimi's "Reject with
		// feedback"), then the words once the field shows.
		if err := s.send(ctx, tgt, false, p.TextKeys...); err != nil {
			return err
		}
		// Words typed before the field opens would land in the option
		// list, where a digit picks an option. No change on screen, or a
		// capture that fails, means nothing more is typed.
		if !s.awaitChange(ctx, tgt, screen) {
			return errUnconfirmed
		}
	}
	if d.Kind == wire.DecisionText {
		if a.SignIn != nil && a.SignIn.Secret {
			// A key or code goes in as one paste, never as typed keys, and
			// no error carries it: tmux reads it from a pipe.
			if err := s.cfg.Tmux.PasteText(ctx, tgt, d.Text); err != nil {
				return errors.New("approval: could not paste the secret into the session")
			}
			return s.send(ctx, tgt, false, "Enter")
		}
		if err := s.send(ctx, tgt, true, d.Text); err != nil {
			return err
		}
		if err := s.send(ctx, tgt, false, "Enter"); err != nil {
			return err
		}
		// Recorded as answered only once the prompt has left the screen.
		return s.awaitTaken(ctx, tgt, fingerprint)
	}
	if err := s.send(ctx, tgt, false, "Escape"); err != nil {
		return err
	}
	return s.awaitTaken(ctx, tgt, fingerprint)
}

// errUnconfirmed means the answer's keys were sent and the prompt did not
// leave the screen: nothing shows the program took the answer.
var errUnconfirmed = errors.New("approval: the prompt is still on screen after the answer")

// How long an answer waits for the cursor to reach its option, and for the
// prompt to leave once confirmed. A menu redraws within a frame or two; a
// program busy starting the approved tool can take a moment to clear it.
var (
	answerSelect  = 2 * time.Second
	answerConfirm = 3 * time.Second
	answerPoll    = 80 * time.Millisecond
	// answerFieldWait is how long a typed answer waits for its field.
	answerFieldWait = 2 * time.Second
)

// awaitSelected waits for the screen to show the cursor on the option
// labelled label, which is when one key confirms it, and returns that key.
// A prompt that left meanwhile was answered at the desk.
// movesOnly reports whether every key moves the cursor. Only then does the
// screen show the option selected before the confirm key: a prompt answered
// by a digit and Enter (Kimi) never puts the cursor on the option, so its
// keys go in one batch.
func movesOnly(keys []string) bool {
	for _, k := range keys {
		switch k {
		case "Up", "Down", "Left", "Right", "Tab", "BTab":
		default:
			return false
		}
	}
	return len(keys) > 0
}

func (s *Store) awaitSelected(ctx context.Context, tgt, fingerprint, label string) ([]string, error) {
	deadline := time.Now().Add(answerSelect)
	for {
		screen, err := s.cfg.Tmux.CapturePane(ctx, tgt)
		if err != nil {
			return nil, fmt.Errorf("capture pane: %w", err)
		}
		keys, ok := detect.KeysFor(screen, fingerprint, label)
		if !ok {
			return nil, errPromptGone
		}
		if len(keys) == 1 {
			return keys, nil
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			// The cursor never arrived. No confirm key was sent, so
			// nothing was chosen.
			return nil, errUnconfirmed
		}
		time.Sleep(answerPoll)
	}
}

// awaitTaken waits for the prompt to leave the screen (or become another
// prompt) after its answer was sent.
func (s *Store) awaitTaken(ctx context.Context, tgt, fingerprint string) error {
	deadline := time.Now().Add(answerConfirm)
	for {
		screen, err := s.cfg.Tmux.CapturePane(ctx, tgt)
		if err != nil {
			return fmt.Errorf("capture pane: %w", err)
		}
		if p, ok := detect.Detect(screen); !ok || p.Fingerprint != fingerprint {
			return nil
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			return errUnconfirmed
		}
		time.Sleep(answerPoll)
	}
}

func (s *Store) send(ctx context.Context, tgt string, literal bool, keys ...string) error {
	if err := s.cfg.Tmux.SendKeys(ctx, tgt, literal, keys...); err != nil {
		return fmt.Errorf("send keys: %w", err)
	}
	return nil
}

// awaitChange waits a moment for the pane to differ from before, as a
// field opens after the key that asked for it. It reports whether a
// capture differed; a failed capture or the deadline report false.
func (s *Store) awaitChange(ctx context.Context, tgt, before string) bool {
	deadline := time.Now().Add(answerFieldWait)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
		now, err := s.cfg.Tmux.CapturePane(ctx, tgt)
		if err != nil {
			return false
		}
		if now != before {
			return true
		}
	}
	return false
}

// captureRetries are the waits before reading the screen again after a read
// that failed. Nothing has been typed yet at that point, so reading again is
// safe, and an answer that came through the relay cannot be sent twice: one
// failed start of tmux must not lose it.
var captureRetries = []time.Duration{150 * time.Millisecond, 600 * time.Millisecond}

func (s *Store) firstCapture(ctx context.Context, tgt string) (string, error) {
	screen, err := s.cfg.Tmux.CapturePane(ctx, tgt)
	for _, wait := range captureRetries {
		if err == nil || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			return "", err
		case <-time.After(wait):
		}
		screen, err = s.cfg.Tmux.CapturePane(ctx, tgt)
	}
	return screen, err
}
