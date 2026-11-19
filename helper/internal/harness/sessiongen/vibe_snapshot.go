package sessiongen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ask writes the ask_user_question tool: an effect that runs while the
// dialog waits and settles with the answers (journal-ask.jsonl).
func (v *Vibe) ask(g *Gen, a *HookAsk, open bool) error {
	s := v.cur
	if a.Plan != "" {
		return fmt.Errorf("sessiongen: vibe has no plan approval record")
	}
	opts := make([]any, 0, len(a.Options))
	for _, o := range a.Options {
		opts = append(opts, map[string]any{"description": "Option " + o, "label": o})
	}
	in := map[string]any{"questions": []any{map[string]any{"options": opts, "question": a.Prompt}}}
	at := g.Tick()
	id := "effect-" + uuid(g)
	e := v.base("effect", id, at)
	e["generationStatus"], e["title"] = "in_progress", "ui.ask_user_question"
	e["detail"] = map[string]any{"display": map[string]any{"message": "ui.ask_user_question", "settledMessage": "ui.ask_user_question",
		"settledVerb": "Ran", "statusText": "Running ui.ask_user_question", "summary": "ui.ask_user_question", "verb": "Running"},
		"input": in, "kind": "tool", "toolName": "ui.ask_user_question"}
	e["state"] = map[string]any{"outputText": "", "status": "running"}
	s.steps++
	if err := v.delta(g, at, "in_progress", nil, v.add(e)); err != nil {
		return err
	}
	cb := "user-input-" + g.Hex(32) + "-" + g.Hex(8)
	at = g.Tick()
	routing := map[string]any{"callbackId": cb, "createdAt": vibeMS(at), "generationStatus": "in_progress", "id": cb, "inputEntryId": s.turnEntry,
		"relatedEntryId": id, "sessionId": s.id, "state": map[string]any{"status": "open"}, "title": "User input required", "turnId": s.turn,
		"type": "callback", "updatedAt": vibeMS(at), "detail": map[string]any{"kind": "user_input", "relatedEntryId": id, "request": in}}
	if err := v.record(g, "callback_registered", map[string]any{"callback_id": cb, "kind": "user_input", "routing": routing}, at); err != nil {
		return err
	}
	rest := func() error {
		answers := []any{}
		if !a.Cancel {
			answers = append(answers, map[string]any{"answer": a.Answer, "isOther": false, "question": a.Prompt})
		}
		result := map[string]any{"answers": answers, "cancelled": a.Cancel}
		at = g.Tick()
		if err := v.record(g, "callback_resolved", map[string]any{"callback_id": cb, "state": "resolved", "result": map[string]any{
			"callbackId": cb, "output": map[string]any{"result": result, "type": "user_input"}}}, at); err != nil {
			return err
		}
		cbe := v.base("callback", cb, at)
		cbe["callbackId"], cbe["relatedEntryId"], cbe["title"] = cb, id, "User input required"
		cbe["detail"] = routing["detail"]
		cbe["state"] = map[string]any{"output": map[string]any{"result": result, "type": "user_input"}, "status": "answered"}
		if err := v.delta(g, at, "in_progress", nil, v.add(cbe)); err != nil {
			return err
		}
		at = g.Tick()
		raw, _ := json.Marshal(map[string]any{"answers": answers, "cancelled": a.Cancel})
		n := map[string]any{}
		for k, val := range e {
			n[k] = val
		}
		n["generationStatus"], n["updatedAt"] = "completed", vibeMS(at)
		n["state"] = map[string]any{"approvalSource": "config", "approvalType": "always", "decision": "execute",
			"display": map[string]any{"message": "ui.ask_user_question", "success": true}, "outputText": string(raw), "status": "completed",
			"output": map[string]any{"_meta": map[string]any{"approval": map[string]any{"approvalSource": "config", "approvalType": "always", "decision": "execute"}},
				"content": []any{map[string]any{"text": string(raw), "type": "text"}}, "structured_content": result, "type": "success"}}
		return v.delta(g, at, "in_progress", nil, v.swap(n))
	}
	if open {
		s.finish = rest
		g.L.Truth.Last = id
		return nil
	}
	return rest()
}

