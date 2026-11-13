package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func TestOpenNamesTheCoveredHistoryInterval(t *testing.T) {
	const id = "s_interval01"
	e, _ := newFake(t, id, "1.2.3")
	for i := 0; i < 250; i++ {
		e.log.Append(id, []harness.Item{item(itoa(int64(i)))})
	}
	sub := &fakeSub{done: make(chan struct{})}
	defer close(sub.done)
	res, err := e.Open(context.Background(), sub, id, 10, 200)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if fields["coveredAfter"] != float64(50) {
		t.Fatalf("response advances to %d without identifying the omitted interval: coveredAfter=%v", res.Cursor, fields["coveredAfter"])
	}
}

func TestHistoryCoverageDistinguishesReplacementAndEviction(t *testing.T) {
	const id = "s_interval02"
	e, _ := newFake(t, id, "1.2.3")
	for i := 0; i < 10; i++ {
		e.log.Append(id, []harness.Item{item(itoa(int64(i)))})
	}
	for i := 0; i < 100; i++ {
		e.log.Append(id, []harness.Item{item("9")})
	}
	sub := &fakeSub{done: make(chan struct{})}
	defer close(sub.done)
	res, err := e.Open(context.Background(), sub, id, 10, 200)
	if err != nil || res.CoveredAfter == nil || *res.CoveredAfter != 10 || *res.DiscardedThrough != 0 {
		t.Fatalf("replacement was treated as a lost interval: %+v, %v", res, err)
	}
	e.log.Max = 20
	for i := 0; i < 50; i++ {
		e.log.Append(id, []harness.Item{item("new" + itoa(int64(i)))})
	}
	res, err = e.Open(context.Background(), sub, id, 10, 200)
	if err != nil || res.DiscardedThrough == nil || *res.DiscardedThrough <= 10 {
		t.Fatalf("ring eviction was not reported: %+v, %v", res, err)
	}
}

func TestByteCappedOpenNamesItsCoveredInterval(t *testing.T) {
	const id = "s_interval03"
	e, _ := newFake(t, id, "1.2.3")
	for i := 0; i < 5; i++ {
		it := item(itoa(int64(i)))
		it.User = &harness.UserBody{Text: strings.Repeat("x", 128)}
		e.log.Append(id, []harness.Item{it})
	}
	previous := pageBytes
	pageBytes = 64
	t.Cleanup(func() { pageBytes = previous })
	sub := &fakeSub{done: make(chan struct{})}
	defer close(sub.done)
	res, err := e.Open(context.Background(), sub, id, 1, 200)
	if err != nil || len(res.Items) != 1 || res.CoveredAfter == nil || *res.CoveredAfter != 4 {
		t.Fatalf("byte truncation lost its interval: %+v, %v", res, err)
	}
}
