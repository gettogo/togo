package harness

import "testing"

func TestMergeWorking(t *testing.T) {
	st := func(w, at string) map[string]string {
		m := map[string]string{"working": w}
		if at != "" {
			m[StateWorkingAt] = at
		}
		return m
	}
	const t1, t2 = "2026-11-07T13:04:39.55Z", "2026-11-07T13:04:41.655Z"
	cases := []struct {
		name   string
		states []map[string]string
		want   bool
	}{
		{"none", nil, false},
		{"no working key", []map[string]string{{"mode": "plan"}}, false},
		{"unstamped OR", []map[string]string{st("false", ""), st("true", "")}, true},
		{"newer stamped false wins", []map[string]string{st("true", t1), st("false", t2)}, false},
		{"newer stamped true wins", []map[string]string{st("false", t1), st("true", t2)}, true},
		{"tie is working", []map[string]string{st("false", t2), st("true", t2)}, true},
		{"unstamped true still ORs", []map[string]string{st("false", t2), st("true", "")}, true},
		{"bad stamp is unstamped", []map[string]string{st("true", "soon"), st("false", t2)}, true},
	}
	for _, c := range cases {
		if got := MergeWorking(c.states...); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
