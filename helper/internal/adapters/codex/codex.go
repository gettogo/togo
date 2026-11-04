// Package codex translates Codex hook and notify payloads into Togo hook
// events. Hooks receive a JSON object on stdin; the legacy notify program
// receives the same kind of object as its last argument, which the caller
// passes here as a reader.
package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	agentName      = "codex"
	maxPayload     = 1 << 20
	maxMessageRune = 256
	maxCommandRune = 1024
	lookupTimeout  = 2 * time.Second
)

// ErrDropped means the payload was valid but cannot be attributed to exactly
// one managed session, so no event is produced.
var ErrDropped = errors.New("codex hook dropped: session not resolved")

// Candidate is a managed session the adapter may attribute an event to.
type Candidate struct {
	ID    string
	Agent string
	Cwd   string
}

// Adapter maps Codex payloads to events and remembers which managed session
// each Codex session id belongs to.
type Adapter struct {
	// Sessions lists managed sessions; it is only consulted when the hook
	// environment carries no TOGO_SESSION.
	Sessions func(ctx context.Context) ([]Candidate, error)
	// Logf receives one-line diagnostics. It never gets payload text.
	Logf func(format string, args ...any)

	mu   sync.Mutex
	seen map[string]string
}

type payload struct {
	Type                 string  `json:"type"`
	SessionID            string  `json:"session_id"`
	ThreadID             string  `json:"thread-id"`
	Cwd                  string  `json:"cwd"`
	Model                string  `json:"model"`
	ToolName             string  `json:"tool_name"`
	ToolInput            toolIn  `json:"tool_input"`
	LastAssistantMessage *string `json:"last_assistant_message"`
	NotifyLastMessage    *string `json:"last-assistant-message"`
}

// toolIn tolerates tool_input being any JSON value.
type toolIn struct{ Command string }

func (t *toolIn) UnmarshalJSON(b []byte) error {
	var o struct {
		Command json.RawMessage `json:"command"`
	}
	if json.Unmarshal(b, &o) != nil || len(o.Command) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(o.Command, &s) == nil {
		t.Command = s
		return nil
	}
	var argv []string
	if json.Unmarshal(o.Command, &argv) == nil {
		t.Command = strings.Join(argv, " ")
	}
	return nil
}

// Map converts one payload. event is the Codex hook name (UserPromptSubmit,
// PermissionRequest, Stop) or "notify" for the legacy notify argument.
func (a *Adapter) Map(event string, stdin io.Reader, env map[string]string) (wire.HookEvent, error) {
	name, ok := normalize(event)
	if !ok {
		return wire.HookEvent{}, fmt.Errorf("codex: unsupported event %q", truncate(event, 40))
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, maxPayload+1))
	if err != nil {
		return wire.HookEvent{}, fmt.Errorf("codex: read payload: %w", err)
	}
	if len(raw) > maxPayload {
		return wire.HookEvent{}, errors.New("codex: payload too large")
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return wire.HookEvent{}, errors.New("codex: malformed payload")
	}
	agentID := p.SessionID
	if agentID == "" {
		agentID = p.ThreadID
	}
	if name == "notify" && p.Type != "agent-turn-complete" {
		return wire.HookEvent{}, errors.New("codex: unsupported notify type")
	}
	if name == "notify" {
		name = "stop"
	}

	ev := wire.HookEvent{Agent: agentName, Event: name, Cwd: p.Cwd}
	ev.Data.Model = p.Model
	ev.Data.AgentSessionID = agentID
	switch name {
	case "permission":
		ev.Data.Tool = truncate(p.ToolName, 64)
		ev.Data.Command = truncate(p.ToolInput.Command, maxCommandRune)
	case "stop":
		msg := p.LastAssistantMessage
		if msg == nil {
			msg = p.NotifyLastMessage
		}
		if msg != nil {
			ev.Data.Message = truncate(*msg, maxMessageRune)
		}
	}

	id, err := a.resolve(env, agentID, p.Cwd)
	if err != nil {
		return wire.HookEvent{}, err
	}
	ev.SessionID = &id
	return ev, nil
}

func normalize(event string) (string, bool) {
	switch strings.ToLower(event) {
	case "userpromptsubmit", "prompt":
		return "prompt", true
	case "permissionrequest", "permission":
		return "permission", true
	case "stop":
		return "stop", true
	case "notify":
		return "notify", true
	}
	return "", false
}

func (a *Adapter) resolve(env map[string]string, agentID, cwd string) (string, error) {
	if id := env["TOGO_SESSION"]; id != "" {
		a.remember(agentID, id)
		return id, nil
	}
	if id := a.recall(agentID); id != "" {
		return id, nil
	}
	if a.Sessions == nil || cwd == "" {
		return "", a.drop("no session hint", cwd)
	}
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	cands, err := a.Sessions(ctx)
	if err != nil {
		return "", a.drop("session lookup failed", cwd)
	}
	var match []string
	for _, c := range cands {
		if c.Agent == agentName && c.Cwd == cwd {
			match = append(match, c.ID)
		}
	}
	if len(match) != 1 {
		return "", a.drop(fmt.Sprintf("%d sessions match cwd", len(match)), cwd)
	}
	a.remember(agentID, match[0])
	return match[0], nil
}

func (a *Adapter) drop(why, cwd string) error {
	if a.Logf != nil {
		sum := sha256.Sum256([]byte(cwd))
		a.Logf("codex hook dropped: %s (cwd %s)", why, hex.EncodeToString(sum[:])[:6])
	}
	return ErrDropped
}

func (a *Adapter) remember(agentID, id string) {
	if agentID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.seen == nil {
		a.seen = map[string]string{}
	}
	a.seen[agentID] = id
}

func (a *Adapter) recall(agentID string) string {
	if agentID == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.seen[agentID]
}

func truncate(s string, runes int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	if utf8.RuneCountInString(s) <= runes {
		return s
	}
	return string([]rune(s)[:runes])
}
