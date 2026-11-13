package push

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/approval"
	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/wire"
)

var update = flag.Bool("update", false, "rewrite protocol/fixtures/render from the detector captures")

const (
	renderDir     = "../../../protocol/fixtures/render"
	capturesGlob  = "../detect/testdata/*/*.txt"
	renderSession = "s_aaaaaaaaaa"
)

// renderFixture is one screen carried through the detector, the approval
// store and the Sender to the plaintext a device opens, with what the
// notification shows for it. The app's tests build the notification from
// plaintext and compare it with expect.
type renderFixture struct {
	Name      string             `json:"name"`
	Plaintext wire.PushPlaintext `json:"plaintext"`
	Expect    renderExpect       `json:"expect"`
}

type renderExpect struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Body     string `json:"body"`
	Category string `json:"category"`
}

type renderTmux struct {
	screen, agent string
}

func (f renderTmux) ListSessions(context.Context) ([]tmux.SessionInfo, error) {
	return []tmux.SessionInfo{{Name: renderSession, Bib: "03", Agent: f.agent}}, nil
}

func (f renderTmux) CapturePane(context.Context, string) (string, error) { return f.screen, nil }

func (renderTmux) SendKeys(context.Context, string, bool, ...string) error { return nil }
func (renderTmux) PasteText(context.Context, string, string) error         { return nil }

type openCapture struct{ open []wire.ApprovalOpenEvent }

func (c *openCapture) Publish(_ string, _ *string, payload any) {
	if o, ok := payload.(wire.ApprovalOpenEvent); ok {
		c.open = append(c.open, o)
	}
}

// renderCapture runs one capture to the needs_you plaintext. ok is false
// when the screen is not a prompt.
func renderCapture(t *testing.T, screen, agent string) (wire.PushPlaintext, bool) {
	t.Helper()
	p, ok := detect.Detect(screen)
	if !ok {
		return wire.PushPlaintext{}, false
	}
	now := time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)
	pub := &openCapture{}
	st := approval.New(approval.Config{
		Tmux: renderTmux{screen: screen, agent: agent}, Pub: pub,
		Clock: func() time.Time { return now }, TTL: time.Hour,
		NewID: func() string { return "r_render0001" },
	})
	st.PromptSeen(context.Background(), renderSession, p)
	if len(pub.open) != 1 {
		t.Fatalf("approval.open events = %d, want 1", len(pub.open))
	}
	s := &Sender{Hub: &fakeHub{}, Host: "studio", Now: func() time.Time { return now }}
	s.init()
	sid := renderSession
	m, ok := s.message(wire.Event{Kind: wire.EventApprovalOpen, SessionID: &sid, Payload: pub.open[0]}, sid, SessionInfo{})
	if !ok {
		t.Fatal("sender sent nothing for an open approval")
	}
	return m.Plaintext, true
}

