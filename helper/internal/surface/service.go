package surface

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
)

var errLease = errors.New("surface: renderer is no longer active; reopen the interface")
var errBusy = errors.New("surface: another renderer owns this interface")

// MaxRenderers bounds concurrent owning connections independently of how many
// surfaces one original program may describe. Keep the existing resource cap.
const MaxRenderers = 4

// Service owns native renderers over the existing authenticated RPC stream.
type Service struct {
	// BeforeOpen serializes native admission with typed input. Configure before Run.
	// The returned function releases the input gate after the reservation only.
	BeforeOpen func(string) func()

	mu      sync.Mutex
	runs    map[string]*renderer
	backend runtimeBackend
	logf    func(string, ...any)
	stopped bool
	startup sync.Once
}

type renderer struct {
	action           sync.Mutex
	mu               sync.Mutex
	lease            Lease
	owner            *rpc.Conn
	runtime          runtimeInfo
	cache            *Cache
	cancel           context.CancelFunc
	done             chan struct{}
	lastRead         time.Time
	closed           bool
	snapshot         []byte
	snapshotID       string
	snapshotRevision uint64
	acks             map[uint64]int64
}

// NewService constructs the original-runtime companion and surface transport.
func NewService(home string, sessions *session.Service, logf func(string, ...any)) *Service {
	return newService(&paneBackend{sessions: sessions, companions: &Companions{Home: home}, logf: logf}, logf)
}

func newService(backend runtimeBackend, logf func(string, ...any)) *Service {
	return &Service{runs: map[string]*renderer{}, backend: backend, logf: logf}
}

func randomID() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func validateOptions(o RenderOptions) error {
	if o.Cols < 1 || o.Cols > 1000 || len(o.Kinds) == 0 || len(o.Kinds) > 64 {
		return ErrLimit
	}
	seen := map[string]bool{}
	for _, k := range o.Kinds {
		if !validID(k) || len(k) > 64 || seen[k] {
			return ErrMalformed
		}
		seen[k] = true
	}
	if !seen["col"] {
		return ErrMalformed
	}
	return nil
}

// Open observes the pane before enabling native output. A request cancellation
// aborts opening; after success the connection and visible reads own the lease.
func (s *Service) Open(ctx context.Context, owner *rpc.Conn, id string, options RenderOptions) (Lease, error) {
	managed, validErr := session.ValidateID(id)
	if owner == nil || validErr != nil || !managed {
		return Lease{}, errLease
	}
	if err := validateOptions(options); err != nil {
		return Lease{}, err
	}
	s.start()
	options.Kinds = append([]string(nil), options.Kinds...)
	runctx, cancel := context.WithCancel(context.Background())
	r := &renderer{owner: owner, lease: Lease{ID: id, Token: randomID()}, cancel: cancel,
		done: make(chan struct{}), lastRead: time.Now(), acks: map[uint64]int64{}}
	if err := s.reserve(ctx, id, r); err != nil {
		cancel()
		return Lease{}, err
	}
	fail := func(err error) (Lease, error) {
		cancel()
		if r.cache != nil {
			r.cache.Close()
		}
		s.mu.Lock()
		delete(s.runs, id)
		s.mu.Unlock()
		close(r.done)
		return Lease{}, err
	}
	run, err := s.backend.resolve(ctx, id)
	if err != nil {
		return fail(err)
	}
	r.runtime, r.lease.Generation = run, run.wireGeneration()
	// What this program's companion can do beyond the protocol's own events.
	// And what this helper adds to the scene it serves: when each node was
	// born, and the results lately removed from the transcript.
	r.lease.Features = append(slices.Clone(run.state.Features), "births")
	r.cache, err = OpenCache(run.cacheDir(), run.identity(), options)
	if err != nil {
		return fail(err)
	}
	ready, observed := make(chan struct{}), make(chan error, 1)
	var once sync.Once
	go func() {
		stream := receiver{cache: r.cache, logf: s.logf, send: func(reply Message) error {
			wire, err := Encode(reply.Verb, reply.Body)
			if err != nil {
				return err
			}
			return s.backend.send(runctx, id, run, r.lease.Token, wire)
		}}
		observed <- s.backend.observe(runctx, id, run, func() { once.Do(func() { close(ready) }) }, stream.feed)
	}()
	select {
	case <-ready:
	case err := <-observed:
		if err == nil {
			err = errLease
		}
		return fail(err)
	case <-ctx.Done():
		cancel()
		<-observed
		return fail(ctx.Err())
	case <-runctx.Done():
		<-observed
		return fail(errLease)
	case <-time.After(CompanionWait):
		cancel()
		<-observed
		return fail(errLease)
	}
	if err = s.backend.control(ctx, id, run, r.lease.Token, "native"); err != nil {
		cancel()
		<-observed
		restore, stop := context.WithTimeout(context.Background(), CompanionWait)
		_ = s.backend.control(restore, id, run, r.lease.Token, "ansi")
		stop()
		return fail(err)
	}
	go s.watch(runctx, r, observed)
	return r.lease, nil
}

