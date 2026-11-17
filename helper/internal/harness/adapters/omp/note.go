package omp

import (
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// note is a message omp adds on its own account (role "developer": open
// todo items, a background agent's report). The model answers each one, so
// after a reply that ended the turn it starts the next: without it the
// session read as idle, and done, while the agent worked on. Inside a
// running turn it changes nothing, and the note itself is not shown.
func (s *session) note(id, parent string, at time.Time) ([]harness.Item, []harness.Op) {
	if parent != "" || s.turn == nil || !s.turn.ended {
		return nil, nil
	}
	s.turn = &turn{files: map[string]bool{}, item: harness.Item{ID: "turn-" + id, Kind: harness.KindTurn, At: at,
		Turn: &harness.TurnBody{State: harness.StateRunning, Started: at}}}
	s.turn.item.TurnID = s.turn.item.ID
	return []harness.Item{s.turn.item}, nil
}
