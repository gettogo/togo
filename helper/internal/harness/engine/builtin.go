package engine

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Refresh periods of the built-in sections.
var (
	portsEvery   = 15 * time.Second
	changesEvery = 30 * time.Second
)

// startBuiltins runs the Changes, Ports and adapter mission sources for a
// session, and lists approvals already open.
func (e *Engine) startBuiltins(s *sess) {
	if e.cfg.Approvals != nil && s.hasNeeds() {
		for _, a := range e.cfg.Approvals.Pending(s.id) {
			e.addApproval(s, a)
		}
	}
	caps := s.ad.Capabilities
	if harness.Has(caps, harness.CapChanges) && e.cfg.Git != nil {
		go e.every(s, changesEvery, func(ctx context.Context) { e.refreshChanges(ctx, s) })
	}
	if harness.Has(caps, harness.CapPorts) && e.cfg.Ports != nil {
		go e.every(s, portsEvery, func(ctx context.Context) { e.refreshPorts(ctx, s) })
	}
	for _, section := range sortedIDs(s.ad.Mission) {
		ms := s.ad.Mission[section]
		if ms.Source == "" || !slices.Contains(harness.SectionOrder, section) {
			continue
		}
		every := ms.Refresh
		if every <= 0 {
			every = 30 * time.Second
		}
		go e.every(s, every, func(ctx context.Context) { e.refreshMission(ctx, s, section) })
	}
}

func (s *sess) hasNeeds() bool {
	return harness.Has(s.ad.Capabilities, harness.CapNeedsApprovals) || harness.Has(s.ad.Capabilities, harness.CapNeedsQuestions)
}

func (e *Engine) every(s *sess, d time.Duration, fn func(ctx context.Context)) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		fn(ctx)
		cancel()
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
	}
}

// turnEnded refreshes what a turn may have changed. Called with s.mu held.
func (e *Engine) turnEnded(s *sess) {
	if harness.Has(s.ad.Capabilities, harness.CapChanges) && e.cfg.Git != nil {
		go func() {
			ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
			defer cancel()
			e.refreshChanges(ctx, s)
		}()
	}
}

