package engine

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// seedSettle lets the agent redraw its prompt box after a turn ends
// before the pane is read.
const seedSettle = 400 * time.Millisecond

// screenSeed is state read from the pane (the permission mode under
// Claude Code's prompt box). It stands until a mapper reports a different
// value for the same key: a hook or transcript record is newer than it.
type screenSeed struct {
	state map[string]string
	// under is the mappers' value of each key when the screen was read.
	under map[string]string
}

func (sd *screenSeed) overlay(out map[string]string) {
	for k, v := range sd.state {
		if out[k] == sd.under[k] {
			out[k] = v
		}
	}
}

// seedFromScreen reads the pane once, after wait, with the mappers that
// can read state from it, so the mode is known without a hook payload or
// status line, and again whenever the agent goes idle (a Shift+Tab at the
// desk changes it without any record).
func (s *sess) seedFromScreen(wait time.Duration) {
	if s.e.cfg.Screen == nil || s.ad == nil || s.ad.Modes == nil {
		return
	}
	select {
	case <-s.ctx.Done():
		return
	case <-time.After(wait):
	}
	s.mu.Lock()
	var readers []ScreenReader
	for _, m := range s.mappers {
		if r, ok := m.(ScreenReader); ok {
			readers = append(readers, r)
		}
	}
	s.mu.Unlock()
	if len(readers) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	text, err := s.e.cfg.Screen(ctx, s.id)
	cancel()
	if err != nil {
		return
	}
	read := map[string]string{}
	for _, r := range readers {
		for k, v := range r.ReadScreen(text) {
			read[k] = v
		}
	}
	if len(read) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	under, _ := s.mapperStateRawLocked()
	s.seed.state, s.seed.under = read, map[string]string{}
	for k := range read {
		s.seed.under[k] = under[k]
	}
	s.refreshStatusLocked()
}

// mapperStateRawLocked is the mappers' merged state without the seed.
func (s *sess) mapperStateRawLocked() (map[string]string, []map[string]string) {
	seed := s.seed
	s.seed = screenSeed{}
	defer func() { s.seed = seed }()
	return s.mapperStateLocked()
}

// mergeStatusLocked keeps what an incoming status block leaves out from
// the block shown now: Claude's transcript reports only the model and the
// context, its status line also the meters and cost.
func (s *sess) mergeStatusLocked(st *harness.StatusBlock) {
	sec := s.mission.section(harness.SectionStatus)
	if sec == nil || sec.Status == nil {
		return
	}
	cur := sec.Status
	if st.Model == "" {
		st.Model = cur.Model
	}
	if st.ContextPercent == nil {
		st.ContextPercent = cur.ContextPercent
	}
	if st.CostUSD == nil {
		st.CostUSD = cur.CostUSD
	}
	if st.Meters == nil {
		st.Meters = cur.Meters
	}
	if st.Facts == nil {
		st.Facts = cur.Facts
	}
}
