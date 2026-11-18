package claude

import (
	"encoding/json"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A prompt that starts with markup is still a prompt: only the wrapper tags
// Claude Code writes itself are skipped.
func TestPromptStartingWithMarkupOpensATurn(t *testing.T) {
	for i, text := range []string{
		"<div>\n  <p>hi</p>\n</div>\nwhy is this centered?",
		"<template><ul></ul></template> fix it",
		"<script>x()</script> is this safe?",
		"<details>a</details>",
	} {
		tr := newTranscript(env())
		body, _ := json.Marshal(text)
		r := mapAll(tr, []harness.Record{trRec(`{"type":"user","message":{"role":"user","content":` + string(body) + `},"uuid":"u` + string(rune('1'+i)) + `","timestamp":"2026-11-03T13:00:00.100Z","sessionId":"s2"}`)})
		if len(r.items) == 0 {
			t.Errorf("%q: no user item", text)
			continue
		}
		if !harness.MergeWorking(tr.State()) {
			t.Errorf("%q: no turn opened", text)
		}
	}
}

func TestWrapperTagsStaySkipped(t *testing.T) {
	for _, text := range []string{
		"<system-reminder>x</system-reminder>",
		"<local-command-caveat>x</local-command-caveat>",
		"<bash-input>ls</bash-input>",
		"<bash-stdout>a</bash-stdout><bash-stderr></bash-stderr>",
		"<command-message>init</command-message>",
	} {
		tr := newTranscript(env())
		body, _ := json.Marshal(text)
		r := mapAll(tr, []harness.Record{trRec(`{"type":"user","message":{"role":"user","content":` + string(body) + `},"uuid":"w1","timestamp":"2026-11-03T13:00:00.100Z","sessionId":"s2"}`)})
		if len(r.items) != 0 {
			t.Errorf("%q: made %d items", text, len(r.items))
		}
	}
}
