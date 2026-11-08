package crush

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// db-errors.jsonl holds the rows of two real Crush 0.100.0 `crush run "hi"`
// turns that failed: one against a closed port, one asking a llama-swap
// server for a model it does not serve.
func TestErrorFinish(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "db-errors.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type want struct{ reason, text string }
	wants := map[string]want{
		"704be591-76ed-4779-ae08-83185e3e0f20": {harness.ErrProviderUnreachable, "Can't reach the provider: dial tcp 127.0.0.1:9: connect: connection refused"},
		"696cd9c9-bbf9-4773-bd25-7ee56d0af244": {harness.ErrModelNotFound, "The provider doesn't have this model: no router for requested model"},
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	byS := map[string][]string{}
	var order []string
	for sc.Scan() {
		var r messageRow
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if byS[r.Session] == nil {
			order = append(order, r.Session)
		}
		byS[r.Session] = append(byS[r.Session], sc.Text())
	}
	for _, s := range order {
		m := newDB(harness.Env{})
		m.state["harness_session"] = s
		final := map[string]harness.Item{}
		for _, row := range byS[s] {
			feedRow(m, row, final)
		}
		var notice *harness.NoticeBody
		var turn *harness.TurnBody
		for _, x := range final {
			if x.Notice != nil {
				notice = x.Notice
			}
			if x.Turn != nil {
				turn = x.Turn
			}
		}
		w := wants[s]
		if notice == nil || notice.Code != harness.NoticeError || notice.Reason != w.reason || notice.Text != w.text {
			t.Errorf("%s: notice %+v", s, notice)
		}
		if turn == nil || turn.State != harness.StateFailed || turn.Ended == nil {
			t.Errorf("%s: turn %+v", s, turn)
		}
		if m.State()["working"] == "true" {
			t.Errorf("%s: still working", s)
		}
	}
}

func TestErrorNoticeTitles(t *testing.T) {
	for _, c := range []struct{ title, details, reason, prefix string }{
		{"Unauthorized", "invalid x-api-key", harness.ErrAuth, "The provider refused the API key"},
		{"Too Many Requests", "slow down", harness.ErrRateLimit, "The provider's rate limit"},
		{"Bad Request", "prompt is too long: 210000 tokens > 200000 maximum", harness.ErrContextOverflow, "The conversation is too long"},
		{"Provider Error", "something odd", harness.ErrOther, "The provider returned an error: something odd"},
	} {
		n := errorNotice(c.title, c.details)
		if n.Reason != c.reason || !strings.HasPrefix(n.Text, c.prefix) {
			t.Errorf("%s: %+v", c.title, n)
		}
	}
}
