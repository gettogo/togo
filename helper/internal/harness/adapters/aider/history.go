package aider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/wire"
)

// MapperHistory reads .aider.chat.history.md, as aider.yaml names it.
const MapperHistory = "aider.history"

// historyName is the chat history file aider writes by default, at the git
// root of the folder it starts in, else in that folder (args.py, 0.86).
const historyName = ".aider.chat.history.md"

// headerSlack is how much earlier than the Togo session an aider start
// header may be stamped: the header has whole seconds of local time.
const headerSlack = 2 * time.Second

// history maps the chat history aider appends as markdown (io.py, 0.86):
//
//   - "# aider chat started at 2006-01-02 15:04:05" when aider starts;
//   - "#### text  " per line of each prompt the user entered, slash
//     commands included (user_input);
//   - "> text  " per line of tool output, warnings, errors and answered
//     confirmations (append_chat_history with blockquote);
//   - the reply as plain markdown, written whole once it finished
//     (ai_output).
//
// Prompt and quote lines always end in the two spaces of a markdown line
// break, which a reply's own headings and quotes do not, so they are told
// apart by that. The file has no turn end: a prompt opens a turn, and the
// turn closes when the next prompt arrives or the session goes idle by the
// pane (CloseIdle). Only content after a start header no older than the
// Togo session is mapped: the file holds every earlier aider run in the
// folder too.
type history struct {
	env     harness.Env
	created time.Time

	mu     sync.Mutex
	state  map[string]string
	active bool
	n      int

	user    []string // prompt lines not yet published
	reply   []string // the reply being read
	replyID string
	quote   []string // a run of tool lines not yet published

	turn    string
	open    bool
	started time.Time
	steps   int

	launchSeen  bool          // the command line after the start header was dropped
	failed      bool          // the open turn hit an error that ends it
	interrupted bool          // the user pressed ^C in the open turn
	notice      *harness.Item // the last error or warning notice, which later lines may update
	noticeBase  string        // its text before any retry note
	noticeFails bool          // it ends the turn unless aider retries
	retries     int           // how often aider retried after it
	absorb      absorb        // which following lines belong to it
}

func newHistory(env harness.Env) *history {
	h := &history{env: env, state: map[string]string{}}
	if c, err := time.Parse(time.RFC3339, env.Vars["created"]); err == nil {
		h.created = c
	}
	if p := historyPath(env); p != "" {
		h.state["transcript_path"] = p
	}
	return h
}

func (h *history) Name() string { return MapperHistory }

func (h *history) State() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]string, len(h.state))
	for k, v := range h.state {
		out[k] = v
	}
	return out
}

// historyPath is the file the session's aider writes: --chat-history-file
// from its argv, else .aider.chat.history.md at the git root above the
// session folder, else in the folder.
func historyPath(env harness.Env) string {
	if env.Cwd == "" {
		return ""
	}
	for i, a := range env.Argv {
		var p string
		if v, ok := strings.CutPrefix(a, "--chat-history-file="); ok {
			p = v
		} else if a == "--chat-history-file" && i+1 < len(env.Argv) {
			p = env.Argv[i+1]
		}
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(env.Cwd, p)
		}
		return filepath.Clean(p)
	}
	for dir := filepath.Clean(env.Cwd); ; {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return filepath.Join(dir, historyName)
		}
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	return filepath.Join(filepath.Clean(env.Cwd), historyName)
}

var (
	headerLine = regexp.MustCompile(`^# aider chat started at (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)\s*$`)
	editLine   = regexp.MustCompile(`^Applied edit to (.+)$`)
	commitLine = regexp.MustCompile(`^Commit ([0-9a-f]{7,40}) (.*)$`)
	runLine    = regexp.MustCompile(`^Running (.+)$`)
	// groupAnswer is a confirmation aider answered by itself after an
	// earlier "(A)ll" or "(S)kip all" in the same group: confirm_ask logs
	// it with user_input, so it arrives as a prompt line ("#### Add file to
	// the chat? (Y)es/(N)o/(A)ll/(S)kip all/(D)on't ask again [Yes]: all"),
	// though nobody typed it (io.py, 0.86).
	groupAnswer = regexp.MustCompile(`\(Y\)es/\(N\)o(/\([A-Z]\)[^/\[]*)*\s*\[(Yes|No)\]:\s*(all|skip)$`)
	errorLine   = regexp.MustCompile(`^(litellm\.\S*Error|\S*Error:|Error |Traceback )`)
)

// Map takes one lines record: the whole lines one poll read.
func (h *history) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var lines []string
	if json.Unmarshal(rec.Data, &lines) != nil {
		return nil, nil
	}
	at := rec.At
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	h.mu.Lock()
	defer h.mu.Unlock()
	var items []harness.Item
	for _, line := range lines {
		items = append(items, h.line(line, at)...)
	}
	return append(items, h.flush(at)...), nil
}

