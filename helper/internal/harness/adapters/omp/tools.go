package omp

import (
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// toolInfo names a tool call: category, title, subject and the file it
// changes, from its name and arguments.
type toolInfo struct {
	category string
	title    string
	subject  *wire.Subject
	file     string
}

func firstArg(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(args, k); s != "" {
			return s
		}
	}
	return ""
}

func describeTool(name string, args map[string]any, cwd string) toolInfo {
	n := strings.ToLower(name)
	path := rel(cwd, firstArg(args, "path", "file_path", "filePath", "file", "notebook_path"))
	cmd := firstLine(firstArg(args, "command", "cmd", "code", "script"))
	pattern := firstArg(args, "pattern", "query", "glob", "regex")
	url := firstArg(args, "url", "uri")
	info := toolInfo{category: harness.ToolOther, title: name}
	switch {
	case n == "task" || strings.HasPrefix(n, "subagent") || n == "agent":
		info.category = harness.ToolAgent
		info.title = "Run subagents"
		if d := firstArg(args, "description", "agent"); d != "" {
			info.title = "Run " + d
		}
	case n == "read" || n == "read_file" || n == "view" || n == "cat":
		info.category, info.title = harness.ToolRead, join("Read", path)
	case n == "write" || n == "write_file" || n == "create":
		info.category, info.title, info.file = harness.ToolWrite, join("Write", path), path
	case strings.Contains(n, "edit") || strings.Contains(n, "patch") || n == "replace":
		info.category, info.title, info.file = harness.ToolEdit, join("Edit", path), path
	case n == "bash" || n == "shell" || n == "exec" || n == "python" || n == "run":
		info.category, info.title = harness.ToolShell, join("Run", cmd)
		if cmd != "" {
			info.subject = &wire.Subject{Kind: wire.SubjectCommand, Text: truncate(cmd, harness.MaxTitle)}
		}
	case n == "grep" || n == "find" || n == "glob" || n == "ls" || n == "search" || strings.HasSuffix(n, "_search") && !strings.HasPrefix(n, "web"):
		info.category, info.title = harness.ToolSearch, join("Search", firstNonEmpty(pattern, path))
	case strings.HasPrefix(n, "web") || n == "fetch" || strings.Contains(n, "browser"):
		info.category, info.title = harness.ToolWeb, join("Fetch", firstNonEmpty(url, pattern))
		if n == "web_search" {
			info.title = join("Search the web", pattern)
		}
	case strings.Contains(n, "todo"):
		info.title = "Update the plan"
	case strings.HasPrefix(n, "mcp") || strings.Contains(n, "__"):
		info.category = harness.ToolMCP
	}
	if info.subject == nil && path != "" {
		info.subject = &wire.Subject{Kind: wire.SubjectFile, Text: truncate(path, harness.MaxTitle)}
	}
	info.title = truncate(info.title, harness.MaxTitle)
	return info
}

func join(verb, what string) string {
	if what == "" {
		return verb
	}
	return verb + " " + what
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// todoRows reads a todo tool's arguments into plan rows: a list under
// todos, items or phases[].tasks, each with content (or text, title) and
// status (pending, in_progress, completed).
func todoRows(args map[string]any) []harness.Row {
	var list []any
	for _, k := range []string{"todos", "items", "tasks"} {
		if l := asList(args[k]); len(l) > 0 {
			list = l
			break
		}
	}
	for _, ph := range asList(args["phases"]) {
		list = append(list, asList(obj(ph)["tasks"])...)
	}
	var rows []harness.Row
	for i, el := range list {
		m := obj(el)
		title := firstArg(m, "content", "text", "title", "description")
		if title == "" {
			continue
		}
		id := firstArg(m, "id")
		if id == "" {
			id = "todo-" + itoa(i+1)
		}
		rows = append(rows, harness.Row{ID: id, Title: truncate(title, harness.MaxTitle), State: rowState(str(m, "status"))})
	}
	return rows
}

func rowState(s string) string {
	switch strings.ToLower(s) {
	case "in_progress", "active", "doing", "running":
		return harness.RowActive
	case "completed", "done", "complete":
		return harness.RowDone
	case "skipped", "cancelled", "abandoned":
		return harness.RowSkipped
	}
	return harness.RowPending
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
