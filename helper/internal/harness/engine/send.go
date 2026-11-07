package engine

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// MaxQueued is how many prompts a session holds while the agent works.
const MaxQueued = 8

// Send types a prompt now, or queues it while the agent works.
func (e *Engine) Send(ctx context.Context, req harness.SendRequest) (harness.SendResult, error) {
	s, err := e.served(ctx, req.Session)
	if err != nil {
		return harness.SendResult{}, err
	}
	images := harness.ImageRefs(req.Images)
	if act, ok := s.ad.Actions[actionCommand]; ok && len(req.Images) == 0 {
		if name, args, ok := slashCommand(req.Text); ok {
			// The harness API takes a / line as a command, not as prompt
			// text the model would read (OpenCode's /init).
			if err := e.runCommand(ctx, s, act, name, args); err != nil {
				return harness.SendResult{}, err
			}
			return e.userItem(s, req.Text, nil, false), nil
		}
	}
	if act, ok := s.ad.Actions["send"]; ok {
		// Through the harness API every path and the text go in the one request.
		text := harness.Together(req.Text, req.Images)
		raw, _ := json.Marshal(text)
		res, err := e.run(ctx, s, "send", act, map[string]any{"text": text, "value": json.RawMessage(raw), "images": req.Images})
		if err != nil {
			return harness.SendResult{}, err
		}
		if res.Result == harness.ResultFailed || res.Result == harness.ResultFallback {
			return harness.SendResult{}, &harness.FallbackError{Reason: "source", Detail: res.Message}
		}
		return e.userItem(s, req.Text, images, false), nil
	}
	if e.cfg.Typist == nil {
		return harness.SendResult{}, &harness.FallbackError{Reason: "source", Detail: "no typist"}
	}
	// Typing takes the session's typing lock, so a prompt never lands
	// between the key presses of a mode cycle or a typed command; while
	// prompts are queued a new one goes behind them.
	l := typingLock(s.id)
	l.Lock()
	defer l.Unlock()
	s.mu.Lock()
	working := s.stateLocked()["working"] == "true"
	if working || len(s.queue) > 0 {
		if req.Mode == harness.SendNow || len(s.queue) >= MaxQueued {
			s.mu.Unlock()
			return harness.SendResult{}, harness.ErrBusy
		}
		it := s.newUserItemLocked(req.Text, images, true)
		s.queue = append(s.queue, queued{item: it})
		s.publishLocked([]harness.Item{it}, nil)
		stored, _ := e.log.Get(s.id, it.ID)
		s.drainLocked()
		s.mu.Unlock()
		return harness.SendResult{Item: stored}, nil
	}
	s.mu.Unlock()
	if err := e.typePrompt(ctx, s, req.Text, req.Images); err != nil {
		return harness.SendResult{}, err
	}
	return e.userItem(s, req.Text, images, false), nil
}

// actionCommand is the action that runs a / line through the harness's
// API, when the adapter has one.
const actionCommand = "command"

// commandAccept is how long a command request may take to be refused.
// OpenCode answers POST /session/{id}/command only when the reply is
// done, so a request still open after this was accepted and runs on.
var commandAccept = 3 * time.Second

// runCommand runs the command action and returns once the harness refused
// it or has held it for commandAccept.
func (e *Engine) runCommand(ctx context.Context, s *sess, act spec.Action, name, args string) error {
	type outcome struct {
		res harness.ActionResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := e.run(context.WithoutCancel(ctx), s, actionCommand, act, map[string]any{"command": name, "arguments": args})
		done <- outcome{res, err}
	}()
	select {
	case o := <-done:
		if o.err != nil {
			return o.err
		}
		if o.res.Result == harness.ResultFailed || o.res.Result == harness.ResultFallback {
			return &harness.FallbackError{Reason: "source", Detail: o.res.Message}
		}
	case <-time.After(commandAccept):
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// slashCommand splits "/name rest" into the command name and its
// arguments. A lone "/" or a path such as "/usr/bin" is not a command.
func slashCommand(text string) (name, args string, ok bool) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "/") {
		return "", "", false
	}
	name, args, _ = strings.Cut(t[1:], " ")
	if name == "" || strings.ContainsAny(name, "/\n\t") {
		return "", "", false
	}
	return name, strings.TrimSpace(args), true
}

