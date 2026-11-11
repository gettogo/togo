package engine

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
)

// partialEvery is the minimum gap between two partial updates of an item.
const partialEvery = 250 * time.Millisecond

// summaryEvery debounces summary change callbacks.
const summaryEvery = time.Second

// phoneWindow is how long a prompt typed from the phone waits to be matched
// with the agent's record of it.
const phoneWindow = 2 * time.Minute

// publishLocked prepares items (limits, redaction, anchors), stores them,
// applies ops and notifies subscribers.
func (s *sess) publishLocked(items []harness.Item, ops []harness.Op) {
	var ready []harness.Item
	now := s.e.cfg.Now()
	mark := s.batchMark()
	for _, it := range items {
		if it.ID == "" || it.Kind == "" {
			continue
		}
		s.prepareLocked(&it, mark)
		s.labelModelNoticeLocked(&it)
		if it.Kind == harness.KindQuestion && it.Question != nil {
			ops = append(ops, s.questionLocked(&it)...)
			s.offerLocked(it)
		}
		if txt := partialText(it); txt != nil && txt.Partial {
			if t, ok := s.lastPart[it.ID]; ok && now.Sub(t) < partialEvery {
				s.pending[it.ID] = it
				s.schedulePartialLocked(it.ID)
				continue
			}
			s.lastPart[it.ID] = now
		} else {
			delete(s.pending, it.ID)
			delete(s.lastPart, it.ID)
		}
		if it.Kind != harness.KindUser && it.Kind != harness.KindTurn {
			s.awaitReply = false // the agent answered something
		}
		ready = append(ready, it)
	}
	if len(ready) > 0 {
		stored := s.appendFormLocked(s.e.log.Append(s.id, ready))
		for _, chunk := range chunkItems(stored, batchBytes) {
			s.notifyLocked(harness.Notification{Kind: harness.NoteItems, Items: chunk})
		}
	}
	for i := range ops {
		if ops[i].Op == harness.OpStatus && ops[i].Status != nil {
			st := *ops[i].Status
			s.mergeStatusLocked(&st)
			s.decorateStatusLocked(&st)
			ops[i].Status = &st
		}
	}
	if d := s.mission.apply(ops); d != nil {
		s.missionDeltaLocked(d)
	}
	s.relinkLocked()
}

// decorateStatusLocked fills the status block's mode, effort and actions
// from the session's state and the adapter's status screens.
func (s *sess) decorateStatusLocked(st *harness.StatusBlock) {
	if s.ad.Modes != nil {
		if m := s.currentModeLocked(); m != "" {
			st.Mode = m
		}
	}
	state := s.stateLocked()
	if st.Effort == "" {
		st.Effort = state["effort"]
	}
	fromState := st.Model == ""
	if fromState {
		st.Model = state["model"]
	}
	if st.Model != "" {
		st.Model = s.modelLabelLocked(st.Model)
	}
	if fromState {
		s.statusModelFromState = st.Model
	}
	if len(st.Actions) == 0 {
		// Status screens' actions, then the adapter's status.actions
		// (compact on the context meter), once each.
		var names []string
		for _, name := range s.ad.screenOrder {
			sc := s.ad.Screens[name]
			if sc.Primitive == harness.PrimStatus && sc.Section == harness.SectionStatus {
				names = append(names, sc.Actions...)
			}
		}
		st.Actions = s.statusActionRefsLocked(names)
	}
}

// refreshStatusLocked updates the status block when the mode, effort or
// model changed since it was last built, and creates it once any of them
// is known: the mode chip reads it even when no status line reports.
func (s *sess) refreshStatusLocked() {
	sec := s.mission.section(harness.SectionStatus)
	if sec == nil {
		return
	}
	if sec.Status == nil {
		st := harness.StatusBlock{}
		s.decorateStatusLocked(&st)
		if st.Mode == "" && st.Effort == "" && st.Model == "" {
			return
		}
		if d := s.mission.apply([]harness.Op{{Op: harness.OpStatus, Status: &st}}); d != nil {
			s.missionDeltaLocked(d)
		}
		return
	}
	st := *sec.Status
	st.Mode, st.Effort = "", ""
	if m := s.stateLocked()["model"]; st.Model == m || (m != "" && st.Model == s.modelLabelLocked(m)) ||
		(st.Model != "" && st.Model == s.statusModelFromState) {
		// The model shown came from the session's state: read it again, so
		// a switch replaces it rather than leaving the one before.
		st.Model = ""
	}
	s.decorateStatusLocked(&st)
	if st.Mode != sec.Status.Mode || st.Effort != sec.Status.Effort || st.Model != sec.Status.Model {
		if d := s.mission.apply([]harness.Op{{Op: harness.OpStatus, Status: &st}}); d != nil {
			s.missionDeltaLocked(d)
		}
	}
}

