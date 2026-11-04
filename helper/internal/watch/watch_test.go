package watch

import (
	"context"
	"errors"
	"maps"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tap"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	sid     = "s_abcdefghij"
	promptA = "Do you want to continue? (Y/n)\n"
	plain   = "$ ./deploy.sh staging\n"
)

type harness struct {
	w *Watcher

	mu       sync.Mutex
	now      time.Time
	screen   string
	capErr   error
	captures int
	sessions []session.Session
	attached []string
	statuses []string
	seen     []detect.Prompt
	gone     int
	events   []wire.NotifyEvent
	removed  []string
	// sessionsGone lists SessionGone calls.
	sessionsGone []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{now: time.Unix(1000, 0), screen: plain,
		sessions: []session.Session{{ID: sid, Name: "api", Managed: true}}}
	w := &Watcher{
		cfg:      Config{Status: h, Prompts: h, Pub: h},
		idle:     6 * time.Second,
		now:      func() time.Time { h.mu.Lock(); defer h.mu.Unlock(); return h.now },
		sessions: map[string]*entry{},
	}
	w.list = func(context.Context) ([]session.Session, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		return append([]session.Session(nil), h.sessions...), nil
	}
	w.attach = func(_ context.Context, id string) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.attached = append(h.attached, id)
		return nil
	}
	w.capture = func(context.Context, string) (string, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.captures++
		return h.screen, h.capErr
	}
	h.w = w
	return h
}

func (h *harness) SetStatus(id, status string, _ *string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.statuses = append(h.statuses, id+":"+status)
}

func (h *harness) PromptSeen(_ context.Context, _ string, p detect.Prompt) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen = append(h.seen, p)
}

func (h *harness) PromptGone(context.Context, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gone++
}

func (h *harness) SessionGone(_ context.Context, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessionsGone = append(h.sessionsGone, id)
}

func (h *harness) Publish(kind string, id *string, payload any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch kind {
	case wire.EventNotify:
		h.events = append(h.events, payload.(wire.NotifyEvent))
	case wire.EventSessionRemoved:
		h.removed = append(h.removed, *id)
	}
}

func (h *harness) advance(d time.Duration) {
	h.mu.Lock()
	h.now = h.now.Add(d)
	h.mu.Unlock()
}

func (h *harness) setScreen(s string) {
	h.mu.Lock()
	h.screen = s
	h.mu.Unlock()
}

func (h *harness) signal(t *testing.T, kind, title string) {
	t.Helper()
	if err := h.w.Signal(context.Background(), wire.Signal{SessionID: sid, Kind: kind, Title: title}); err != nil {
		t.Fatal(err)
	}
}

func TestOutputThenSilenceRunsDetectOnce(t *testing.T) {
	h := newHarness(t)
	h.signal(t, wire.SignalOutput, "")
	h.advance(5 * time.Second)
	h.w.Tick(context.Background())
	if h.captures != 0 {
		t.Fatalf("inspected before the idle window: %d captures", h.captures)
	}
	h.advance(2 * time.Second)
	h.w.Tick(context.Background())
	h.advance(10 * time.Second)
	h.w.Tick(context.Background())
	if h.captures != 1 {
		t.Fatalf("captures = %d, want 1", h.captures)
	}
	if got := h.statuses; len(got) != 2 || got[0] != sid+":working" || got[1] != sid+":idle" {
		t.Fatalf("statuses = %v", got)
	}
}

func TestSilenceWithPromptCallsPromptSeen(t *testing.T) {
	h := newHarness(t)
	h.setScreen(promptA)
	h.signal(t, wire.SignalOutput, "")
	h.advance(7 * time.Second)
	h.w.Tick(context.Background())
	if len(h.seen) != 1 || h.seen[0].Title == "" {
		t.Fatalf("PromptSeen calls = %+v", h.seen)
	}
	if len(h.events) != 0 {
		t.Fatalf("a pending prompt must not also publish done: %+v", h.events)
	}
}