func (s *Service) watch(ctx context.Context, r *renderer, observed <-chan error) {
	ticker := time.NewTicker(RendererRenew)
	defer ticker.Stop()
	observerEnded := false
	for {
		select {
		case <-ctx.Done():
			goto end
		case <-r.owner.Done():
			goto end
		case err := <-observed:
			observerEnded = true
			if err != nil && !errors.Is(err, context.Canceled) && s.logf != nil {
				s.logf("surface observer stopped: %v", err)
			}
			goto end
		case <-ticker.C:
			r.mu.Lock()
			expired := time.Since(r.lastRead) >= RendererLease
			r.mu.Unlock()
			if expired {
				goto end
			}
			r.action.Lock()
			call, cancel := context.WithTimeout(ctx, CompanionWait)
			err := s.backend.control(call, r.lease.ID, r.runtime, r.lease.Token, "renew")
			cancel()
			r.action.Unlock()
			if err != nil {
				if s.logf != nil {
					why := ""
					if b, ok := s.backend.(interface{ released(runtimeInfo) string }); ok {
						why = b.released(r.runtime)
					}
					if why != "" {
						s.logf("surface lease renewal stopped: %v (the program left native rendering: %s)", err, why)
					} else {
						s.logf("surface lease renewal stopped: %v", err)
					}
				}
				goto end
			}
		}
	}
end:
	r.cancel()
	r.action.Lock()
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	restore, cancel := context.WithTimeout(context.Background(), CompanionWait)
	_ = s.backend.control(restore, r.lease.ID, r.runtime, r.lease.Token, "ansi")
	cancel()
	r.action.Unlock()
	if !observerEnded {
		<-observed
	}
	r.cache.Close()
	s.mu.Lock()
	if s.runs[r.lease.ID] == r {
		delete(s.runs, r.lease.ID)
	}
	s.mu.Unlock()
	close(r.done)
}

func (s *Service) lookup(owner *rpc.Conn, id, token string) (*renderer, error) {
	s.mu.Lock()
	r := s.runs[id]
	s.mu.Unlock()
	if r == nil || owner == nil || r.owner != owner || r.lease.Token != token {
		return nil, errLease
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil, errLease
	}
	return r, nil
}

// Close releases this connection's renderer and joins its observer.
func (s *Service) Close(ctx context.Context, owner *rpc.Conn, ref LeaseRef) error {
	r, err := s.lookup(owner, ref.ID, ref.Lease)
	if err != nil {
		return err
	}
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop releases all owned panes before returning.
func (s *Service) Stop() {
	s.mu.Lock()
	s.stopped = true
	runs := make([]*renderer, 0, len(s.runs))
	for _, r := range s.runs {
		runs = append(runs, r)
		r.cancel()
	}
	s.mu.Unlock()
	for _, r := range runs {
		<-r.done
	}
}

// Run binds daemon shutdown to renderer cleanup.
func (s *Service) Run(ctx context.Context) { s.start(); <-ctx.Done(); s.Stop() }

func (s *Service) start() {
	s.startup.Do(func() {
		if backend, ok := s.backend.(interface{ startup(context.Context) error }); ok {
			ctx, cancel := context.WithTimeout(context.Background(), CompanionWait)
			defer cancel()
			if err := backend.startup(ctx); err != nil && s.logf != nil {
				s.logf("surface: startup cleanup failed: %v", err)
			}
		}
	})
}
