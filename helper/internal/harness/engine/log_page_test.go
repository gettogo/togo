package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// busyLog holds a conversation of `main` messages, each followed by `per`
// steps of a subagent (items with a parent), as a session with busy
// subagents does. The main chat shows only the messages.
func busyLog(main, per int) *Log {
	l := &Log{NewEpoch: func() string { return "e1" }}
	for m := 0; m < main; m++ {
		items := []harness.Item{{ID: fmt.Sprintf("m%d", m), Kind: harness.KindAssistant}}
		for c := 0; c < per; c++ {
			items = append(items, harness.Item{ID: fmt.Sprintf("m%d-c%d", m, c), Kind: harness.KindTool, Parent: "agent-1"})
		}
		l.Append("s", items)
	}
	return l
}

func shown(items []harness.Item) int {
	n := 0
	for _, it := range items {
		if it.Parent == "" {
			n++
		}
	}
	return n
}

// Seen on a two-day session: of the newest 200 items about 190 were a
// subagent's steps, so the chat opened on a handful of lines and Show
// Earlier had to be tapped again and again. A page is a page of what the
// chat shows; the steps between come with it.
func TestAHistoryPageCountsWhatTheChatShows(t *testing.T) {
	l := busyLog(30, 19) // 600 items, 30 of them shown
	page, more := l.Before("s", 1<<40, 10)
	if got := shown(page); got != 10 || !more {
		t.Fatalf("newest page shows %d items (of %d), more %v; want 10, true", got, len(page), more)
	}
	if page[0].ID != "m20" || page[len(page)-1].ID != "m29-c18" {
		t.Fatalf("page runs %s to %s; want m20 to m29-c18", page[0].ID, page[len(page)-1].ID)
	}
	// The next page continues where this one began, with nothing skipped or repeated.
	older, more := l.Before("s", page[0].Seq, 10)
	if got := shown(older); got != 10 || !more || older[0].ID != "m10" || older[len(older)-1].Seq != page[0].Seq-1 {
		t.Fatalf("second page: %d shown, more %v, %s to seq %d (first page began at %d)", got, more, older[0].ID, older[len(older)-1].Seq, page[0].Seq)
	}
	oldest, more := l.Before("s", older[0].Seq, 10)
	if shown(oldest) != 10 || more || oldest[0].ID != "m0" {
		t.Fatalf("last page: %d shown, more %v, starts %s", shown(oldest), more, oldest[0].ID)
	}
	// A conversation with no subagent steps pages exactly as before.
	plain := busyLog(30, 0)
	if p, more := plain.Before("s", 1<<40, 10); len(p) != 10 || !more || p[0].ID != "m20" {
		t.Fatalf("plain log: %d items, more %v, first %s", len(p), more, p[0].ID)
	}
	// No limit: everything.
	if p, more := l.Before("s", 1<<40, 0); len(p) != 600 || more {
		t.Fatalf("no limit: %d items, more %v", len(p), more)
	}
}

// Steps that come after the last message (a subagent still working) belong
// to the newest page; they do not use up its count.
func TestTrailingSubagentStepsDoNotEmptyThePage(t *testing.T) {
	l := busyLog(5, 0)
	var steps []harness.Item
	for c := 0; c < 400; c++ {
		steps = append(steps, harness.Item{ID: fmt.Sprintf("late-%d", c), Kind: harness.KindTool, Parent: "agent-1"})
	}
	l.Append("s", steps)
	page, more := l.Before("s", 1<<40, 3)
	if shown(page) != 3 || !more || page[0].ID != "m2" || len(page) != 403 {
		t.Fatalf("page: %d items, %d shown, more %v, first %s", len(page), shown(page), more, page[0].ID)
	}
}

func TestOpenCountsWhatTheChatShows(t *testing.T) {
	if got := pageStart(busyLog(30, 19).get("s").items, 10); got != 400 {
		t.Fatalf("pageStart = %d, want 400", got)
	}
}

// Seen on the same session after a helper restart: every subagent file is
// read after the conversation, so the newest part of the log is subagent
// steps only, and large ones. The size limit of a page then kept those and
// dropped the whole conversation. What the chat shows is kept first; the
// newest subagent steps fill what room is left.
func TestThePageSizeLimitKeepsTheConversationFirst(t *testing.T) {
	var items []harness.Item
	for m := 0; m < 40; m++ {
		items = append(items, harness.Item{ID: fmt.Sprintf("m%d", m), Seq: int64(m + 1), Kind: harness.KindAssistant,
			Assistant: &harness.TextBody{Text: "a reply"}})
	}
	big := strings.Repeat("x", 4000)
	for c := 0; c < 300; c++ {
		items = append(items, harness.Item{ID: fmt.Sprintf("c%d", c), Seq: int64(41 + c), Kind: harness.KindThinking, Parent: "agent-1",
			Thinking: &harness.TextBody{Text: big}})
	}
	max := 200 << 10 // room for about 50 of the 300 steps
	page, cut := fitPage(items, max)
	if !cut || shown(page) != 40 {
		t.Fatalf("page keeps %d of 40 conversation items (cut %v)", shown(page), cut)
	}
	size := 0
	for i, it := range page {
		size += itemSize(it)
		if i > 0 && page[i-1].Seq >= it.Seq {
			t.Fatalf("page is out of order at %s", it.ID)
		}
	}
	steps := len(page) - 40
	if size > max || steps < 30 || steps > 60 || page[len(page)-1].ID != "c299" {
		t.Fatalf("page is %d bytes with %d steps, last %s", size, steps, page[len(page)-1].ID)
	}
	// Everything fits: nothing is cut or reordered.
	if all, cut := fitPage(items[:60], 1<<20); cut || len(all) != 60 {
		t.Fatalf("a page that fits: %d items, cut %v", len(all), cut)
	}
	// The conversation alone is too large: its newest part is kept, as before.
	var long []harness.Item
	for m := 0; m < 100; m++ {
		long = append(long, harness.Item{ID: fmt.Sprintf("m%d", m), Seq: int64(m + 1), Kind: harness.KindAssistant, Assistant: &harness.TextBody{Text: big}})
	}
	kept, cut := fitPage(long, max)
	if !cut || len(kept) == 0 || len(kept) > 55 || kept[len(kept)-1].ID != "m99" {
		t.Fatalf("long conversation: %d kept, cut %v", len(kept), cut)
	}
}
