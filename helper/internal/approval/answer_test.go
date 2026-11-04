package approval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

// stubScript draws a Claude-style permission menu, then reads keys one byte
// at a time in raw mode, appending each to the log file and clearing the
// screen, the way an agent removes its prompt once it has an answer.
const stubScript = `log=$1
printf 'Bash command\n\n  make build\n\nDo you want to proceed?\n'
printf '\342\235\257 1. Yes\n  2. Yes, and don'"'"'t ask again for make commands\n  3. No\n'
stty raw -echo
while :; do
	k=$(dd bs=1 count=1 2>/dev/null)
	printf '%s' "$k" >> "$log"
	printf '\033[2J\033[Hanswered\r\n'
done
`

type event struct {
	kind    string
	payload any
}

type fakePub struct {
	mu     sync.Mutex
	events []event
}

func (p *fakePub) Publish(kind string, _ *string, payload any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event{kind, payload})
}

// closed returns the resolutions of every approval.closed event so far.
func (p *fakePub) closed() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, e := range p.events {
		if c, ok := e.payload.(wire.ApprovalClosedEvent); ok {
			out = append(out, c.Resolution)
		}
	}
	return out
}

type pane struct {
	tm  *tmux.Client
	id  string
	log string
}

// startStub runs the stub prompt in a new session on a private tmux label
// and waits until the detector sees its menu.
func startStub(t *testing.T, tm *tmux.Client, id string) (pane, detect.Prompt) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "stub.sh")
	if err := os.WriteFile(script, []byte(stubScript), 0o600); err != nil {
		t.Fatal(err)
	}
	p := pane{tm: tm, id: id, log: filepath.Join(dir, "keys.log")}
	err := tm.NewSession(context.Background(), tmux.NewSessionOpts{
		Name: id, Dir: dir, Cols: 100, Rows: 30,
		Command: []string{"/bin/sh", script, p.log},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		screen, err := tm.CapturePane(context.Background(), target(id))
		if err == nil {
			if pr, ok := detect.Detect(screen); ok {
				return p, pr
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("stub prompt never appeared")
	return pane{}, detect.Prompt{}
}

// keys returns what the stub has read so far.
func (p pane) keys(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(p.log)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(b)
}

// waitKeys waits for the stub to have read want, then holds for a moment
// to catch any key arriving after it.
func (p pane) waitKeys(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for p.keys(t) != want && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if got := p.keys(t); got != want {
		t.Fatalf("stub read %q, want %q", got, want)
	}
}

func newStore(t *testing.T, tm Tmux) (*Store, *fakePub) {
	t.Helper()
	pub := &fakePub{}
	return New(Config{Tmux: tm, Pub: pub}), pub
}

// openOnStub starts the stub and opens a screen approval for its prompt.
func openOnStub(t *testing.T) (*Store, *fakePub, pane, wire.Approval) {
	t.Helper()
	tm := tmuxtest.New(t)
	st, pub := newStore(t, tm)
	p, pr := startStub(t, tm, "s_aaaaaaaaaa")
	st.PromptSeen(context.Background(), p.id, pr)
	got := st.Pending(p.id)
	if len(got) != 1 {
		t.Fatalf("pending = %d approvals, want 1", len(got))
	}
	return st, pub, p, got[0]
}

func wantConflict(t *testing.T, err error, reason, by string) {
	t.Helper()
	var ce *api.CodeError
	if !errors.As(err, &ce) || ce.Code != rpc.CodeConflict {
		t.Fatalf("err = %v, want -32006", err)
	}
	if c := ce.Data.(Conflict); c.Reason != reason || c.By != by {
		t.Fatalf("conflict = %+v, want reason %q by %q", c, reason, by)
	}
}

func wantInvalid(t *testing.T, err error) {
	t.Helper()
	var ie *session.InvalidParamsError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want invalid params", err)
	}
}

func TestAnswerApproveTypesOneTimeOption(t *testing.T) {
	st, pub, p, a := openOnStub(t)
	if err := st.Answer(context.Background(), a.RequestID, "approve", wire.ByPhone); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	p.waitKeys(t, "1")
	if got := pub.closed(); len(got) != 1 || got[0] != wire.ResolutionPhone {
		t.Fatalf("closed events = %v, want [phone]", got)
	}
	if n := len(st.Pending("")); n != 0 {
		t.Fatalf("pending after answer = %d", n)
	}
}

func TestAnswerDenyTypesDenyOption(t *testing.T) {
	st, _, p, a := openOnStub(t)
	if err := st.Answer(context.Background(), a.RequestID, "deny", wire.ByPhone); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	p.waitKeys(t, "3")
}

func TestAnswerAfterDeskAnswerSendsNoKeys(t *testing.T) {
	st, pub, p, a := openOnStub(t)
	if err := p.tm.SendKeys(context.Background(), target(p.id), false, "1"); err != nil {
		t.Fatal(err)
	}
	p.waitKeys(t, "1")
	err := st.Answer(context.Background(), a.RequestID, "deny", wire.ByPhone)
	wantConflict(t, err, wire.ConflictPromptGone, "")
	p.waitKeys(t, "1")
	if got := pub.closed(); len(got) != 1 || got[0] != wire.ResolutionGone {
		t.Fatalf("closed events = %v, want [gone]", got)
	}
}

func TestPromptGoneFromWatchClosesAsDesk(t *testing.T) {
	st, pub, p, a := openOnStub(t)
	st.PromptGone(context.Background(), p.id)
	err := st.Answer(context.Background(), a.RequestID, "approve", wire.ByPhone)
	wantConflict(t, err, wire.ConflictAnswered, wire.ByDesk)
	p.waitKeys(t, "")
	if got := pub.closed(); len(got) != 1 || got[0] != wire.ResolutionDesk {
		t.Fatalf("closed events = %v, want [desk]", got)
	}
}

func TestSecondAnswerConflicts(t *testing.T) {
	st, _, p, a := openOnStub(t)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, d := range []string{"approve", "deny"} {
		wg.Go(func() { errs[i] = st.Answer(context.Background(), a.RequestID, d, wire.ByPhone) })
	}
	wg.Wait()
	var winner int
	switch {
	case errs[0] == nil && errs[1] != nil:
		winner = 0
	case errs[1] == nil && errs[0] != nil:
		winner = 1
	default:
		t.Fatalf("errs = %v, want exactly one success", errs)
	}
	wantConflict(t, errs[1-winner], wire.ConflictAnswered, wire.ByPhone)
	p.waitKeys(t, []string{"1", "3"}[winner])
}

// TestDeskRacingPhoneTypesNoPhoneKeyWhenRefused types a desk key while the
// phone answers. Either may win, but a refused phone answer must have typed
// nothing, and the phone key reaches the pane at most once.
func TestDeskRacingPhoneTypesNoPhoneKeyWhenRefused(t *testing.T) {
	for range 3 {
		st, _, p, a := openOnStub(t)
		var wg sync.WaitGroup
		wg.Go(func() { _ = p.tm.SendKeys(context.Background(), target(p.id), false, "3") })
		err := st.Answer(context.Background(), a.RequestID, "approve", wire.ByPhone)
		wg.Wait()
		time.Sleep(300 * time.Millisecond)
		got := p.keys(t)
		if strings.Count(got, "1") > 1 || (err != nil && strings.Contains(got, "1")) {
			t.Fatalf("answer err %v, stub read %q", err, got)
		}
	}
}

func TestTextAnswerRequiresAllowText(t *testing.T) {
	st, _, p, a := openOnStub(t)
	wantInvalid(t, st.Answer(context.Background(), a.RequestID, "text:go ahead", wire.ByPhone))
	p.waitKeys(t, "")
	if n := len(st.Pending("")); n != 1 {
		t.Fatalf("pending after refused text = %d, want 1", n)
	}
}

func TestOptionOutOfRangeIsInvalidParams(t *testing.T) {
	st, _, p, a := openOnStub(t)
	wantInvalid(t, st.Answer(context.Background(), a.RequestID, "option:7", wire.ByPhone))
	wantInvalid(t, st.Answer(context.Background(), a.RequestID, "option:0", wire.ByPhone))
	p.waitKeys(t, "")
}

func TestUnknownRequestIsNotFound(t *testing.T) {
	st, _ := newStore(t, nil)
	if err := st.Answer(context.Background(), "r_zzzzzzzzzz", "approve", wire.ByPhone); !errors.Is(err, ErrUnknown) {
		t.Fatalf("err = %v, want ErrUnknown", err)
	}
}

func TestAnswerApprovesWithoutTypingAlwaysOption(t *testing.T) {
	a := wire.Approval{Options: []wire.Option{
		{N: 1, Label: "Yes, always", Role: wire.RoleApproveAlways},
		{N: 2, Label: "Yes", Role: wire.RoleApprove},
	}}
	o, err := pick(a, wire.Decision{Kind: wire.DecisionApprove})
	if err != nil || o.N != 2 {
		t.Fatalf("pick approve = %+v, %v; want option 2", o, err)
	}
	a.Options = a.Options[:1]
	if _, err := pick(a, wire.Decision{Kind: wire.DecisionApprove}); err == nil {
		t.Fatal("approve with only an always option was accepted")
	}
}
