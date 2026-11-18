package approval

import (
	"errors"
	"testing"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/wire"
)

func TestOutcomeNamesWhatBecameOfAnAnswer(t *testing.T) {
	unconfirmed := &api.CodeError{Code: rpc.CodeRefused, Data: map[string]string{"reason": wire.RefusedUnconfirmed}}
	for want, err := range map[string]error{
		wire.AnswerApplied:      nil,
		wire.ConflictExpired:    conflict(wire.ConflictExpired, ""),
		wire.ConflictAnswered:   conflict(wire.ConflictAnswered, wire.ByDesk),
		wire.ConflictPromptGone: conflict(wire.ConflictPromptGone, ""),
		wire.AnswerUnconfirmed:  unconfirmed,
		wire.AnswerFailed:       errors.New("capture pane: exit status 1"),
	} {
		if got := Outcome(err); got != want {
			t.Errorf("Outcome(%v) = %q, want %q", err, got, want)
		}
	}
	if got := Outcome(invalid("approval offers no approve option")); got != wire.AnswerFailed {
		t.Errorf("an invalid answer = %q, want failed", got)
	}
}