func (s *sess) schedulePartialLocked(id string) {
	time.AfterFunc(partialEvery, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		it, ok := s.pending[id]
		if !ok || s.ctx.Err() != nil {
			// Gone, or the session was forgotten: appending now would
			// start a new log epoch for a session nobody serves.
			return
		}
		delete(s.pending, id)
		s.lastPart[id] = s.e.cfg.Now()
		stored := s.appendFormLocked(s.e.log.Append(s.id, []harness.Item{it}))
		s.notifyLocked(harness.Notification{Kind: harness.NoteItems, Items: stored})
	})
}

// appendFormLocked turns streamed text updates into their new tail: a
// partial assistant or thinking text that continues what subscribers were
// last sent goes as {text: tail, appendFrom: bytes held}. Anything else
// (the first update, a rewrite, the final text) goes whole. The log keeps
// whole text, which is what harness.open serves, so a client that opens
// in between holds exactly the sent text the next tail continues.
func (s *sess) appendFormLocked(items []harness.Item) []harness.Item {
	if s.sentText == nil {
		s.sentText = map[string]string{}
	}
	out := items
	for i, it := range items {
		t := partialText(it)
		if t == nil {
			continue
		}
		if !t.Partial {
			delete(s.sentText, it.ID)
			continue
		}
		prev, held := s.sentText[it.ID]
		s.sentText[it.ID] = t.Text
		if !held || prev == "" || len(t.Text) <= len(prev) || !strings.HasPrefix(t.Text, prev) {
			continue
		}
		if &out[0] == &items[0] {
			out = append([]harness.Item(nil), items...)
		}
		n := len(prev)
		tail := harness.TextBody{Text: t.Text[n:], Partial: true, AppendFrom: &n}
		c := it
		if c.Kind == harness.KindAssistant {
			c.Assistant = &tail
		} else {
			c.Thinking = &tail
		}
		out[i] = c
	}
	return out
}

func partialText(it harness.Item) *harness.TextBody {
	switch it.Kind {
	case harness.KindAssistant:
		return it.Assistant
	case harness.KindThinking:
		return it.Thinking
	}
	return nil
}

func (s *sess) missionDeltaLocked(d *harness.Delta) {
	s.notifyLocked(harness.Notification{Kind: harness.NoteMission, Mission: d})
	for _, op := range d.Ops {
		if op.Op == harness.OpSummary {
			s.storeSummaryLocked()
			s.summaryChangedLocked()
		}
	}
}

func (s *sess) storeSummaryLocked() {
	sum := s.mission.summary
	if sum.Plan != nil {
		p := *sum.Plan
		sum.Plan = &p
	}
	s.sum.Store(&sum)
}

func (s *sess) notifyLocked(n harness.Notification) {
	n.SessionID, n.TS, n.Epoch = s.id, s.e.cfg.Now(), s.e.log.Epoch(s.id)
	for _, q := range s.subs {
		q.push(n)
	}
}

func (s *sess) summaryChangedLocked() {
	if s.e.cfg.OnSummary == nil || s.summaryT != nil {
		return
	}
	s.summaryT = time.AfterFunc(summaryEvery, func() {
		s.mu.Lock()
		s.summaryT = nil
		s.mu.Unlock()
		s.e.cfg.OnSummary(s.id)
	})
}

