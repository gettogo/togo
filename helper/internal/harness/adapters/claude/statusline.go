package claude

import (
	"strconv"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// statusline maps the status line payload (code.claude.com/docs/en/
// statusline) to the Status section: model, effort, context, cost and the
// rate limit meters.
type statusline struct {
	mu    sync.Mutex
	state map[string]string
	last  string
}

func newStatusline(harness.Env) *statusline {
	return &statusline{state: map[string]string{}}
}

func (s *statusline) Name() string { return MapperStatusline }

func (s *statusline) State() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.state))
	for k, v := range s.state {
		out[k] = v
	}
	return out
}

var meterLabels = map[string]string{"five_hour": "5 hour limit", "seven_day": "7 day limit"}

func (s *statusline) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	p := decodeObj(rec.Data)
	if p == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := &harness.StatusBlock{}
	if m := obj(p["model"]); m != nil {
		id, name := str(m, "id"), str(m, "display_name")
		st.Model = firstNonEmpty(name, id)
		if a := Alias(firstNonEmpty(id, name)); a != "" {
			s.state["model"] = a
		}
		s.state["model_name"] = st.Model
	}
	if e := obj(p["effort"]); e != nil {
		if lvl := str(e, "level"); lvl != "" {
			st.Effort = lvl
			s.state["effort"] = lvl
		}
	}
	if cw := obj(p["context_window"]); cw != nil {
		if v, ok := num(cw["used_percentage"]); ok && v > 0 && v <= 100 { // 0 is "not known yet"
			st.ContextPercent = &v
			s.state["context"] = strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	if c := obj(p["cost"]); c != nil {
		if v, ok := num(c["total_cost_usd"]); ok && v >= 0 {
			st.CostUSD = &v
		}
	}
	if rl := obj(p["rate_limits"]); rl != nil {
		for _, id := range []string{"five_hour", "seven_day"} {
			m := obj(rl[id])
			if m == nil {
				continue
			}
			v, ok := num(m["used_percentage"])
			if !ok {
				continue
			}
			meter := harness.Meter{ID: id, Label: meterLabels[id], Percent: v}
			if t, ok := resetTime(m["resets_at"]); ok {
				meter.Resets = &t
			}
			st.Meters = append(st.Meters, meter)
		}
	}
	key := statusKey(st)
	if key == s.last {
		return nil, nil
	}
	s.last = key
	return nil, []harness.Op{{Op: harness.OpStatus, Status: st}}
}

func statusKey(st *harness.StatusBlock) string {
	k := st.Model + "|" + st.Effort
	if st.ContextPercent != nil {
		k += "|" + strconv.FormatFloat(*st.ContextPercent, 'f', 1, 64)
	}
	if st.CostUSD != nil {
		k += "|" + strconv.FormatFloat(*st.CostUSD, 'f', 2, 64)
	}
	for _, m := range st.Meters {
		k += "|" + m.ID + strconv.FormatFloat(m.Percent, 'f', 1, 64)
		if m.Resets != nil {
			k += m.Resets.String()
		}
	}
	return k
}

// resetTime reads unix seconds or an RFC 3339 time.
func resetTime(v any) (time.Time, bool) {
	if f, ok := num(v); ok && f > 0 {
		if f > 1e12 {
			return time.UnixMilli(int64(f)).UTC(), true
		}
		return time.Unix(int64(f), 0).UTC(), true
	}
	if s, ok := v.(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