func (h *history) line(line string, at time.Time) []harness.Item {
	if m := headerLine.FindStringSubmatch(line); m != nil {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", m[1], time.Local)
		if err != nil {
			return nil
		}
		if !h.created.IsZero() && t.Before(h.created.Add(-headerSlack)) {
			return nil // an earlier aider run in this folder
		}
		items := h.flush(at)
		if h.active && h.open {
			items = append(items, h.closeTurn(at, harness.StateInterrupted)...)
		}
		h.active, h.launchSeen = true, false
		h.endNotice()
		return items
	}
	if !h.active {
		return nil
	}
	breakEnd := strings.HasSuffix(line, "  ")
	switch {
	case breakEnd && strings.HasPrefix(line, "####") && groupAnswer.MatchString(strings.TrimSpace(strings.TrimPrefix(line, "####"))):
		// Not the user's: a step of the turn, like the answered question
		// it repeats.
		items := h.flushUser(at)
		items = append(items, h.flushReply(at)...)
		h.quote = append(h.quote, strings.TrimSpace(strings.TrimPrefix(line, "####")))
		return items
	case breakEnd && strings.HasPrefix(line, "####"):
		items := h.flushReply(at)
		items = append(items, h.flushQuote(at)...)
		text := strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(line, "####"), " "), " ")
		h.user = append(h.user, text)
		return items
	case breakEnd && strings.HasPrefix(line, ">"):
		items := h.flushUser(at)
		items = append(items, h.flushReply(at)...)
		h.quote = append(h.quote, strings.TrimSpace(strings.TrimPrefix(line, ">")))
		return items
	}
	items := h.flushUser(at)
	items = append(items, h.flushQuote(at)...)
	if len(h.reply) == 0 && strings.TrimSpace(line) == "" {
		return items
	}
	if h.replyID == "" {
		h.n++
		h.replyID = "aider-" + strconv.Itoa(h.n) + ":text"
	}
	h.reply = append(h.reply, line)
	return items
}

// flush publishes what the record left pending. A reply stays open across
// records (it grows under the same id) until another kind of line comes.
func (h *history) flush(at time.Time) []harness.Item {
	items := h.flushUser(at)
	items = append(items, h.flushQuote(at)...)
	if len(h.reply) > 0 {
		items = append(items, h.replyItem(at))
	}
	return items
}

func (h *history) flushUser(at time.Time) []harness.Item {
	if len(h.user) == 0 {
		return nil
	}
	text := strings.Join(h.user, "\n")
	h.user = nil
	if text == "<blank>" {
		text = ""
	}
	var items []harness.Item
	if h.open {
		items = h.closeTurn(at, h.endState())
	}
	h.n++
	id := "aider-" + strconv.Itoa(h.n)
	h.turn, h.open, h.started, h.steps = id+".turn", true, at, 0
	h.failed, h.interrupted = false, false
	h.endNotice()
	h.state["turn"] = h.turn
	items = append(items, harness.Item{ID: id, TurnID: h.turn, Kind: harness.KindUser, At: at,
		User: &harness.UserBody{Text: hookstyle.Clip(text), Source: "unknown"}})
	return append(items, h.turnItem(harness.StateRunning, nil))
}

func (h *history) replyItem(at time.Time) harness.Item {
	text := strings.TrimRight(strings.Join(h.reply, "\n"), "\n ")
	return harness.Item{ID: h.replyID, TurnID: h.turn, Kind: harness.KindAssistant, At: at,
		Assistant: &harness.TextBody{Text: hookstyle.Clip(text)}}
}

func (h *history) flushReply(at time.Time) []harness.Item {
	if len(h.reply) == 0 {
		h.replyID = ""
		return nil
	}
	it := h.replyItem(at)
	h.reply, h.replyID = nil, ""
	return []harness.Item{it}
}

// flushQuote turns a run of tool lines into items: edits, commits and shell
// runs become tool steps, errors and warnings become notices (errors.go),
// the launch command line and token reports are dropped, and each run of other lines between them (files added,
// answered confirmations) folds into one step titled by its first line.
func (h *history) flushQuote(at time.Time) []harness.Item {
	if len(h.quote) == 0 {
		return nil
	}
	lines := h.quote
	h.quote = nil
	var items []harness.Item
	var rest []string
	fold := func() {
		if len(rest) == 0 {
			return
		}
		x := h.tool(at, "aider", harness.ToolOther, rest[0], nil, nil)
		if len(rest) > 1 {
			x.Tool.Output = hookstyle.Tail(strings.Join(rest, "\n"), harness.MaxToolOutput)
		}
		items = append(items, x)
		rest = nil
	}
	for _, l := range lines {
		if h.absorb != absorbNone && h.notice != nil {
			before := h.notice.Notice.Text
			if h.follows(h.absorb, l, h.notice) {
				if h.notice.Notice.Text != before {
					items = append(items, *h.notice)
				}
				continue
			}
			h.absorb = absorbNone
		}
		c := h.classify(l)
		switch c.kind {
		case lineSkip:
		case lineInterrupt:
			h.interrupted = true
		case lineRetry:
			if it := h.retry(); it != nil {
				items = append(items, *it)
			}
		case lineError, lineWarning:
			fold()
			if it := h.noticeItem(at, c); it != nil {
				items = append(items, *it)
			}
		default:
			switch {
			case editLine.MatchString(l):
				fold()
				p := editLine.FindStringSubmatch(l)[1]
				items = append(items, h.tool(at, "edit", harness.ToolEdit, "Edited "+p,
					&wire.Subject{Kind: wire.SubjectFile, Text: p}, []harness.FileChange{{Path: p}}))
			case commitLine.MatchString(l):
				fold()
				m := commitLine.FindStringSubmatch(l)
				items = append(items, h.tool(at, "commit", harness.ToolOther, "Commit "+m[1]+" "+m[2], nil, nil))
			case runLine.MatchString(l):
				fold()
				c := runLine.FindStringSubmatch(l)[1]
				items = append(items, h.tool(at, "run", harness.ToolShell, c,
					&wire.Subject{Kind: wire.SubjectCommand, Text: c}, nil))
			default:
				rest = append(rest, l)
			}
		}
	}
	fold()
	if h.open && len(items) > 0 {
		items = append(items, h.turnItem(harness.StateRunning, nil))
	}
	return items
}