// refreshChanges replaces the Changes rows with git's view of the working
// tree when the cwd is a repository, keeping line counts mappers knew.
func (e *Engine) refreshChanges(ctx context.Context, s *sess) {
	st, err := e.cfg.Git(ctx, s.env.Cwd)
	if err != nil || !st.Repo {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	known := map[string]string{}
	if sec := s.mission.section(harness.SectionChanges); sec != nil {
		for _, r := range sec.Rows {
			known[r.ID] = r.Subtitle
		}
	}
	rows := []harness.Row{}
	for _, f := range st.Files {
		sub := known[f.Path]
		if sub == "" {
			sub = changeWord(f.Index, f.Worktree)
		}
		rows = append(rows, harness.Row{ID: f.Path, Title: f.Path, Subtitle: sub, State: harness.StateOK,
			Open: &harness.Open{Kind: harness.OpenDiff, Ref: f.Path}})
	}
	s.publishLocked(nil, []harness.Op{{Op: harness.OpReplace, Section: harness.SectionChanges, Rows: rows}})
}

func changeWord(index, worktree string) string {
	for _, c := range []string{worktree, index} {
		switch c {
		case "?":
			return "new"
		case "A":
			return "added"
		case "D":
			return "deleted"
		case "R":
			return "renamed"
		case "M":
			return "modified"
		}
	}
	return "changed"
}

func (e *Engine) refreshPorts(ctx context.Context, s *sess) {
	list, err := e.cfg.Ports(ctx, s.id)
	if err != nil {
		return
	}
	rows := []harness.Row{}
	for _, l := range list {
		p := strconv.Itoa(l.Port)
		rows = append(rows, harness.Row{ID: p, Title: ":" + p, Subtitle: l.Command, State: harness.StateRunning,
			Open: &harness.Open{Kind: harness.OpenURL, Ref: p}})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publishLocked(nil, []harness.Op{{Op: harness.OpReplace, Section: harness.SectionPorts, Rows: rows}})
}

// refreshMission fills a section from an adapter source. Without a map,
// rows read id, title, subtitle and state from common field names.
func (e *Engine) refreshMission(ctx context.Context, s *sess, section string) {
	ms := s.ad.Mission[section]
	data, _, err := e.fetch(ctx, s, ms.Source, nil, false)
	if err != nil {
		return
	}
	list, ok := data.([]any)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.stateLocked()
	pick := func(key string, elem any, def string) string {
		if expr := ms.Map["item."+key]; expr != "" {
			v, _ := MapValue(expr, elem, state)
			return v
		}
		v, _ := JQFirst(def, elem, state)
		return toString(v)
	}
	var ops []harness.Op
	seen := map[string]bool{}
	for _, elem := range list {
		r := harness.Row{
			ID:       pick("id", elem, `.id // .sessionId // .pid // empty`),
			Title:    pick("title", elem, `.name // .title // .description // .id // empty`),
			Subtitle: pick("subtitle", elem, `.cwd // .status // empty`),
			State:    runState(pick("state", elem, `.state // .status // empty`)),
		}
		if r.ID == "" {
			continue
		}
		r.ID = "src-" + r.ID
		seen[r.ID] = true
		ops = append(ops, harness.Op{Op: harness.OpUpsert, Section: section, Row: &r})
	}
	// Rows this source listed before and no longer lists go; rows that
	// mappers keep in the same section stay.
	if s.srcRows == nil {
		s.srcRows = map[string]map[string]bool{}
	}
	for id := range s.srcRows[section] {
		if !seen[id] {
			ops = append(ops, harness.Op{Op: harness.OpRemove, Section: section, ID: id})
		}
	}
	s.srcRows[section] = seen
	s.publishLocked(nil, ops)
}

func runState(s string) string {
	switch s {
	case "busy", "running", "working", "active", "in_progress":
		return harness.StateRunning
	case "idle", "waiting":
		return harness.StateIdle
	case "failed", "error":
		return harness.StateFailed
	case "done", "completed", "finished", "ok":
		return harness.StateOK
	case "":
		return harness.StateRunning
	}
	return s
}

// approvalOpened adds an approval item and a Needs row.
func (e *Engine) approvalOpened(id string, ev wire.Event) {
	var a wire.Approval
	switch p := ev.Payload.(type) {
	case wire.ApprovalOpenEvent:
		a = p.Approval
	case *wire.ApprovalOpenEvent:
		a = p.Approval
	default:
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := e.session(ctx, id)
	if err != nil || s.ad == nil || !s.hasNeeds() {
		return
	}
	e.addApproval(s, a)
}

// addApproval adds a pending approval's item and Needs row. Hub events are
// handled on their own goroutines, so a close can be handled before its
// open: the request is checked to be still pending under s.mu, and
// approvalClosed looks for the item under s.mu too, so whichever runs
// second sees the other's result.
func (e *Engine) addApproval(s *sess, a wire.Approval) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.cfg.Approvals != nil && !slices.ContainsFunc(e.cfg.Approvals.Pending(s.id), func(p wire.Approval) bool { return p.RequestID == a.RequestID }) {
		return
	}
	item := harness.Item{ID: "approval-" + a.RequestID, TurnID: s.turn, Kind: harness.KindApproval, At: a.CreatedAt,
		Approval: &harness.ApprovalBody{RequestID: a.RequestID}}
	row := harness.Row{ID: a.RequestID, Title: a.Title, State: harness.StateWaiting,
		Open: &harness.Open{Kind: harness.OpenApproval, Ref: a.RequestID}}
	if a.Subject != nil {
		row.Subtitle = a.Subject.Text
	}
	row.Title, row.Subtitle = s.e.red.String(row.Title), s.e.red.String(row.Subtitle)
	s.publishLocked([]harness.Item{item}, []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionNeeds, Row: &row}})
}

// approvalClosed resolves the approval item and removes its row from
// whichever session holds it.
func (e *Engine) approvalClosed(ev wire.Event) {
	var reqID, resolution string
	switch p := ev.Payload.(type) {
	case wire.ApprovalClosedEvent:
		reqID, resolution = p.RequestID, p.Resolution
	case *wire.ApprovalClosedEvent:
		reqID, resolution = p.RequestID, p.Resolution
	default:
		return
	}
	e.mu.Lock()
	list := make([]*sess, 0, len(e.sessions))
	for _, s := range e.sessions {
		list = append(list, s)
	}
	e.mu.Unlock()
	for _, s := range list {
		if s.ad == nil {
			continue
		}
		s.mu.Lock()
		it, ok := e.log.Get(s.id, "approval-"+reqID)
		if !ok {
			s.mu.Unlock()
			continue
		}
		if resolution == "" {
			resolution = "gone"
		}
		it.Approval = &harness.ApprovalBody{RequestID: reqID, Resolved: resolution}
		s.publishLocked([]harness.Item{it}, []harness.Op{{Op: harness.OpRemove, Section: harness.SectionNeeds, ID: reqID}})
		s.mu.Unlock()
	}
}
