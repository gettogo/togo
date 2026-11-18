package surface

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func apc(s string) []byte { return []byte("\x1b_tsp;" + s + "\x1b\\") }

func TestParserReassemblesEveryByteBoundary(t *testing.T) {
	want := `{"sf":"s:1","s":4,"ops":[["set","o",{"placeholder":"Search all sessions…","empty":"No sessions yet; x=1;y=2"}]]}`
	data := apc("f;" + want)
	for at := 0; at <= len(data); at++ {
		var p Parser
		a, ea := p.Feed(data[:at], time.Time{})
		b, eb := p.Feed(data[at:], time.Time{})
		got := append(a, b...)
		if len(ea)+len(eb) != 0 || len(got) != 1 || got[0].Verb != 'f' || string(got[0].Body) != want {
			t.Fatalf("split %d: messages=%v errors=%v %v", at, got, ea, eb)
		}
	}
}

func TestParserIgnoresOtherTerminalStrings(t *testing.T) {
	var p Parser
	data := []byte("ordinary tsp;f;{}\x1b]2;title\x07\x1bPignored\x1b_tsp;f;{}\x1b\\\x1b_hello\x1b\\")
	data = append(data, apc(`q;{"q":"hello","v":[1]}`)...)
	got, errs := p.Feed(data, time.Time{})
	if len(errs) != 0 || len(got) != 1 || got[0].Verb != 'q' {
		t.Fatalf("messages=%v errors=%v", got, errs)
	}
}

func TestParserRecoversAfterCancelledAndOversizedStrings(t *testing.T) {
	var p Parser
	data := append([]byte("\x1b_tsp;f;{\x18"), apc("f;"+strings.Repeat("x", MaxStringBytes+1))...)
	data = append(data, apc(`q;{"q":"hello","v":[1]}`)...)
	got, errs := p.Feed(data, time.Time{})
	if len(errs) != 1 || len(got) != 1 || got[0].Verb != 'q' || cap(p.body) > MaxStringBytes*2 {
		t.Fatalf("messages=%v errors=%v capacity=%d", got, errs, cap(p.body))
	}
}

func TestParserJoinsChunksSplitInsideUTF8(t *testing.T) {
	var p Parser
	whole := []byte(`{"text":"界=1; hello"}`)
	at := bytes.Index(whole, []byte("界")) + 1
	data := apc("f;c=chunk1;m=1;" + string(whole[:at]))
	data = append(data, apc("f;c=chunk1;"+string(whole[at:]))...)
	got, errs := p.Feed(data, time.Time{})
	if len(errs) != 0 || len(got) != 1 || !bytes.Equal(got[0].Body, whole) || !json.Valid(got[0].Body) {
		t.Fatalf("messages=%v errors=%v", got, errs)
	}
}

func TestParserChunkKeepsFirstParameters(t *testing.T) {
	var p Parser
	data := apc("b;c=a;m=1;mime=image/png;YWJj")
	data = append(data, apc("b;c=a;mime=text/plain;ZA==")...)
	got, errs := p.Feed(data, time.Time{})
	if len(errs) != 0 || len(got) != 1 || string(got[0].Body) != "YWJjZA==" || got[0].Params["mime"] != "image/png" {
		t.Fatalf("messages=%v errors=%v", got, errs)
	}
}

func TestParserInterruptedAndExpiredChunksDoNotBlockNewMessages(t *testing.T) {
	for _, expire := range []bool{false, true} {
		var p Parser
		now := time.Unix(10, 0)
		p.Feed(apc(`f;c=a;m=1;{"sf":`), now)
		if expire {
			now = now.Add(ChunkTimeout + time.Millisecond)
		}
		got, errs := p.Feed(apc(`q;{"q":"hello"}`), now)
		if len(errs) != 1 || len(got) != 1 || got[0].Verb != 'q' {
			t.Fatalf("expire=%v messages=%v errors=%v", expire, got, errs)
		}
	}
}

func TestParserDropsOverLimitChunksThroughTheirLastPart(t *testing.T) {
	var p Parser
	now := time.Unix(10, 0)
	part := strings.Repeat("a", MaxAPCBytes-128)
	var errs []error
	for n := 0; n <= MaxMessageBytes/len(part); n++ {
		got, e := p.Feed(apc("f;c=big;m=1;"+part), now)
		errs = append(errs, e...)
		if len(got) != 0 {
			t.Fatal("emitted an unfinished oversized message")
		}
	}
	got, e := p.Feed(append(apc("f;c=big;end"), apc(`q;{"q":"hello"}`)...), now)
	errs = append(errs, e...)
	if len(errs) != 1 || len(got) != 1 || got[0].Verb != 'q' || len(p.chunks.body) != 0 {
		t.Fatalf("messages=%v errors=%v retained=%d", got, errs, len(p.chunks.body))
	}
}

func TestEncodeRejectsOversizedInputWithoutEmittingBytes(t *testing.T) {
	got, err := Encode('e', map[string]any{"ev": "edit", "text": strings.Repeat("x", MaxAPCBytes)})
	if err == nil || len(got) != 0 {
		t.Fatal("oversized terminal input accepted")
	}
	got, err = Encode('e', map[string]any{"ev": "action", "act": "semi;equals=界"})
	if err != nil {
		t.Fatal(err)
	}
	var p Parser
	msgs, errs := p.Feed(got, time.Time{})
	if len(errs) != 0 || len(msgs) != 1 || !json.Valid(msgs[0].Body) {
		t.Fatalf("encoded input did not round trip: %v %v", msgs, errs)
	}
}
