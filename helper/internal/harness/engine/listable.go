package engine

import (
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// degradedAfter is how many failures in a row mark a feed degraded.
const degradedAfter = 3

// screenListableLocked reports whether describe lists a screen: never one
// that could only open on a failure. A status screen needs a Status block
// to show; a screen read from a feed needs that feed working.
func (s *sess) screenListableLocked(sc spec.Screen) bool {
	if sc.Primitive == harness.PrimStatus {
		sec := s.mission.section(harness.SectionStatus)
		if sec == nil || sec.Status == nil {
			return false
		}
	}
	if run := s.feeds[sc.Source]; run != nil && run.failures >= degradedAfter {
		return false
	}
	return true
}

// stoppable reports whether target is a row of the Agents or Background
// section, the only things the stop route acts on.
func (s *sess) stoppable(target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, kind := range []string{harness.SectionAgents, harness.SectionBackground} {
		if sec := s.mission.section(kind); sec != nil {
			for _, r := range sec.Rows {
				if r.ID == target {
					return true
				}
			}
		}
	}
	return false
}
