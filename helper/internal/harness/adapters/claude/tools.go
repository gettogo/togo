package claude

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/harness"
)

// planTools change the plan; they show in mission control, not as steps.
var planTools = map[string]bool{"TodoWrite": true, "TaskCreate": true, "TaskUpdate": true}

// toolUse maps one tool call.
func (t *transcript) toolUse(e entry, b block, at time.Time, turn, parent string) ([]harness.Item, []harness.Op) {
	in := decodeObj(b.Input)
	if in == nil {
		in = map[string]any{}
	}
	switch {
	case planTools[b.Name]:
		return t.planUse(b, in, at, turn, parent)
	case b.Name == "AskUserQuestion":
		return []harness.Item{t.question(b, in, at, turn, parent)}, nil
	}
	title, category, subj := describeTool(b.Name, in, t.cwd)
	it := harness.Item{ID: b.ID, TurnID: turn, Parent: parent, Kind: harness.KindTool, At: at,
		Tool: &harness.ToolBody{CallID: b.ID, Name: b.Name, Title: title, Category: category, State: harness.StateRunning, Started: at}}
	if subj != nil {
		it.Tool.Subject = detect.NewSubject(subj[0], subj[1])
	}
	t.tools[b.ID] = &it
	if parent == "" && t.turn != nil && !t.turn.ended {
		t.turn.steps++
	}
	if b.Name == "Agent" || b.Name == "Task" {
		if p := str(in, "prompt"); p != "" {
			t.prompts[strings.TrimSpace(p)] = b.ID
		}
		t.roles[b.ID] = str(in, "subagent_type")
	}
	return []harness.Item{it}, nil
}

// describeTool picks a title, a category and a subject for a tool call.
func describeTool(name string, in map[string]any, cwd string) (string, string, *[2]string) {
	path := rel(cwd, firstNonEmpty(str(in, "file_path"), str(in, "notebook_path"), str(in, "path")))
	switch name {
	case "Bash":
		cmd := firstLine(str(in, "command"))
		return firstNonEmpty(cmd, str(in, "description"), "Run a command"), harness.ToolShell, &[2]string{"command", str(in, "command")}
	case "Read":
		return "Read " + path, harness.ToolRead, &[2]string{"file", path}
	case "Edit", "MultiEdit", "NotebookEdit":
		return "Edit " + path, harness.ToolEdit, &[2]string{"file", path}
	case "Write":
		return "Write " + path, harness.ToolWrite, &[2]string{"file", path}
	case "Grep":
		return "Search " + str(in, "pattern"), harness.ToolSearch, nil
	case "Glob":
		return "Find " + str(in, "pattern"), harness.ToolSearch, nil
	case "WebFetch":
		return "Fetch " + str(in, "url"), harness.ToolWeb, &[2]string{"url", str(in, "url")}
	case "WebSearch":
		return "Search the web for " + str(in, "query"), harness.ToolWeb, nil
	case "Agent", "Task":
		return firstNonEmpty(str(in, "description"), "Run an agent"), harness.ToolAgent, nil
	case "Skill":
		return "Use skill " + firstNonEmpty(str(in, "skill"), str(in, "name")), harness.ToolOther, nil
	}
	if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
		server, tool, _ := strings.Cut(rest, "__")
		return server + ": " + strings.ReplaceAll(tool, "_", " "), harness.ToolMCP, nil
	}
	return name, harness.ToolOther, nil
}

