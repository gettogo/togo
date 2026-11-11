// Package pisession maps the session JSONL (version 3) that Pi writes and
// that omp, its fork, also writes: a header line, then entries linked by
// id and parentId (messages, model and thinking level changes,
// compactions). Each adapter registers the mapper under its own name.
package pisession

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	x "github.com/gettogo/togo/helper/internal/harness/adapters/internal/itemtext"
)

// Session maps one session file stream.
type Session struct {
	name string
	env  harness.Env

	mu    sync.Mutex
	state map[string]string
	turn  *turn
	tools map[string]harness.Item
	// flagsAt is when a relaunch set effort from its flags; a thinking
	// level the file recorded before then is older than that.
	flagsAt time.Time
}

type turn struct {
	id      string
	started time.Time
	steps   int
	files   map[string]bool
}

// New returns a mapper reporting name as its Name.
func New(name string, env harness.Env) *Session {
	return &Session{name: name, env: env, state: map[string]string{"working": "false"}, tools: map[string]harness.Item{}}
}

func (s *Session) Name() string { return s.name }

// State reports harness_session (the header id), session_title, model
// ("provider/id"), effort (the thinking level), turn and working.
func (s *Session) State() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.state))
	for k, v := range s.state {
		out[k] = v
	}
	return out
}

type entry struct {
	Type          string          `json:"type"`
	ID            string          `json:"id"`
	Cwd           string          `json:"cwd"`
	Timestamp     string          `json:"timestamp"`
	Message       json.RawMessage `json:"message"`
	Provider      string          `json:"provider"`
	ModelID       string          `json:"modelId"`
	ThinkingLevel string          `json:"thinkingLevel"`
	TokensBefore  int             `json:"tokensBefore"`
	Name          string          `json:"name"`
}

func (s *Session) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var e entry
	if json.Unmarshal(rec.Data, &e) != nil || e.Type == "" {
		return nil, nil
	}
	at := rec.At
	if t, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
		at = t
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e.Type {
	case "session":
		if s.env.Cwd != "" && e.Cwd != "" && e.Cwd != s.env.Cwd {
			return nil, nil
		}
		items, ops := s.endTurn(harness.StateInterrupted, at)
		s.state["harness_session"] = e.ID
		return items, ops
	case "message":
		return s.message(e.ID, e.Message, at)
	case "model_change":
		return s.setModel(e.Provider, e.ModelID, e.ID, at)
	case "thinking_level_change":
		if at.Before(s.flagsAt) {
			return nil, nil
		}
		if e.ThinkingLevel != "" && s.state["effort"] != e.ThinkingLevel {
			s.state["effort"] = e.ThinkingLevel
			return nil, []harness.Op{s.statusOp()}
		}
	case "compaction":
		text := "Context compacted"
		if e.TokensBefore > 0 {
			text += " from " + strconv.Itoa(e.TokensBefore) + " tokens"
		}
		return []harness.Item{{ID: e.ID, TurnID: s.turnID(), Kind: harness.KindNotice, At: at,
			Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeCompacted, Text: text}}}, nil
	case "session_info":
		s.state["session_title"] = x.Title(e.Name)
	}
	return nil, nil
}

// Relaunched takes the thinking level and model a relaunch gave Pi. Pi
// writes no thinking_level_change when --thinking resumes a session that
// already has one (setThinkingLevel finds the level unchanged), so its
// file would keep reporting the old level; a later change in the TUI is
// recorded and wins. The model is corrected by the next reply's record.
func (s *Session) Relaunched(argv []string, at time.Time) []harness.Op {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for i := 1; i+1 < len(argv); i++ {
		switch v := argv[i+1]; argv[i] {
		case "--thinking":
			if v != "" && s.state["effort"] != v {
				s.state["effort"], changed = v, true
			}
			s.flagsAt = at.UTC()
		case "--model":
			if v != "" && s.state["model"] != v {
				s.state["model"], changed = v, true
			}
		}
	}
	if !changed {
		return nil
	}
	return []harness.Op{s.statusOp()}
}

func (s *Session) setModel(provider, model, id string, at time.Time) ([]harness.Item, []harness.Op) {
	if model == "" {
		return nil, nil
	}
	ref := model
	if provider != "" {
		ref = provider + "/" + model
	}
	if s.state["model"] == ref {
		return nil, nil
	}
	first := s.state["model"] == ""
	s.state["model"] = ref
	ops := []harness.Op{s.statusOp()}
	if first || id == "" {
		return nil, ops
	}
	return []harness.Item{{ID: id, TurnID: s.turnID(), Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeModel, Text: "Model: " + ref}}}, ops
}

func (s *Session) statusOp() harness.Op {
	return harness.Op{Op: harness.OpStatus, Status: &harness.StatusBlock{Model: s.state["model"], Effort: s.state["effort"]}}
}

func (s *Session) turnID() string {
	if s.turn == nil {
		return ""
	}
	return s.turn.id
}

func (s *Session) turnItem(state string, ended *time.Time) harness.Item {
	t := s.turn
	return harness.Item{ID: t.id, TurnID: t.id, Kind: harness.KindTurn, At: t.started,
		Turn: &harness.TurnBody{State: state, Started: t.started, Ended: ended, Steps: t.steps, FilesChanged: len(t.files)}}
}

func (s *Session) startTurn(id string, at time.Time) []harness.Item {
	items, _ := s.endTurn(harness.StateInterrupted, at)
	s.turn = &turn{id: "turn-" + id, started: at, files: map[string]bool{}}
	s.state["turn"], s.state["working"] = s.turn.id, "true"
	return append(items, s.turnItem(harness.StateRunning, nil))
}

func (s *Session) endTurn(state string, at time.Time) ([]harness.Item, []harness.Op) {
	s.state["working"] = "false"
	if s.turn == nil {
		return nil, nil
	}
	var items []harness.Item
	for id, t := range s.tools {
		tb := *t.Tool
		tb.State, tb.Ended = harness.StateInterrupted, x.Ptr(at)
		t.Tool = &tb
		items = append(items, t)
		delete(s.tools, id)
	}
	items = append(items, s.turnItem(state, x.Ptr(at)))
	s.turn = nil
	return items, nil
}
