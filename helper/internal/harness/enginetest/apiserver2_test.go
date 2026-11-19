package enginetest

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// keysOf flattens a JSON value to its object paths, list elements named
// "[]" and a message's content parts by their type.
func keysOf(v any, at string, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			keysOf(e, at+"."+k, out)
		}
	case []any:
		for _, e := range x {
			name := "[]"
			if m, ok := e.(map[string]any); ok && strings.HasSuffix(at, "content") {
				name = "[" + m["type"].(string) + "]"
			}
			keysOf(e, at+name, out)
		}
	default:
		out[at] = true
	}
}

func recorded(t *testing.T, name string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(sourceDir(), "harness", "adapters", "opencode", "testdata", "v2", name))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	keysOf(v, "", out)
	return out
}

// The 2.x server answers its routes only with fields the recorded answers
// have: the messages, a child session and the running shells are built
// from the journal in the shape of messages.json, children.json and
// shells.json. Paging follows the cursor to the start and no further.
func TestServer2AnswersAsRecorded(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	p := &sessiongen.OpenCode2{}
	turns := make([]sessiongen.Turn, 30)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 30, Tools: 1, Output: 20}
	}
	turns[1].Agents = &sessiongen.Batch{Size: 1, Steps: 1}
	turns[29].Jobs = 1
	if _, err := sessiongen.Write(home, p, sessiongen.Script{Seed: 3, Start: time.Now().UTC(), Cwd: cwd, Turns: turns}); err != nil {
		t.Fatal(err)
	}
	s := newAPIServer("opencode", home)
	s.v2 = newV2State()
	get := func(path string) any {
		t.Helper()
		u, _ := url.Parse(path)
		st, body := s.route2("GET", u, nil)
		if st != 200 {
			t.Fatalf("%s: %d", path, st)
		}
		var v any
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	check := func(what string, v any, want map[string]bool) {
		t.Helper()
		have := map[string]bool{}
		keysOf(v, "", have)
		var extra []string
		for k := range have {
			if !want[k] {
				extra = append(extra, k)
			}
		}
		sort.Strings(extra)
		if len(extra) > 0 {
			t.Errorf("%s has fields no recording has: %v", what, extra)
		}
	}
	main := p.Session()
	var all []any
	path := "/api/session/" + main + "/message?limit=50&order=desc"
	for pages := 0; ; pages++ {
		page := get(path).(map[string]any)
		check("a messages page", page, recorded(t, "messages.json"))
		data := page["data"].([]any)
		all = append(all, data...)
		next, _ := page["cursor"].(map[string]any)["next"].(string)
		if len(data) == 0 {
			if next != "" {
				t.Fatal("the page past the start names a next page")
			}
			break
		}
		if pages > 10 || next == "" {
			t.Fatalf("paging stopped at page %d with cursor %q", pages, next)
		}
		path = "/api/session/" + main + "/message?cursor=" + next + "&limit=50"
	}
	users := 0
	for i, m := range all {
		mm := m.(map[string]any)
		if mm["type"] == "user" {
			users++
		}
		if i > 0 && mm["id"].(string) >= all[i-1].(map[string]any)["id"].(string) {
			t.Fatalf("messages are not newest first at %d", i)
		}
	}
	if users != 30 {
		t.Errorf("%d prompts across the pages, want 30", users)
	}
	children := get("/api/session?parentID=" + main)
	check("children", children, recorded(t, "children.json"))
	if n := len(children.(map[string]any)["data"].([]any)); n != 1 {
		t.Errorf("%d children listed, want 1", n)
	}
	check("shells", get("/api/shell"), recorded(t, "shells.json"))
	if n := len(get("/api/shell").(map[string]any)["data"].([]any)); n != 1 {
		t.Errorf("%d shells listed, want the one running", n)
	}
}
