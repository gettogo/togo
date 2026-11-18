package surface

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/procs"
	"github.com/gettogo/togo/helper/internal/session"
)

var errRuntimeChanged = errors.New("surface: original runtime is no longer available")

type runtimeInfo struct {
	config  CompanionConfig
	state   CompanionState
	birth   string
	control *sync.Mutex
}

func (r runtimeInfo) identity() CacheIdentity {
	return CacheIdentity{Generation: r.config.Generation, PID: r.state.PID, Birth: r.birth, Conversation: r.state.Conversation, Epoch: r.state.Epoch}
}

// A process can keep running while its conversation changes. App drafts and
// snapshots must bind to the conversation epoch as well as the launch.
func (r runtimeInfo) wireGeneration() string {
	raw, _ := json.Marshal(r.identity())
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:16])
}

func (r runtimeInfo) cacheDir() string {
	return filepath.Join(filepath.Dir(r.config.Path), "cache-"+strconv.FormatUint(r.state.Epoch, 10))
}

type runtimeBackend interface {
	resolve(context.Context, string) (runtimeInfo, error)
	observe(context.Context, string, runtimeInfo, func(), func([]byte) error) error
	control(context.Context, string, runtimeInfo, string, string) error
	send(context.Context, string, runtimeInfo, string, []byte) error
	dispatch(context.Context, string, runtimeInfo, string, []byte) error
	validate(context.Context, string, runtimeInfo) (CompanionState, error)
}

type paneBackend struct {
	sessions   *session.Service
	companions *Companions
	logf       func(string, ...any)
}

func (b *paneBackend) startup(ctx context.Context) error {
	return b.sessions.Tmux.ReleaseStaleSurfaces(ctx)
}