func (h *history) tool(at time.Time, name, cat, title string, subj *wire.Subject, files []harness.FileChange) harness.Item {
	h.n++
	id := "aider-" + strconv.Itoa(h.n)
	if h.open {
		h.steps++
	}
	end := at
	return harness.Item{ID: id, TurnID: h.turn, Kind: harness.KindTool, At: at, Tool: &harness.ToolBody{
		CallID: id, Name: name, Title: hookstyle.Truncate(title, harness.MaxTitle), Category: cat,
		Subject: subj, State: harness.StateOK, Started: at, Ended: &end, Files: files}}
}

func (h *history) closeTurn(at time.Time, state string) []harness.Item {
	it := h.turnItem(state, &at)
	h.open = false
	return []harness.Item{it}
}

func (h *history) turnItem(state string, ended *time.Time) harness.Item {
	return harness.Item{ID: h.turn, TurnID: h.turn, Kind: harness.KindTurn, At: h.started,
		Turn: &harness.TurnBody{State: state, Started: h.started, Ended: ended, Steps: h.steps}}
}

// CloseIdle ends the open turn once the session went idle: aider is back at
// its prompt.
func (h *history) CloseIdle(at time.Time) []harness.Item {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open {
		return nil
	}
	items := h.flush(at.UTC())
	return append(items, h.closeTurn(at.UTC(), h.endState())...)
}

// endState is how the open turn ends: failed after an error aider gave up
// on, interrupted after ^C, else ok.
func (h *history) endState() string {
	switch {
	case h.failed:
		return harness.StateFailed
	case h.interrupted:
		return harness.StateInterrupted
	}
	return harness.StateOK
}

// endNotice forgets the last notice: lines after this no longer update it.
func (h *history) endNotice() {
	h.notice, h.noticeBase, h.noticeFails, h.retries, h.absorb = nil, "", false, 0, absorbNone
}

// noticeItem makes the notice for an error or warning line. The same error
// again while aider retries updates the notice it already showed.
func (h *history) noticeItem(at time.Time, c classified) *harness.Item {
	if c.kind == lineError && h.notice != nil && h.retries > 0 && c.text == h.noticeBase {
		// Failed again: an error until aider says it retries once more.
		h.absorb = c.absorb
		if c.fails && h.open {
			h.failed = true
		}
		it := *h.notice
		body := *it.Notice
		body.Level, body.Text = harness.ErrorNotice(c.reason, c.text).Level, h.noticeBase
		it.Notice = &body
		h.notice = &it
		return &it
	}
	var body *harness.NoticeBody
	if c.kind == lineError {
		body = harness.ErrorNotice(c.reason, c.text)
	} else {
		body = &harness.NoticeBody{Level: "warning", Code: harness.NoticeError, Text: hookstyle.Truncate(c.text, harness.MaxNoticeText)}
	}
	h.n++
	it := harness.Item{ID: "aider-" + strconv.Itoa(h.n), TurnID: h.turn, Kind: harness.KindNotice, At: at, Notice: body}
	h.notice, h.noticeBase, h.noticeFails, h.retries, h.absorb = &it, body.Text, c.fails, 0, c.absorb
	if c.fails && h.open {
		h.failed = true
	}
	return &it
}

// retry marks the last error as one aider retries: a warning, and the turn
// goes on.
func (h *history) retry() *harness.Item {
	if h.notice == nil || h.notice.Notice.Reason == "" {
		return nil
	}
	h.retries++
	if h.noticeFails {
		h.failed = false
	}
	body := *h.notice.Notice
	body.Level = "warning"
	body.Text = h.noticeBase + " Retrying…"
	if h.retries > 1 {
		body.Text = h.noticeBase + " Retrying (attempt " + strconv.Itoa(h.retries) + ")…"
	}
	it := *h.notice
	it.Notice = &body
	h.notice = &it
	return &it
}