// prepareLocked enforces limits, redacts text, keeps first anchors and
// tags prompts typed from the phone.
func (s *sess) prepareLocked(it *harness.Item, mark func() (harness.Anchor, bool)) {
	red := s.e.red
	switch it.Kind {
	case harness.KindUser:
		if u := it.User; u != nil {
			u.Text = clipBytes(red.String(u.Text), harness.MaxItemText)
			if u.Source == "" {
				u.Source = "unknown"
			}
			if !u.Queued {
				if p, ok := s.matchPhoneLocked(it.ID, u.Text); ok {
					it.ID, u.Source = p.id, "phone"
					// The phone's paths can be fetched; the agent's copy of a
					// picture may be only described.
					if len(p.images) > 0 {
						u.Images = p.images
					}
					if len(u.Images) > 0 {
						u.Text = p.text
					}
				}
			}
		}
	case harness.KindAssistant, harness.KindThinking:
		if t := partialText(*it); t != nil {
			t.Text = clipBytes(red.String(t.Text), harness.MaxItemText)
		}
	case harness.KindTool:
		if t := it.Tool; t != nil {
			t.Title = clipRunes(red.String(t.Title), harness.MaxTitle)
			t.Output = tailBytes(red.String(t.Output), harness.MaxToolOutput)
			if t.Subject != nil {
				sub := *t.Subject
				sub.Text = red.String(sub.Text)
				t.Subject = &sub
			}
		}
	case harness.KindShell:
		if sh := it.Shell; sh != nil {
			sh.Command = clipBytes(red.String(sh.Command), harness.MaxItemText)
			sh.Output = tailBytes(red.String(sh.Output), harness.MaxToolOutput)
		}
	case harness.KindNotice:
		if n := it.Notice; n != nil {
			n.Text = clipRunes(red.String(n.Text), 4*harness.MaxTitle)
		}
	case harness.KindAgent:
		if a := it.Agent; a != nil {
			a.Name = clipRunes(red.String(a.Name), harness.MaxTitle)
			a.Summary = clipBytes(red.String(a.Summary), harness.MaxItemText)
		}
	case harness.KindBackground:
		if b := it.Background; b != nil {
			b.Title = clipRunes(red.String(b.Title), harness.MaxTitle)
			b.Command = clipRunes(red.String(b.Command), 4*harness.MaxTitle)
		}
	case harness.KindQuestion:
		if q := it.Question; q != nil {
			for i := range q.Questions {
				q.Questions[i].Prompt = clipRunes(red.String(q.Questions[i].Prompt), 4*harness.MaxTitle)
			}
		}
	}
	if a, ok := s.anchors[it.ID]; ok {
		if a != nil {
			c := *a
			it.Anchor = &c
		}
		return
	}
	if it.Kind == harness.KindTurn || it.Kind == harness.KindPlan || it.Parent != "" || s.e.cfg.Marker == nil {
		return
	}
	if it.Kind == harness.KindUser && it.User != nil && it.User.Queued {
		return // not in the pane yet; anchored once typed
	}
	if !it.At.IsZero() && s.e.cfg.Now().Sub(it.At) > replayAge {
		// Replayed history (a daemon restart, a resumed thread): the
		// pane's cursor says nothing about where it happened.
		s.anchors[it.ID] = nil
		return
	}
	a, ok := mark()
	if !ok {
		s.anchors[it.ID] = nil
		return
	}
	s.anchors[it.ID] = &a
	c := a
	it.Anchor = &c
}

// replayAge is how old an item may be and still be anchored at the
// pane's current position.
const replayAge = 2 * time.Minute

// markWait bounds the one pane read a batch makes for its anchors.
const markWait = 500 * time.Millisecond

// batchMark returns a function that reads the pane's position at most
// once per published batch: every new item of a batch arrived at the same
// moment, so they share one anchor, and the session lock is held for one
// short pane read rather than one per item.
func (s *sess) batchMark() func() (harness.Anchor, bool) {
	var (
		done bool
		a    harness.Anchor
		ok   bool
	)
	return func() (harness.Anchor, bool) {
		if !done {
			done = true
			ctx, cancel := context.WithTimeout(s.ctx, markWait)
			var err error
			a, err = s.e.cfg.Marker.Mark(ctx, s.id)
			cancel()
			ok = err == nil
		}
		return a, ok
	}
}

// notePhoneLocked remembers a prompt the helper typed.
func (s *sess) notePhoneLocked(id, text string, images []harness.ImageRef) {
	now := s.e.cfg.Now()
	keep := s.phone[:0]
	for _, p := range s.phone {
		if now.Sub(p.at) < phoneWindow {
			keep = append(keep, p)
		}
	}
	s.phone = append(keep, phoneText{id: id, text: strings.TrimSpace(text), images: images, at: now})
}

// imagePlaceholder is how an agent writes a pasted picture into the prompt text.
var imagePlaceholder = regexp.MustCompile(`\[Image #\d+\]`)

// matchPhoneLocked finds the phone prompt the agent's record of text is.
// A prompt sent with pictures matches once the pasted paths and the
// agent's [Image #n] placeholders are taken out.
func (s *sess) matchPhoneLocked(self, text string) (phoneText, bool) {
	text = strings.TrimSpace(text)
	for i, p := range s.phone {
		if p.id == self {
			return phoneText{}, false
		}
		if p.text == text || (len(p.images) > 0 && strings.Join(strings.Fields(p.text), " ") == withoutImages(text, p.images)) {
			s.phone = append(s.phone[:i], s.phone[i+1:]...)
			return p, true
		}
	}
	return phoneText{}, false
}

func withoutImages(text string, images []harness.ImageRef) string {
	for _, r := range images {
		if r.Path != "" {
			text = strings.ReplaceAll(text, harness.QuotePath(r.Path), "")
			text = strings.ReplaceAll(text, r.Path, "")
		}
	}
	text = imagePlaceholder.ReplaceAllString(text, "")
	return strings.Join(strings.Fields(text), " ")
}

// currentModeLocked is the mode id from the modes source, else state.
func (s *sess) currentModeLocked() string {
	m := s.ad.Modes
	if m == nil {
		return ""
	}
	state := s.stateLocked()
	if m.Select != "" {
		var input any
		if raw, ok := s.last[m.Current]; ok {
			input, _ = decode(raw)
		}
		if v, err := JQFirst(m.Select, input, state); err == nil && v != nil {
			if str := toString(v); str != "" {
				return str
			}
		}
	}
	return state["mode"]
}

func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func sortedIDs[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