func TestSilenceWithoutPromptPublishesDone(t *testing.T) {
	h := newHarness(t)
	h.signal(t, wire.SignalOutput, "")
	h.advance(7 * time.Second)
	h.w.Tick(context.Background())
	if len(h.events) != 1 || h.events[0].Level != wire.LevelDone || h.events[0].Title != "api" {
		t.Fatalf("events = %+v", h.events)
	}
	if len(h.seen) != 0 {
		t.Fatal("unexpected prompt")
	}
}

func TestBellPublishesNeedsYouNotify(t *testing.T) {
	h := newHarness(t)
	h.signal(t, wire.SignalBell, "")
	h.advance(time.Second)
	h.signal(t, wire.SignalOSC9, "Build finished")
	if len(h.events) != 2 {
		t.Fatalf("events = %+v", h.events)
	}
	if h.events[0].Level != wire.LevelNeedsYou || h.events[0].Title != "Bell" {
		t.Fatalf("bell event = %+v", h.events[0])
	}
	if h.events[1].Title != "Build finished" {
		t.Fatalf("osc event = %+v", h.events[1])
	}
}

func TestBellWithPromptReportsPromptInstead(t *testing.T) {
	h := newHarness(t)
	h.setScreen(promptA)
	h.signal(t, wire.SignalBell, "")
	if len(h.seen) != 1 || len(h.events) != 0 {
		t.Fatalf("seen=%d events=%+v", len(h.seen), h.events)
	}
}

func TestPromptGoneAfterDeskAnswer(t *testing.T) {
	h := newHarness(t)
	h.setScreen(promptA)
	h.signal(t, wire.SignalOutput, "")
	h.advance(7 * time.Second)
	h.w.Tick(context.Background())

	h.signal(t, wire.SignalOutput, "")
	if h.gone != 0 {
		t.Fatal("prompt still on screen was reported gone")
	}
	h.setScreen(plain)
	h.signal(t, wire.SignalOutput, "")
	if h.gone != 1 {
		t.Fatalf("PromptGone calls = %d, want 1", h.gone)
	}
	h.signal(t, wire.SignalOutput, "")
	if h.gone != 1 {
		t.Fatalf("PromptGone repeated: %d", h.gone)
	}
}

func TestReconcileAttachesNewAndRemovesGone(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.sessions = append(h.sessions, session.Session{ID: "my-own", Managed: false})
	h.w.Reconcile(ctx)
	h.w.Reconcile(ctx)
	if len(h.attached) != 1 || h.attached[0] != sid {
		t.Fatalf("attached = %v", h.attached)
	}
	h.sessions = nil
	h.w.Reconcile(ctx)
	if len(h.removed) != 1 || h.removed[0] != sid {
		t.Fatalf("removed = %v", h.removed)
	}
	h.w.Reconcile(ctx)
	if len(h.removed) != 1 {
		t.Fatalf("removal repeated: %v", h.removed)
	}
}

func TestVanishedSessionWithoutPromptReportedGone(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.w.Reconcile(ctx)
	h.sessions = nil
	h.w.Reconcile(ctx)
	if len(h.sessionsGone) != 1 || h.sessionsGone[0] != sid {
		t.Fatalf("SessionGone calls %v, want [%s]", h.sessionsGone, sid)
	}
}

func TestReconcileReattachesDeadTap(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	piped := map[string]bool{}
	h.w.piped = func(context.Context) (map[string]bool, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		return maps.Clone(piped), nil
	}
	h.w.Reconcile(ctx)
	h.mu.Lock()
	piped[sid] = true
	h.mu.Unlock()
	h.w.Reconcile(ctx)
	if len(h.attached) != 1 {
		t.Fatalf("live tap attached again: %v", h.attached)
	}
	h.mu.Lock()
	piped[sid] = false
	h.mu.Unlock()
	h.w.Reconcile(ctx)
	if len(h.attached) != 2 {
		t.Fatalf("dead tap not attached again: %v", h.attached)
	}
}

