package hookstyle

import (
	"fmt"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/internal/errtext"
)

func (m *Mapper) notice(at time.Time, code, level, text string) []harness.Item {
	m.n++
	return []harness.Item{{
		ID: fmt.Sprintf("hook-notice-%d-%d", at.UnixMilli(), m.n), Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: level, Code: code, Text: text},
	}}
}

// errorNotice says why a turn failed: the reason the prep read, else one
// picked from the agent's words, else fallback.
func (m *Mapper) errorNotice(at time.Time, turn string, p payload, fallback string) harness.Item {
	m.n++
	var body *harness.NoticeBody
	switch {
	case p.ErrorReason != "":
		body = errtext.Of(p.ErrorReason, p.ErrorDetails)
	case p.ErrorDetails != "":
		body = errtext.Notice(p.ErrorDetails, 0, fallback)
	default:
		body = harness.ErrorNotice(harness.ErrOther, fallback)
	}
	return harness.Item{ID: fmt.Sprintf("hook-notice-%d-%d", at.UnixMilli(), m.n), TurnID: turn, Kind: harness.KindNotice, At: at, Notice: body}
}
