package cline

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/wire"
)

// toolTitle names Cline's own tools (read_files, run_commands, editor,
// apply_patch, search_codebase, fetch_web_content) from their inputs, and
// any other tool the shared way.
func toolTitle(name string, input json.RawMessage) (string, string, *wire.Subject) {
	var in struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
		Commands json.RawMessage `json:"commands"`
		Path     string          `json:"path"`
		OldText  *string         `json:"old_text"`
		Input    string          `json:"input"`
		Queries  []string        `json:"queries"`
		Requests []struct {
			URL string `json:"url"`
		} `json:"requests"`
	}
	_ = json.Unmarshal(input, &in)
	more := func(first string, n int) string {
		if n > 1 {
			return first + " +" + strconv.Itoa(n-1)
		}
		return first
	}
	file := func(p string) *wire.Subject {
		return &wire.Subject{Kind: wire.SubjectFile, Text: hookstyle.Truncate(p, wire.MaxSubjectText)}
	}
	switch name {
	case "read_files":
		if len(in.Files) > 0 && in.Files[0].Path != "" {
			return "Read " + more(in.Files[0].Path, len(in.Files)), harness.ToolRead, file(in.Files[0].Path)
		}
	case "run_commands":
		var cmds []string
		if json.Unmarshal(in.Commands, &cmds) != nil {
			var one string
			if json.Unmarshal(in.Commands, &one) == nil && one != "" {
				cmds = []string{one}
			}
		}
		if len(cmds) > 0 {
			c := strings.Join(cmds, "\n")
			return strings.Join(cmds, "; "), harness.ToolShell, &wire.Subject{Kind: wire.SubjectCommand, Text: hookstyle.Truncate(c, wire.MaxSubjectText)}
		}
	case "editor":
		if in.Path != "" {
			if in.OldText == nil {
				return "Write " + in.Path, harness.ToolWrite, file(in.Path)
			}
			return "Edit " + in.Path, harness.ToolEdit, file(in.Path)
		}
	case "apply_patch":
		for _, l := range strings.Split(in.Input, "\n") {
			for _, p := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: "} {
				if f, ok := strings.CutPrefix(l, p); ok && strings.TrimSpace(f) != "" {
					f = strings.TrimSpace(f)
					return "Edit " + f, harness.ToolEdit, file(f)
				}
			}
		}
		return "Apply patch", harness.ToolEdit, nil
	case "search_codebase":
		if len(in.Queries) > 0 {
			return "Search " + more(in.Queries[0], len(in.Queries)), harness.ToolSearch, nil
		}
	case "fetch_web_content":
		if len(in.Requests) > 0 && in.Requests[0].URL != "" {
			u := in.Requests[0].URL
			return more(u, len(in.Requests)), harness.ToolWeb, &wire.Subject{Kind: wire.SubjectURL, Text: hookstyle.Truncate(u, wire.MaxSubjectText)}
		}
	}
	return hookstyle.ToolTitle(name, input)
}

// modelMissing is how OpenAI-compatible servers refuse a model name the
// shared classifier does not know ("no router for requested model").
var modelMissing = regexp.MustCompile(`(?i)\bno (router|provider|endpoints?|deployment) (found )?for (the )?(requested )?model\b|\bmodel\b.{0,40}\bnot (found|exist)`)

// errorText reads a terminal error Cline recorded: its reason, and a short
// sentence that leads with what it means. Cline repeats a fetch failure's
// message after its error class ("Cannot connect to API: Unable to
// connect. Is the computer able to access the url?: TypeError: Unable to
// connect. … (ConnectionRefused)"); the repeat is dropped.
func errorText(raw string) (string, string) {
	text := strings.TrimSpace(raw)
	for _, sep := range []string{": TypeError: ", ": Error: ", ": APIError: "} {
		if i := strings.Index(text, sep); i >= 0 {
			tail := text[i+len(sep):]
			text = strings.TrimSpace(text[:i])
			if j := strings.LastIndex(tail, " ("); j >= 0 && strings.HasSuffix(tail, ")") {
				text += tail[j:] // the error code, e.g. (ConnectionRefused)
			}
		}
	}
	reason := harness.ClassifyError(raw)
	if reason == harness.ErrOther && modelMissing.MatchString(raw) {
		reason = harness.ErrModelNotFound
	}
	lead := map[string]string{
		harness.ErrAuth:                "The provider refused the API key or sign-in.",
		harness.ErrProviderUnreachable: "Can't reach the model provider.",
		harness.ErrRateLimit:           "Rate limit or quota reached.",
		harness.ErrModelNotFound:       "The provider doesn't have this model.",
		harness.ErrContextOverflow:     "The conversation is too long for the model.",
	}[reason]
	switch {
	case lead == "":
		return reason, text
	case text == "":
		return reason, lead
	}
	return reason, lead + " " + text
}

var (
	userInput  = regexp.MustCompile(`(?s)^\s*<user_input(?: mode="([a-z]+)")?>(.*)</user_input>\s*$`)
	modeNotice = regexp.MustCompile(`(?s)<mode_notice>.*?</mode_notice>`)
)

// userText unwraps the <user_input mode="act"> envelope Cline stores a
// typed prompt in, returning the prompt and the mode it was sent in. The
// <mode_notice> Cline adds after a Tab between Plan and Act is for the
// model only.
func userText(s string) (string, string) {
	if m := userInput.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(modeNotice.ReplaceAllString(m[2], "")), m[1]
	}
	return strings.TrimSpace(s), ""
}

// resultText reads a tool result's content: a string (itself often the
// JSON of {query, result, success} or a list of them), a list of text
// parts, or else its JSON. failed is true when an entry says success false
// or carries an error.
func resultText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		t := strings.TrimSpace(s)
		if (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) && json.Valid([]byte(t)) {
			if out, failed := resultText(json.RawMessage(t)); out != "" {
				return out, failed
			}
		}
		return s, false
	}
	type entry struct {
		Text    string          `json:"text"`
		Result  json.RawMessage `json:"result"`
		Error   string          `json:"error"`
		Success *bool           `json:"success"`
	}
	var list []entry
	if json.Unmarshal(raw, &list) != nil {
		var one entry
		if json.Unmarshal(raw, &one) == nil && (one.Text != "" || len(one.Result) > 0 || one.Error != "") {
			list = []entry{one}
		}
	}
	var out []string
	failed := false
	for _, p := range list {
		if p.Error != "" || (p.Success != nil && !*p.Success) {
			failed = true
		}
		switch {
		case p.Text != "":
			out = append(out, p.Text)
		case len(p.Result) > 0:
			r, f := resultText(p.Result)
			failed = failed || f
			out = append(out, r)
		case p.Error != "":
			out = append(out, p.Error)
		}
	}
	if len(out) > 0 {
		return strings.Join(out, "\n"), failed
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) == nil {
		return buf.String(), false
	}
	return string(raw), false
}
