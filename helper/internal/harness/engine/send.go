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
	// The prompt belongs to the conversation the chat showed when it was
	// asked for. Waiting for the typing lock and reading the screen take
	// time; if the agent moves to another conversation meanwhile, the
	// prompt is not sent to it.
	s.mu.Lock()
	gen := s.convGen
	s.mu.Unlock()
	if act, ok := s.ad.Actions[actionCommand]; ok && len(req.Images) == 0 {
		// A command of the terminal's own is not the API's: it is typed below.
		if name, args, ok := slashCommand(req.Text); ok && !s.ad.TypedCommand(name) {
			// The harness API takes a / line as a command, not as prompt
			// text the model would read (OpenCode's /init).
			err := e.runCommand(ctx, s, act, name, args)
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.convGen != gen {
				return harness.SendResult{}, errMoved
			}
			if err != nil {
				return harness.SendResult{}, err
			}
			return s.userItemLocked(req.Text, nil, false), nil
		}
	}
	api, viaAPI := e.sendAction(s)
	if !viaAPI && e.cfg.Typist == nil {
		return harness.SendResult{}, &harness.FallbackError{Reason: "source", Detail: "no typist"}
	}
	// Sending takes the session's typing lock, so a prompt never lands
	// between the key presses of a mode cycle or a typed command; while
	// prompts are queued a new one goes behind them. The rule is the same
	// whichever way the prompt travels: a prompt sent through the harness
	// API while a turn runs would be taken as steering (OpenCode) or start
	// at once (Kilo), which is neither "now" nor "queue".
	l := typingLock(s.id)
	l.Lock()
	defer l.Unlock()
	blocked := e.asking(ctx, s, viaAPI)
	s.mu.Lock()
	if s.convGen != gen {
		s.mu.Unlock()
		return harness.SendResult{}, errNotSent
	}
	working := s.stateLocked()["working"] == "true"
	if working || len(s.queue) > 0 || blocked != "" {
		if req.Mode == harness.SendNow && blocked != "" {
			// Asked to go now: say why it did not, so the phone shows it.
			s.mu.Unlock()
			return harness.SendResult{}, refuseBlocked(blocked)
		}
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
	if viaAPI {
		err = e.sendAPI(ctx, s, api, req.Text, req.Images)
	} else {
		err = e.typePrompt(ctx, s, req.Text, req.Images)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.convGen != gen {
		// The chat started over while the prompt travelled: it is not
		// shown as sent in a conversation it may not be part of.
		return harness.SendResult{}, errMoved
	}
	if err != nil {
		return harness.SendResult{}, err
	}
	return s.userItemLocked(req.Text, images, false), nil
}

// sendAction is the adapter's API send action, when prompts go that way
// now: it has one and the harness session it names exists.
func (e *Engine) sendAction(s *sess) (spec.Action, bool) {
	act, ok := s.ad.Actions["send"]
	return act, ok && !e.sendNotYet(s, act)
}

// sendAPI sends a prompt through the harness API: every path and the text
// go in the one request.
func (e *Engine) sendAPI(ctx context.Context, s *sess, act spec.Action, text string, images []string) error {
	together := harness.Together(text, images)
	raw, _ := json.Marshal(together)
	res, err := e.run(ctx, s, "send", act, map[string]any{"text": together, "value": json.RawMessage(raw), "images": images})
	if err != nil {
		return err
	}
	if res.Result == harness.ResultFailed || res.Result == harness.ResultFallback {
		return &harness.FallbackError{Reason: "source", Detail: res.Message}
	}
	return nil
}

// asking is what holds a prompt back besides a running turn: a question
// the agent waits on. An API harness reports its questions itself, so its
// screen is not read for one.
func (e *Engine) asking(ctx context.Context, s *sess, viaAPI bool) string {
	// A question the harness reported itself (a form, a permission) holds
	// a prompt whichever way it travels: typed, it would land in the dialog.
	s.mu.Lock()
	open := len(s.needs.open) > 0
	s.mu.Unlock()
	if open {
		return askingText(s, "")
	}
	if !viaAPI {
		if err := e.heldNatively(s); err != nil {
			return err.Detail
		}
		return e.blockedBy(ctx, s)
	}
	if e.cfg.Approvals != nil {
		if p := e.cfg.Approvals.Pending(s.id); len(p) > 0 {
			return askingText(s, p[0].Title)
		}
	}
	return ""
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
	if err := e.heldNatively(s); err != nil {
		return err
	}
	e.awaitReady(ctx, s)
	if err := e.screenClear(ctx, s); err != nil {
		return err
	}
	if err := e.clearInput(ctx, s); err != nil {
		return err
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
		if step.Submit {
			if err := e.confirmSubmit(ctx, s, step.Text); err != nil {
				return err
			}
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
	return s.userItemLocked(text, images, queuedNow)
}

func (s *sess) userItemLocked(text string, images []harness.ImageRef, queuedNow bool) harness.SendResult {
	e := s.e
	it := s.newUserItemLocked(text, images, queuedNow)
	s.notePhoneLocked(it.ID, text, images)
	if !queuedNow {
		s.awaitReply, s.awaitText = true, text
		s.failedHold = false // a new prompt starts a new turn
	}
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
	if len(s.queue) == 0 {
		return
	}
	if _, api := s.ad.Actions["send"]; !api && s.e.cfg.Typist == nil {
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
	s.mu.Lock()
	s.awaitReply = false // a turn stopped on purpose owes no reply
	s.mu.Unlock()
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
		limit = harness.MaxLimit
		if req.Kind == harness.CompleteFile {
			limit = 50
		}
	}
	var items []harness.Completion
	if req.Kind == harness.CompleteFile {
		items = completeFiles(ctx, s.filesDir(ctx), req.Query, limit)
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

// junkFiles are files the OS leaves behind; nobody mentions them to an agent.
var junkFiles = map[string]bool{".DS_Store": true, "Thumbs.db": true, "desktop.ini": true, "Icon\r": true}

// isJunk reports whether a file is OS litter: a known name or an AppleDouble "._" file.
func isJunk(name string) bool {
	return junkFiles[name] || strings.HasPrefix(name, "._")
}

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
		if d.Type()&os.ModeSymlink != 0 || isJunk(d.Name()) {
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
		if len(hits[i].rel) != len(hits[j].rel) {
			return len(hits[i].rel) < len(hits[j].rel)
		}
		return hits[i].rel < hits[j].rel
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

// sendNotYet reports whether the send action names the harness's session
// and none exists yet: a fresh OpenCode or Kilo has no session until the
// first prompt, so that prompt is typed into the TUI, which makes one.
func (e *Engine) sendNotYet(s *sess, act spec.Action) bool {
	raw, err := json.Marshal(act)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return notYet(raw, s.stateLocked())
}