// snapshot folds the finished turn into a new generation and starts the
// next segment, as Vibe does after each turn.
func (v *Vibe) snapshot(g *Gen, at time.Time, status string) error {
	s := v.cur
	s.history = append(s.history, s.open...)
	s.open = nil
	s.gen = s.seq
	if err := os.MkdirAll(filepath.Join(s.dir, "chunks"), 0o755); err != nil {
		return err
	}
	// Full chunks never change; only the last, partial one is written again.
	put := func(entries []map[string]any) (string, error) {
		raw, err := json.Marshal(entries)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(raw)
		name := hex.EncodeToString(sum[:])
		return name, os.WriteFile(filepath.Join(s.dir, "chunks", name+".json"), raw, 0o644)
	}
	for len(s.history)-64*len(s.sealed) >= 64 {
		name, err := put(s.history[64*len(s.sealed) : 64*len(s.sealed)+64])
		if err != nil {
			return err
		}
		s.sealed = append(s.sealed, name)
	}
	chunks := append([]string(nil), s.sealed...)
	if rest := s.history[64*len(s.sealed):]; len(rest) > 0 {
		name, err := put(rest)
		if err != nil {
			return err
		}
		chunks = append(chunks, name)
	}
	gen := fmt.Sprintf("%016d", s.gen)
	gdir := filepath.Join(s.dir, "generations", gen)
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		return err
	}
	var fail map[string]any
	if status == "failed" {
		fail = map[string]any{"code": "model_unauthorized", "message": "failed", "details": nil}
	}
	ps := map[string]any{"projection_state_version": 1, "session_id": s.id, "snapshot": v.envelope(g, status, at, fail),
		"snapshot_sequence": s.gen, "watermark": s.watermark}
	if err := writeJSON(filepath.Join(gdir, "projection-state.json"), ps); err != nil {
		return err
	}
	man := map[string]any{"created_at": at.UTC().Format("2006-01-02T15:04:05.000Z"), "execution_state": "quiescent", "generation": gen,
		"manifest_version": 1, "session_id": s.id, "snapshot_sequence": s.gen, "interop_export": nil,
		"projection_state":         map[string]any{"chunks": chunks, "path": "projection-state.json"},
		"recovery_journal_segment": map[string]any{"first_sequence": s.seq + 1, "path": fmt.Sprintf("journal/%016d.jsonl", s.seq+1)}}
	if err := writeJSON(filepath.Join(gdir, "manifest.json"), man); err != nil {
		return err
	}
	cur := map[string]any{"generation": gen, "session_id": s.id, "snapshot_sequence": s.gen, "store_format": "mistral.vibe.unified-session-store/v1", "store_format_minor": 10}
	if err := writeJSON(filepath.Join(s.dir, "CURRENT"), cur); err != nil {
		return err
	}
	return v.newSegment(g, s)
}

func writeJSON(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// Flushed keeps the last two segments of each session, and the snapshot
// generation and chunks the newest manifest names.
func (v *Vibe) Flushed(g *Gen) error {
	for _, s := range v.all {
		segs, _ := filepath.Glob(filepath.Join(s.dir, "journal", "*.jsonl"))
		sort.Strings(segs)
		for len(segs) > 2 {
			if err := os.Remove(segs[0]); err != nil {
				return err
			}
			segs = segs[1:]
		}
		// The agent writes its segments as the helper watches: dated now,
		// in order, so that the newest is the one written last. Only the
		// session written to: Vibe never touches a session it left.
		now := time.Now()
		for i, p := range segs {
			if s != v.cur {
				break
			}
			// A millisecond apart: a segment dated a second back would
			// look written before a feed that began to watch since.
			at := now.Add(-time.Duration(len(segs)-1-i) * time.Millisecond)
			if err := os.Chtimes(p, at, at); err != nil {
				return err
			}
		}
		gens, _ := filepath.Glob(filepath.Join(s.dir, "generations", "*"))
		sort.Strings(gens)
		for len(gens) > 1 {
			_ = os.RemoveAll(gens[0])
			gens = gens[1:]
		}
		if len(gens) == 1 {
			var man struct {
				P struct {
					Chunks []string `json:"chunks"`
				} `json:"projection_state"`
			}
			if raw, err := os.ReadFile(filepath.Join(gens[0], "manifest.json")); err == nil && json.Unmarshal(raw, &man) == nil {
				keep := map[string]bool{}
				for _, c := range man.P.Chunks {
					keep[c+".json"] = true
				}
				files, _ := filepath.Glob(filepath.Join(s.dir, "chunks", "*.json"))
				for _, f := range files {
					if !keep[filepath.Base(f)] && !strings.HasSuffix(f, ".tmp") {
						_ = os.Remove(f)
					}
				}
			}
		}
	}
	return nil
}