func TestNoIdleEventWithoutPriorOutput(t *testing.T) {
	h := newHarness(t)
	h.w.Reconcile(context.Background())
	h.advance(time.Minute)
	h.w.Tick(context.Background())
	if len(h.events) != 0 || len(h.statuses) != 0 {
		t.Fatalf("events=%v statuses=%v", h.events, h.statuses)
	}
}

func TestPromptAlreadyOnScreenAtAttachIsReported(t *testing.T) {
	h := newHarness(t)
	h.setScreen(promptA)
	ctx := context.Background()
	h.w.Reconcile(ctx)
	h.w.Tick(ctx)
	if len(h.seen) != 1 {
		t.Fatalf("prompts seen %d, want 1 from the screen at attach", len(h.seen))
	}
	if len(h.events) != 0 || len(h.statuses) != 0 {
		t.Fatalf("events=%v statuses=%v, want none", h.events, h.statuses)
	}
	h.advance(time.Minute)
	h.w.Tick(ctx)
	if h.captures != 1 {
		t.Fatalf("captures %d, want one check per attach", h.captures)
	}
}

func TestSignalRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.w.Signal(ctx, wire.Signal{SessionID: "../x", Kind: wire.SignalBell}); err == nil {
		t.Fatal("accepted a bad session id")
	}
	if err := h.w.Signal(ctx, wire.Signal{SessionID: sid, Kind: "nope"}); err == nil {
		t.Fatal("accepted an unknown kind")
	}
}

func TestAttachBakesHomeIntoPipeCommand(t *testing.T) {
	c := tmuxtest.New(t)
	home := "/tmp/it's home"
	if out, err := exec.Command(c.Bin, append(c.Socket, "new-session", "-d", "-s", sid, "sleep", "30")...).CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v %s", err, out)
	}
	w, err := New(Config{Tmux: c, Sessions: &session.Service{Tmux: c}, TogoPath: "/opt/to go/togo", Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.attachTap(context.Background(), sid); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(c.Bin, append(c.Socket, "display-message", "-p", "-t", "="+sid+":", "#{pane_pipe}")...).Output()
	if err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Fatalf("pane_pipe = %q, %v", out, err)
	}
	cmd, _ := tap.PipeCommand("/opt/to go/togo", sid)
	want := `TOGO_HOME='/tmp/it'\''s home' exec '/opt/to go/togo' tap ` + sid
	if got := pipeCommand(home, cmd); got != want {
		t.Fatalf("pipe command = %s", got)
	}
}

func TestBellLoopIsBounded(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 600; i++ {
		h.signal(t, wire.SignalBell, "")
		h.advance(10 * time.Millisecond)
	}
	// 6 seconds of bells.
	if h.captures > 13 || len(h.events) != 1 {
		t.Fatalf("captures=%d events=%d", h.captures, len(h.events))
	}
	h.advance(31 * time.Second)
	h.signal(t, wire.SignalBell, "")
	if len(h.events) != 2 {
		t.Fatalf("events after the gap = %d, want 2", len(h.events))
	}
	h.advance(time.Second)
	h.signal(t, wire.SignalOSC9, "Other")
	if len(h.events) != 3 {
		t.Fatalf("a changed title must notify: %d", len(h.events))
	}
}

func TestCaptureErrorKeepsPendingPrompt(t *testing.T) {
	h := newHarness(t)
	h.setScreen(promptA)
	h.signal(t, wire.SignalOutput, "")
	h.advance(7 * time.Second)
	h.w.Tick(context.Background())

	h.mu.Lock()
	h.capErr = errors.New("tmux gone")
	h.mu.Unlock()
	h.signal(t, wire.SignalOutput, "")
	h.advance(7 * time.Second)
	h.w.Tick(context.Background())
	if h.gone != 0 {
		t.Fatal("transient capture error closed the prompt")
	}

	h.mu.Lock()
	h.capErr = nil
	h.screen = plain
	h.mu.Unlock()
	h.advance(time.Second)
	h.w.Tick(context.Background())
	if h.gone != 1 {
		t.Fatalf("PromptGone after a clean capture = %d, want 1", h.gone)
	}
}