// TestRenderFixtures keeps protocol/fixtures/render in step with the
// detector: every capture that is a prompt has a fixture equal to what the
// pipeline produces now, and nothing else is there. Run with -update after
// a deliberate change.
func TestRenderFixtures(t *testing.T) {
	captures, err := filepath.Glob(capturesGlob)
	if err != nil || len(captures) == 0 {
		t.Fatalf("captures: %v (%d)", err, len(captures))
	}
	want := map[string]bool{}
	for _, c := range captures {
		dir := filepath.Base(filepath.Dir(c))
		name := dir + "/" + strings.TrimSuffix(filepath.Base(c), ".txt")
		screen, err := os.ReadFile(c)
		if err != nil {
			t.Fatal(err)
		}
		agent := dir
		if agent == "generic" {
			agent = ""
		}
		pt, ok := renderCapture(t, string(screen), agent)
		if !ok {
			continue
		}
		file := strings.ReplaceAll(name, "/", "-") + ".json"
		want[file] = true
		fx := renderFixture{Name: name, Plaintext: pt, Expect: renderNotification(pt)}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(fx); err != nil {
			t.Fatal(err)
		}
		b := buf.Bytes()
		path := filepath.Join(renderDir, file)
		if *update {
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		cur, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(cur, b) {
			t.Errorf("%s is out of date (%v); run go test ./internal/push -run TestRenderFixtures -update", file, err)
		}
	}
	all, _ := filepath.Glob(filepath.Join(renderDir, "*.json"))
	for _, f := range all {
		if base := filepath.Base(f); base != "clean.json" && !want[base] {
			t.Errorf("%s has no capture behind it", base)
		}
	}
	if len(want) < 8 {
		t.Errorf("only %d render fixtures", len(want))
	}
}

func TestRenderedTextIsClean(t *testing.T) {
	all, _ := filepath.Glob(filepath.Join(renderDir, "*.json"))
	n := 0
	for _, f := range all {
		if filepath.Base(f) == "clean.json" {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var fx renderFixture
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		n++
		for _, s := range []string{fx.Expect.Title, fx.Expect.Subtitle, fx.Expect.Body} {
			if strings.ContainsFunc(s, func(r rune) bool { return r >= 0x2500 && r <= 0x259F || wire.IsInvisible(r) }) ||
				strings.ContainsAny(firstRune(s), "❯›▸>") {
				t.Errorf("%s: %q carries screen decoration", fx.Name, s)
			}
		}
		if fx.Expect.Subtitle == "" {
			t.Errorf("%s: empty subtitle", fx.Name)
		}
	}
	if n == 0 {
		t.Fatal("no render fixtures")
	}
}

func TestRealCapturesRenderNothing(t *testing.T) {
	reals, _ := filepath.Glob("../detect/testdata/*/real-*.txt")
	for _, c := range reals {
		raw, err := os.ReadFile(strings.TrimSuffix(c, ".txt") + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var e struct{ Match bool }
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		screen, _ := os.ReadFile(c)
		if _, ok := renderCapture(t, string(screen), "claude"); ok != e.Match {
			t.Errorf("%s: rendered %v, want %v", c, ok, e.Match)
		}
	}
}

// renderNotification mirrors the app's NotificationTextBuilder for a
// needs_you push on a device paired with one machine. The app's tests check
// the two agree through the fixtures.
func renderNotification(p wire.PushPlaintext) renderExpect {
	var head []string
	if p.Bib != nil {
		if b := oneLine(*p.Bib); b != "" {
			head = append(head, b)
		}
	}
	if p.Agent != nil {
		if a := agentDisplay(*p.Agent); a != "" {
			head = append(head, a)
		}
	}
	title := "Togo"
	if len(head) > 0 {
		title = strings.Join(head, " ")
	}
	if p.SessionName != nil {
		if n := oneLine(*p.SessionName); n != "" && (p.Agent == nil || n != agentDisplay(*p.Agent)) {
			title += " · " + n
		}
	}
	body := detect.CleanLine(p.Body)
	if s := p.Subject; s != nil && s.Validate() == nil {
		body = detect.CleanLine(s.Text)
		if u, err := url.Parse(s.Text); s.Kind == wire.SubjectURL && err == nil && u.Host != "" {
			body = detect.CleanLine(u.Host + u.Path)
		}
	}
	cat := category(p.Options, p.RequestID, p.AllowText)
	if cat == "togo.info" && body == "" {
		// No button can answer it: the body says where to.
		body = "Open to answer"
	}
	return renderExpect{Title: title, Subtitle: oneLine(p.Title), Body: body, Category: cat}
}

func oneLine(s string) string {
	first, _, _ := strings.Cut(detect.CleanLine(s), "\n")
	return first
}

func agentDisplay(agent string) string {
	// The app names every registry agent as the registry does (TogoPush
	// AgentName mirrors it).
	if a, ok := agents.Lookup(strings.ToLower(agent)); ok && a.Command != "" {
		return a.Name
	}
	a := oneLine(agent)
	if a == "" || strings.ToLower(a) == wire.AgentCustom {
		return ""
	}
	r, n := utf8.DecodeRuneInString(a)
	return string(unicode.ToUpper(r)) + a[n:]
}

func category(opts []wire.Option, requestID string, allowText bool) string {
	roles := []string{}
	for _, o := range opts {
		roles = append(roles, o.Role)
	}
	switch approve, deny := slices.Contains(roles, wire.RoleApprove), slices.Contains(roles, wire.RoleDeny); {
	case approve && deny && allowText:
		return "togo.request.reply"
	case approve && deny:
		return "togo.request"
	case deny && allowText:
		return "togo.request.deny.reply"
	case deny:
		return "togo.request.deny"
	case len(opts) > 0 && len(opts) <= 4 && !approve && !slices.Contains(roles, wire.RoleApproveAlways):
		// A single question's own options, one button each.
		return "togo.choice." + requestID
	}
	if allowText {
		return "togo.request.text"
	}
	return "togo.info"
}

func firstRune(s string) string {
	_, n := utf8.DecodeRuneInString(s)
	return s[:n]
}
