package tmux

import (
	"reflect"
	"testing"
	"time"
)

// record builds a well-formed list-sessions record: the 12 fields joined by
// the field separator, terminated by the record delimiter.
func record(fields ...string) string {
	var out string
	for i, f := range fields {
		if i > 0 {
			out += fieldSep
		}
		out += f
	}
	return out + recSep + "\n"
}

func TestParseListOutputRawNewlineInName(t *testing.T) {
	// A raw newline inside the session name field must stay inside the
	// field: records are delimited by "RecSep", not by "\n" alone. This is
	// the input shape the old line-based splitting could not handle.
	out := record("my\nwork", "1700000000", "0", "0", "/tmp", "b", "a", "t", "c", "0", "", "")
	sessions, err := parseListOutput(out)
	if err != nil {
		t.Fatalf("parseListOutput: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("parseListOutput returned %d sessions, want exactly one", len(sessions))
	}
	if sessions[0].Name != "my\nwork" {
		t.Errorf("Name = %q, want the raw %q", sessions[0].Name, "my\nwork")
	}
}

func TestParseListOutputRawNewlineInOption(t *testing.T) {
	out := record("s", "1700000000", "0", "0", "/tmp", "b", "a", "a\nb", "c", "0", "", "")
	sessions, err := parseListOutput(out)
	if err != nil {
		t.Fatalf("parseListOutput: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Title != "a\nb" {
		t.Fatalf("parseListOutput = %+v, want one session with Title %q", sessions, "a\nb")
	}
}

func TestParseListOutputSkipsMalformedRecord(t *testing.T) {
	// A record without exactly 12 fields is skipped, not an error; the
	// following well-formed record must still be returned.
	out := "broken" + fieldSep + "record" + recSep + "\n" + record("ok", "1700000000", "1", "0", "/tmp", "b", "a", "t", "c", "0", "", "")
	sessions, err := parseListOutput(out)
	if err != nil {
		t.Fatalf("parseListOutput: %v", err)
	}
	want := []SessionInfo{{
		Name:       "ok",
		Created:    time.Unix(1700000000, 0),
		Attached:   1,
		Path:       "/tmp",
		Bib:        "b",
		Agent:      "a",
		Title:      "t",
		CreatedTag: "c",
	}}
	if !reflect.DeepEqual(sessions, want) {
		t.Fatalf("parseListOutput = %+v, want %+v", sessions, want)
	}
}

func TestParseListOutputReadsArgv(t *testing.T) {
	out := record("s", "1700000000", "0", "0", "/tmp", "01", "claude", "t", "c", "0", `["claude","a\u001fb"]`, "")
	sessions, err := parseListOutput(out)
	if err != nil {
		t.Fatalf("parseListOutput: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Argv != `["claude","a\u001fb"]` {
		t.Fatalf("parseListOutput = %+v, want the argv option", sessions)
	}
}

func TestParseListOutputEmpty(t *testing.T) {
	sessions, err := parseListOutput("")
	if err != nil {
		t.Fatalf("parseListOutput: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("parseListOutput(\"\") = %+v, want empty", sessions)
	}
}

func TestParseListOutputBadCreated(t *testing.T) {
	out := record("s", "notanumber", "0", "0", "/tmp", "b", "a", "t", "c", "0", "", "")
	if _, err := parseListOutput(out); err == nil {
		t.Fatal("parseListOutput with a non-numeric session_created = nil error, want an error")
	}
}

func TestParseListOutputReadsMuted(t *testing.T) {
	out := record("on", "1700000000", "0", "0", "/tmp", "01", "a", "t", "c", "1", "", "") +
		record("off", "1700000000", "0", "0", "/tmp", "02", "a", "t", "c", "", "", "")
	sessions, err := parseListOutput(out)
	if err != nil {
		t.Fatalf("parseListOutput: %v", err)
	}
	if len(sessions) != 2 || !sessions[0].Muted || sessions[1].Muted {
		t.Fatalf("parseListOutput = %+v, want only the first muted", sessions)
	}
}

func TestParseListOutputReadsExit(t *testing.T) {
	out := record("s", "1700000000", "0", "0", "/tmp", "01", "claude", "t", "c", "0", "", `{"status":"ended"}`)
	sessions, err := parseListOutput(out)
	if err != nil {
		t.Fatalf("parseListOutput: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Exit != `{"status":"ended"}` {
		t.Fatalf("parseListOutput = %+v, want the exit option", sessions)
	}
}
