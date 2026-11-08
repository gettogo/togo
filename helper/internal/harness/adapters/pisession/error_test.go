package pisession

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Real Pi records: a bogus OpenAI key (401), then a model the server does
// not have (404). Each turn fails with a notice in words.
func TestProviderErrorsEndTurnWithNotice(t *testing.T) {
	f, err := os.Open("testdata/error.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m := New("pi.session", harness.Env{Cwd: "/Users/user/proj"})
	items := map[string]harness.Item{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for i := 0; sc.Scan(); i++ {
		it, _ := m.Map(harness.Record{At: time.Unix(int64(i), 0), Data: json.RawMessage(sc.Text())})
		for _, x := range it {
			items[x.ID] = x
		}
	}
	for id, want := range map[string]string{
		"5fe108f2:error": "The provider rejected the sign-in or API key: 401 Incorrect API key",
		"417a6554:error": "The model was not found: 404 no router for requested model",
	} {
		n := items[id].Notice
		if n == nil || n.Code != harness.NoticeError || !strings.HasPrefix(n.Text, want) {
			t.Errorf("%s = %+v", id, n)
		}
	}
	failed := 0
	for _, it := range items {
		if it.Kind == harness.KindTurn {
			if it.Turn.State != harness.StateFailed {
				t.Errorf("turn %+v", it.Turn)
			}
			failed++
		}
	}
	if failed != 2 {
		t.Fatalf("%d failed turns", failed)
	}
}

// An aborted reply also carries errorMessage; it is an interruption, not
// an error.
func TestAbortIsNotAnError(t *testing.T) {
	m := New("pi.session", harness.Env{Cwd: "/Users/user/proj"})
	var got []harness.Item
	for i, l := range []string{
		`{"type":"message","id":"u1","message":{"role":"user","content":"go"}}`,
		`{"type":"message","id":"a1","message":{"role":"assistant","content":[],"stopReason":"aborted","errorMessage":"Interrupted by user"}}`,
	} {
		it, _ := m.Map(harness.Record{At: time.Unix(int64(i), 0), Data: json.RawMessage(l)})
		got = append(got, it...)
	}
	for _, it := range got {
		if it.Notice != nil && it.Notice.Level == "error" {
			t.Fatalf("error notice for an abort: %+v", it.Notice)
		}
		if it.Kind == harness.KindTurn && it.Turn.State == harness.StateFailed {
			t.Fatalf("failed turn for an abort")
		}
	}
}