// toolResult completes a tool call with its outcome.
func (t *transcript) toolResult(e entry, b block, at time.Time) ([]harness.Item, []harness.Op) {
	it, ok := t.tools[b.ToolUseID]
	if !ok {
		return nil, nil
	}
	body := *it.Tool
	end := at
	body.Ended = &end
	body.State = harness.StateOK
	out := resultText(b.Content)
	if b.IsError {
		body.State = harness.StateFailed
		if strings.Contains(out, "doesn't want to proceed") || strings.Contains(out, "was rejected") {
			body.State = harness.StateDenied
		}
	}
	res := decodeObj(e.ToolUseResult)
	var ops []harness.Op
	var items []harness.Item
	switch body.Name {
	case "Bash":
		code, rest := exitCode(out)
		if code == nil && body.State == harness.StateOK {
			zero := 0
			code = &zero
		}
		body.ExitCode, out = code, rest
		if res != nil && str(res, "backgroundTaskId") != "" {
			bi, bo := t.background(str(res, "backgroundTaskId"), it, at)
			items, ops = append(items, bi...), append(ops, bo...)
		}
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		if fc := fileChange(res, t.cwd); fc != nil {
			body.Files = []harness.FileChange{*fc}
			ops = append(ops, t.changed(*fc)...)
			if it.Parent == "" && t.turn != nil {
				t.turn.files[fc.Path] = true
			}
		}
		if body.State == harness.StateOK {
			out = ""
		}
	case "Agent", "Task":
		ai, ao := t.agentResult(it, res, body.State, at)
		items, ops = append(items, ai...), append(ops, ao...)
		if body.State == harness.StateOK && res != nil && str(res, "status") == "async_launched" {
			out = ""
		}
	case "Read":
		if body.State == harness.StateOK {
			out = ""
		}
	}
	body.Output = strings.TrimSpace(plain(out))
	next := *it
	next.Tool = &body
	t.tools[b.ToolUseID] = &next
	return append([]harness.Item{next}, items...), ops
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

var exitLine = regexp.MustCompile(`^Exit code (\d+)\n?`)

func exitCode(out string) (*int, string) {
	m := exitLine.FindStringSubmatch(out)
	if m == nil {
		return nil, out
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return nil, out
	}
	return &n, out[len(m[0]):]
}

// fileChange counts the lines an edit added and removed.
func fileChange(res map[string]any, cwd string) *harness.FileChange {
	if res == nil {
		return nil
	}
	path := rel(cwd, str(res, "filePath"))
	if path == "" {
		return nil
	}
	fc := &harness.FileChange{Path: path}
	patches, _ := res["structuredPatch"].([]any)
	for _, p := range patches {
		lines, _ := obj(p)["lines"].([]any)
		for _, l := range lines {
			s, _ := l.(string)
			switch {
			case strings.HasPrefix(s, "+"):
				fc.Added++
			case strings.HasPrefix(s, "-"):
				fc.Removed++
			}
		}
	}
	if len(patches) == 0 && str(res, "type") == "create" {
		fc.Added = strings.Count(str(res, "content"), "\n") + 1
	}
	return fc
}

// changed keeps a running count per file for the Changes section.
func (t *transcript) changed(fc harness.FileChange) []harness.Op {
	cur, ok := t.changes[fc.Path]
	if !ok {
		cur = &harness.FileChange{Path: fc.Path}
		t.changes[fc.Path] = cur
	}
	cur.Added += fc.Added
	cur.Removed += fc.Removed
	row := harness.Row{ID: fc.Path, Title: fc.Path, Subtitle: fmt.Sprintf("+%d -%d", cur.Added, cur.Removed),
		State: harness.StateOK, Open: &harness.Open{Kind: harness.OpenDiff, Ref: fc.Path}}
	return []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionChanges, Row: &row}}
}

// question turns AskUserQuestion into a question item.
func (t *transcript) question(b block, in map[string]any, at time.Time, turn, parent string) harness.Item {
	qb := &harness.QuestionBody{Questions: []harness.Question{}}
	qs, _ := in["questions"].([]any)
	for i, q := range qs {
		m := obj(q)
		qu := harness.Question{ID: fmt.Sprintf("q%d", i+1), Header: str(m, "header"), Prompt: str(m, "question"),
			Options: []harness.Choice{}, AllowText: true}
		qu.Multi, _ = m["multiSelect"].(bool)
		opts, _ := m["options"].([]any)
		for j, o := range opts {
			om := obj(o)
			qu.Options = append(qu.Options, harness.Choice{ID: fmt.Sprintf("opt%d", j+1), Label: str(om, "label"), Detail: str(om, "description")})
		}
		qb.Questions = append(qb.Questions, qu)
	}
	return harness.Item{ID: b.ID, TurnID: turn, Parent: parent, Kind: harness.KindQuestion, At: at, Question: qb}
}

// planUse maps TodoWrite, TaskCreate and TaskUpdate onto the plan.
func (t *transcript) planUse(b block, in map[string]any, at time.Time, turn, parent string) ([]harness.Item, []harness.Op) {
	if parent != "" {
		return nil, nil
	}
	switch b.Name {
	case "TodoWrite":
		todos, _ := in["todos"].([]any)
		t.plan = t.plan[:0]
		for i, td := range todos {
			m := obj(td)
			t.plan = append(t.plan, harness.Row{ID: firstNonEmpty(str(m, "id"), strconv.Itoa(i+1)),
				Title: firstNonEmpty(str(m, "content"), str(m, "activeForm")), State: planState(str(m, "status"))})
		}
	case "TaskCreate":
		id := firstNonEmpty(str(in, "id"), strconv.Itoa(len(t.plan)+1))
		t.plan = append(t.plan, harness.Row{ID: id, Title: firstNonEmpty(str(in, "subject"), str(in, "description")), State: harness.RowPending})
	case "TaskUpdate":
		id := firstNonEmpty(str(in, "taskId"), str(in, "id"))
		for i := range t.plan {
			if t.plan[i].ID == id {
				if s := str(in, "status"); s != "" {
					t.plan[i].State = planState(s)
				}
				if s := str(in, "subject"); s != "" {
					t.plan[i].Title = s
				}
			}
		}
	}
	rows := append([]harness.Row{}, t.plan...)
	pb := &harness.PlanBody{Total: len(rows)}
	for _, r := range rows {
		switch r.State {
		case harness.RowDone, harness.RowSkipped:
			pb.Done++
		case harness.RowActive:
			if pb.Current == "" {
				pb.Current = r.Title
			}
		}
	}
	item := harness.Item{ID: "plan-" + firstNonEmpty(turn, "0"), TurnID: turn, Kind: harness.KindPlan, At: at, Plan: pb}
	return []harness.Item{item}, []harness.Op{{Op: harness.OpReplace, Section: harness.SectionPlan, Rows: rows}}
}

func planState(s string) string {
	switch s {
	case "completed", "done":
		return harness.RowDone
	case "in_progress":
		return harness.RowActive
	case "deleted", "cancelled":
		return harness.RowSkipped
	}
	return harness.RowPending
}
