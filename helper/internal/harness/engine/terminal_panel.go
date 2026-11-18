package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/termmenu"
)

// A harness may show a panel that only tells something: where to sign in
// and the code to enter there. The phone shows its lines as they are drawn.
// Nothing is worked out of them, and no key is offered: the keys such a
// panel names act on the machine the harness runs on. An address that
// stands whole on its line is offered as a link, which the phone opens
// itself.

const (
	panelScreenPrefix = "terminal.panel/"
	openLinkAction    = "terminal.openLink."
)

func (e *Engine) readPanel(ctx context.Context, s *sess) (termmenu.Panel, bool) {
	screen, ok := e.settled(ctx, s)
	if !ok {
		return termmenu.Panel{}, false
	}
	return termmenu.ReadPanel(screen)
}

// openPanel holds the panel for the caller and returns its screen.
func (e *Engine) openPanel(ctx context.Context, s *sess, title string, p termmenu.Panel) harness.ActionResult {
	m := &terminalMenu{client: e.client(ctx), sess: s, conv: menuConv(ctx, s), token: newToken(), title: title,
		panel: p.Title, info: true}
	e.setMenu(s.id, m)
	return harness.ActionResult{Result: harness.ResultApplied, Screen: e.panelScreen(m, p)}
}

func (e *Engine) panelScreen(m *terminalMenu, p termmenu.Panel) *harness.Screen {
	session := ""
	if m.sess != nil {
		session = m.sess.id
	}
	token, _ := json.Marshal(m.token)
	actions := []harness.ActionRef{}
	for i, link := range p.Links {
		// A link that holds something entered earlier is not offered.
		if e.redact(session, link) != link {
			continue
		}
		role := "normal"
		if len(actions) == 0 {
			role = "primary"
		}
		url, _ := json.Marshal(link)
		actions = append(actions, harness.ActionRef{ID: openLinkAction + strconv.Itoa(i), Label: "Open link", Role: role,
			Args: map[string]json.RawMessage{"interaction": token, "url": url}})
	}
	return &harness.Screen{
		ID: panelScreenPrefix + m.token, Primitive: harness.PrimDetail, Title: e.redact(session, p.Title), FetchedAt: e.cfg.Now(),
		Interaction: m.token, Actions: []harness.ActionRef{},
		Detail: &harness.Detail{Body: harness.Body{Kind: harness.BodyText, Text: e.redact(session, strings.Join(p.Lines, "\n"))}, Actions: actions},
	}
}

// closePanel presses Escape on the panel this was, if it is still shown.
func (e *Engine) closePanel(ctx context.Context, s *sess, m *terminalMenu) (closed bool, err error) {
	if p, is := e.readPanel(ctx, s); !is || p.Title != m.panel {
		return true, nil
	}
	if err := e.menuKeys(ctx, s, "Escape"); err != nil {
		return false, err
	}
	for deadline := time.Now().Add(e.scaled(2 * time.Second)); time.Now().Before(deadline); {
		if after, still := e.readPanel(ctx, s); !still || after.Title != m.panel {
			return true, nil
		}
		time.Sleep(e.scaled(80 * time.Millisecond))
	}
	return false, nil
}
