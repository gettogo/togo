package approval

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/wire"
)

// arrowMenu is an approval menu moved with arrows and confirmed with Enter.
// Like omp 18.20's, it drops an Enter that arrives in the same batch as a
// move. While deaf it takes no Enter at all.
type arrowMenu struct {
	mu     sync.Mutex
	cursor int
	gone   bool
	deaf   bool
	chose  string
	sent   [][]string
}

var arrowOptions = []string{"Approve", "Approve bash for this session", "Deny"}

func (m *arrowMenu) ListSessions(context.Context) ([]tmux.SessionInfo, error) {
	return []tmux.SessionInfo{{Name: sess, Bib: "03", Agent: "omp"}}, nil
}
func (m *arrowMenu) PasteText(context.Context, string, string) error { return nil }

func (m *arrowMenu) CapturePane(context.Context, string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gone {
		return "Tool call " + m.chose + "\n> \n", nil
	}
	var b strings.Builder
	b.WriteString("Allow bash: printf OMP_DENY_FRESH?\n")
	for i, o := range arrowOptions {
		if i == m.cursor {
			b.WriteString("❯ " + o + "\n")
		} else {
			b.WriteString("  " + o + "\n")
		}
	}
	return b.String(), nil
}

func (m *arrowMenu) SendKeys(_ context.Context, _ string, _ bool, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, keys)
	moved := false
	for _, k := range keys {
		switch k {
		case "Down":
			m.cursor, moved = min(m.cursor+1, len(arrowOptions)-1), true
		case "Up":
			m.cursor, moved = max(m.cursor-1, 0), true
		case "Enter":
			if !moved && !m.deaf {
				m.gone, m.chose = true, arrowOptions[m.cursor]
			}
		}
	}
	return nil
}

func fastAnswer(t *testing.T) {
	t.Helper()
	s, c, p := answerSelect, answerConfirm, answerPoll
	answerSelect, answerConfirm, answerPoll = 300*time.Millisecond, 300*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { answerSelect, answerConfirm, answerPoll = s, c, p })
}

func openArrow(t *testing.T, m *arrowMenu) (*Store, string) {
	t.Helper()
	st := New(Config{Tmux: m, Pub: &fakePub{}})
	screen, _ := m.CapturePane(context.Background(), "")
	p, ok := detect.Detect(screen)
	if !ok {
		t.Fatalf("the menu was not detected:\n%s", screen)
	}
	st.PromptSeen(context.Background(), sess, p)
	pending := st.Pending(sess)
	if len(pending) != 1 {
		t.Fatalf("pending = %+v", pending)
	}
	return st, pending[0].RequestID
}

// Seen live on omp 18.20: Deny was answered from the phone, the helper
// said answered, and the menu still waited with Deny selected. The moves
// and the Enter now go separately, the Enter only once the cursor is on
// the option, and the answer is reported once the prompt has left.
func TestAnswerConfirmsOnlyAfterTheCursorArrives(t *testing.T) {
	fastAnswer(t)
	m := &arrowMenu{}
	st, id := openArrow(t, m)
	if err := st.Answer(context.Background(), id, "deny", wire.ByPhone); err != nil {
		t.Fatalf("answer: %v (keys %v)", err, m.sent)
	}
	if !m.gone || m.chose != "Deny" {
		t.Fatalf("menu gone %v chose %q, keys %v", m.gone, m.chose, m.sent)
	}
}

// A menu that does not take the answer leaves the request open and says
// so; the answer is not reported as delivered. Asked again once the menu
// listens, it is taken.
func TestAnswerThatDoesNotTakeStaysOpen(t *testing.T) {
	fastAnswer(t)
	m := &arrowMenu{deaf: true}
	st, id := openArrow(t, m)
	err := st.Answer(context.Background(), id, "deny", wire.ByPhone)
	var ce *api.CodeError
	if !errors.As(err, &ce) {
		t.Fatalf("answer = %v, want a refusal", err)
	}
	// Not a conflict: released apps read every -32006 on an answer as
	// "answered elsewhere" and drop the request.
	if d, ok := ce.Data.(map[string]string); !ok || ce.Code != rpc.CodeRefused || d["reason"] != wire.RefusedUnconfirmed {
		t.Fatalf("error = %d %+v, want refused/unconfirmed", ce.Code, ce.Data)
	}
	if p := st.Pending(sess); len(p) != 1 || p[0].RequestID != id {
		t.Fatalf("pending = %+v, want the request still open", p)
	}
	m.mu.Lock()
	m.deaf = false
	m.mu.Unlock()
	if err := st.Answer(context.Background(), id, "deny", wire.ByPhone); err != nil {
		t.Fatalf("second answer: %v", err)
	}
	if !m.gone || m.chose != "Deny" {
		t.Fatalf("menu gone %v chose %q", m.gone, m.chose)
	}
}