// typePrompt pastes a prompt and its image paths in the order the
// adapter's images.insert asks for, pressing Enter once at the end.
func (e *Engine) typePrompt(ctx context.Context, s *sess, text string, images []string) error {
	insert := harness.InsertTogether
	if im := s.ad.Images; im != nil {
		insert = im.Insert
	}
	steps := harness.Insertions(text, images, insert)
	for i, step := range steps {
		if i > 0 {
			// Let the input take one path before the next arrives.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(insertGap):
			}
		}
		if err := e.cfg.Typist.Type(ctx, s.id, step.Text, step.Submit, false); err != nil {
			return err
		}
	}
	return nil
}

// insertGap is the pause between two pastes of one prompt.
var insertGap = 150 * time.Millisecond

// userItem publishes a sent prompt and remembers it, so the agent's own
// record of it takes this item's place.
func (e *Engine) userItem(s *sess, text string, images []harness.ImageRef, queuedNow bool) harness.SendResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.newUserItemLocked(text, images, queuedNow)
	s.notePhoneLocked(it.ID, text, images)
	s.publishLocked([]harness.Item{it}, nil)
	stored, _ := e.log.Get(s.id, it.ID)
	return harness.SendResult{Item: stored}
}

func (s *sess) newUserItemLocked(text string, images []harness.ImageRef, queued bool) harness.Item {
	return harness.Item{ID: s.e.newID(), Kind: harness.KindUser, At: s.e.cfg.Now(),
		User: &harness.UserBody{Text: text, Queued: queued, Source: "phone", Images: images}}
}

// drainLocked starts typing the next queued prompt (see drainQueue).
func (s *sess) drainLocked() {
	if len(s.queue) == 0 || s.e.cfg.Typist == nil {
		return
	}
	go s.e.drainQueue(s)
}

// Interrupt runs the adapter's interrupt action.
func (e *Engine) Interrupt(ctx context.Context, id string) (harness.ActionResult, error) {
	s, err := e.served(ctx, id)
	if err != nil {
		return harness.ActionResult{}, err
	}
	act, ok := s.ad.Actions[harness.ActionInterrupt]
	if !ok {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	return e.run(ctx, s, harness.ActionInterrupt, act, map[string]any{})
}

// Complete lists slash commands or files for the composer.
func (e *Engine) Complete(ctx context.Context, req harness.CompleteRequest) (harness.CompleteResult, error) {
	s, err := e.served(ctx, req.Session)
	if err != nil {
		return harness.CompleteResult{}, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	var items []harness.Completion
	if req.Kind == harness.CompleteFile {
		items = completeFiles(ctx, s.env.Cwd, req.Query, limit)
	} else {
		items = e.completeCommands(ctx, s, req.Query, limit)
	}
	if items == nil {
		items = []harness.Completion{}
	}
	return harness.CompleteResult{Items: items}, nil
}

// maxWalk bounds how many entries a file completion visits.
const maxWalk = 20000

var skipDirs = map[string]bool{".git": true, "node_modules": true, ".build": true, "DerivedData": true, "vendor": true, ".venv": true, "target": true, "dist": true}

// completeFiles lists files under cwd whose path contains the query,
// files whose name matches first.
func completeFiles(ctx context.Context, cwd, query string, limit int) []harness.Completion {
	q := strings.ToLower(query)
	type hit struct {
		rel  string
		rank int
	}
	var hits []hit
	visited := 0
	_ = filepath.WalkDir(cwd, func(p string, d fs.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil {
			return filepath.SkipDir
		}
		visited++
		if visited > maxWalk {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if p != cwd && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, _ := filepath.Rel(cwd, p)
		lr := strings.ToLower(rel)
		switch {
		case strings.Contains(strings.ToLower(d.Name()), q):
			hits = append(hits, hit{rel, 0})
		case strings.Contains(lr, q):
			hits = append(hits, hit{rel, 1})
		}
		return nil
	})
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return len(hits[i].rel) < len(hits[j].rel)
	})
	var out []harness.Completion
	for _, h := range hits {
		if len(out) >= limit {
			break
		}
		dir := filepath.Dir(h.rel)
		if dir == "." {
			dir = ""
		}
		out = append(out, harness.Completion{Insert: "@" + h.rel + " ", Title: filepath.Base(h.rel), Detail: dir})
	}
	return out
}