func (b *paneBackend) resolve(ctx context.Context, id string) (runtimeInfo, error) {
	sess, err := b.sessions.Resolve(ctx, id)
	if err != nil || !sess.Managed || sess.Agent != "omp" {
		return runtimeInfo{}, errRuntimeChanged
	}
	ctx, cancel := context.WithTimeout(ctx, CompanionWait)
	defer cancel()
	for {
		// The original process publishes only after it starts. A failed
		// replacement may leave a valid older mapping, which remains usable.
		config, err := b.companions.Current(id)
		if err == nil {
			state, err := b.companions.State(config)
			if err == nil && state.Available && state.Conversation != "" {
				birth, err := procs.Birth(state.PID)
				if err == nil {
					run := runtimeInfo{config: config, state: state, birth: birth, control: &sync.Mutex{}}
					if _, err := b.validate(ctx, id, run); err == nil {
						cleanup, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
						if err := b.companions.Prune(cleanup); err != nil && b.logf != nil {
							b.logf("surface: stale generation cleanup failed: %v", err)
						}
						cancel()
						if err := b.companions.pruneEpochs(ctx, config, state.Epoch); err != nil && b.logf != nil {
							b.logf("surface: old epoch cleanup failed: %v", err)
						}
						return run, nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return runtimeInfo{}, errRuntimeChanged
		case <-time.After(30 * time.Millisecond):
		}
	}
}

func (b *paneBackend) validate(ctx context.Context, id string, run runtimeInfo) (CompanionState, error) {
	state, err := b.companions.State(run.config)
	if err != nil || !state.Available || state.PID != run.state.PID || state.Pane != run.state.Pane ||
		state.Conversation != run.state.Conversation || state.Epoch != run.state.Epoch {
		return CompanionState{}, errRuntimeChanged
	}
	birth, err := procs.Birth(state.PID)
	if err != nil || birth != run.birth {
		return CompanionState{}, errRuntimeChanged
	}
	pane, err := b.sessions.Tmux.ActivePane(ctx, id)
	if err != nil || pane != state.Pane {
		return CompanionState{}, errRuntimeChanged
	}
	tree := (procs.Session{Panes: func(ctx context.Context) ([]int, error) {
		return b.sessions.Tmux.PanePIDs(ctx, id)
	}}).Tree(ctx)
	if !slices.Contains(tree, state.PID) {
		return CompanionState{}, errRuntimeChanged
	}
	return state, nil
}

func (b *paneBackend) observe(ctx context.Context, id string, run runtimeInfo, ready func(), output func([]byte) error) error {
	return b.sessions.Tmux.ObservePane(ctx, id, run.state.Pane, ready, output)
}

func (b *paneBackend) control(ctx context.Context, id string, run runtimeInfo, owner, mode string) error {
	run.control.Lock()
	defer run.control.Unlock()
	if mode == "ansi" {
		// Discovery belongs to this lease, not to the current OMP epoch.
		// Clear it even when process/conversation validation now fails.
		if err := b.sessions.Tmux.ReleaseSurface(ctx, id, owner); err != nil {
			return err
		}
	}
	state, err := b.validate(ctx, id, run)
	if err != nil {
		return err
	}
	if mode == "native" && state.Owner != "" && state.Owner != owner {
		state, err = b.companions.Command(ctx, run.config, state, "ansi", state.Owner)
		if err != nil {
			return err
		}
	}
	if mode == "renew" && (state.Owner != owner || !state.Native) {
		return errRuntimeChanged
	}
	if mode == "native" {
		if err := b.sessions.Tmux.ClaimSurface(ctx, id, owner); err != nil {
			return err
		}
	}
	if mode == "ansi" {
		owner = state.Owner
	}
	_, err = b.companions.Command(ctx, run.config, state, mode, owner)
	return err
}

func (b *paneBackend) send(ctx context.Context, id string, run runtimeInfo, owner string, data []byte) error {
	if len(data) > MaxAPCBytes+16 {
		return ErrLimit
	}
	m, err := wireMessage(data)
	if err != nil {
		return err
	}
	if m.Verb == 'e' {
		return b.dispatch(ctx, id, run, owner, data)
	}
	if m.Verb != 'r' || len(data) > 4096 {
		return ErrLimit
	}
	if _, err := b.validate(ctx, id, run); err != nil {
		return err
	}
	ready, err := b.sessions.Tmux.SurfaceReplyReady(ctx, run.state.Pane, owner)
	if err != nil {
		return err
	}
	if !ready {
		return errLease
	}
	return b.sessions.Tmux.SendKeys(ctx, run.state.Pane, true, string(data))
}

func (b *paneBackend) dispatch(ctx context.Context, id string, run runtimeInfo, owner string, data []byte) error {
	m, err := wireMessage(data)
	if err != nil || m.Verb != 'e' {
		return ErrMalformed
	}
	run.control.Lock()
	defer run.control.Unlock()
	state, err := b.validate(ctx, id, run)
	if err != nil {
		return err
	}
	if state.Owner != owner || !state.Native {
		return errLease
	}
	return b.companions.Dispatch(ctx, run.config, state, owner, m.Body)
}

// dispatchSend is dispatch for a send, with the program's receipt.
func (b *paneBackend) dispatchSend(ctx context.Context, id string, run runtimeInfo, owner string, data []byte) (bool, error) {
	m, err := wireMessage(data)
	if err != nil || m.Verb != 'e' {
		return false, ErrMalformed
	}
	run.control.Lock()
	defer run.control.Unlock()
	state, err := b.validate(ctx, id, run)
	if err != nil {
		return false, err
	}
	if state.Owner != owner || !state.Native {
		return false, errLease
	}
	return b.companions.DispatchSend(ctx, run.config, state, owner, m.Body)
}

func wireMessage(data []byte) (Message, error) {
	if len(data) > MaxAPCBytes+16 {
		return Message{}, ErrLimit
	}
	var parser Parser
	msgs, errs := parser.Feed(data, time.Now())
	if len(errs) != 0 || len(msgs) != 1 {
		return Message{}, ErrMalformed
	}
	return msgs[0], nil
}

var releasedWords = regexp.MustCompile(`^[a-z_]+(:[A-Za-z_~]+)?$`)

// released is why the program last left native rendering, for the log: the
// companion's own fixed words, or nothing if it says anything else.
func (b *paneBackend) released(run runtimeInfo) string {
	state, err := b.companions.State(run.config)
	if err != nil || len(state.Released) > 48 || !releasedWords.MatchString(state.Released) {
		return ""
	}
	return state.Released
}
