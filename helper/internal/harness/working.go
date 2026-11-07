package harness

import "time"

// StateWorkingAt is the mapper state key that stamps the mapper's
// "working" value with when the mapper last learned it (RFC 3339).
const StateWorkingAt = "working_at"

// MergeWorking decides whether a turn runs from the states of a session's
// mappers. A stamped "working" is a statement about the session at that
// time, so among stamped mappers the newest statement wins: a turn the
// transcript saw interrupted is over even though the last hook, sent
// before the interrupt, said working and no Stop hook follows an
// interrupt. An unstamped "working" keeps the old rule and ORs in. On a
// tie, working wins.
func MergeWorking(states ...map[string]string) bool {
	unstamped := false
	var newest time.Time
	stamped, newestWorking := false, false
	for _, st := range states {
		v, ok := st["working"]
		if !ok {
			continue
		}
		w := v == "true"
		at, err := time.Parse(time.RFC3339Nano, st[StateWorkingAt])
		if err != nil {
			unstamped = unstamped || w
			continue
		}
		if !stamped || at.After(newest) || (at.Equal(newest) && w) {
			newest, newestWorking, stamped = at, w, true
		}
	}
	return unstamped || newestWorking
}

// StampWorking formats t for StateWorkingAt; the zero time is no stamp.
func StampWorking(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
