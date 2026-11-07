package codex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source/rpc"
)

// delta grows a streamed assistant or thinking item; the partial copy goes
// out at most every partialEvery.
func (a *AppServer) delta(params json.RawMessage, at time.Time, kind string) ([]harness.Item, []harness.Op) {
	var p struct {
		Delta        string `json:"delta"`
		ItemID       string `json:"itemId"`
		ThreadID     string `json:"threadId"`
		TurnID       string `json:"turnId"`
		SummaryIndex int64  `json:"summaryIndex"`
	}
	if json.Unmarshal(params, &p) != nil || p.ItemID == "" {
		return nil, nil
	}
	s := a.streams[p.ItemID]
	if s == nil {
		s = &stream{kind: kind, turn: p.TurnID}
		a.streams[p.ItemID] = s
	}
	if kind == harness.KindThinking && p.SummaryIndex != s.index && s.text != "" {
		s.text += "\n\n"
	}
	s.index = p.SummaryIndex
	s.text += p.Delta
	if !s.last.IsZero() && at.Sub(s.last) < partialEvery {
		return nil, nil
	}
	s.last = at
	started := a.started[p.ItemID]
	if started.IsZero() {
		started = at
	}
	x := harness.Item{ID: p.ItemID, TurnID: p.TurnID, Parent: a.parentOf(p.ThreadID), Kind: kind, At: started}
	body := &harness.TextBody{Text: clip(s.text), Partial: true}
	if kind == harness.KindThinking {
		x.Thinking = body
	} else {
		x.Assistant = body
	}
	return []harness.Item{x}, nil
}

// output adds streamed command output to a running tool item.
func (a *AppServer) output(params json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		ItemID string `json:"itemId"`
		Delta  string `json:"delta"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil, nil
	}
	x, ok := a.tools[p.ItemID]
	if !ok {
		return nil, nil
	}
	body := *x.Tool
	body.Output = tail(body.Output+p.Delta, harness.MaxToolOutput)
	x.Tool = &body
	a.tools[p.ItemID] = x
	s := a.streams[p.ItemID]
	if s == nil {
		s = &stream{kind: harness.KindTool}
		a.streams[p.ItemID] = s
	}
	if !s.last.IsZero() && at.Sub(s.last) < partialEvery {
		return nil, nil
	}
	s.last = at
	return []harness.Item{x}, nil
}

func planState(s string) string {
	switch s {
	case "completed":
		return harness.RowDone
	case "inProgress":
		return harness.RowActive
	}
	return harness.RowPending
}

func (a *AppServer) plan(params json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Plan     []struct {
			Step   string `json:"step"`
			Status string `json:"status"`
		} `json:"plan"`
	}
	if json.Unmarshal(params, &p) != nil || (p.ThreadID != a.main && a.main != "") {
		return nil, nil
	}
	rows := make([]harness.Row, 0, len(p.Plan))
	body := &harness.PlanBody{Total: len(p.Plan)}
	for i, s := range p.Plan {
		st := planState(s.Status)
		rows = append(rows, harness.Row{ID: "step-" + strconv.Itoa(i+1), Title: truncate(s.Step, harness.MaxTitle), State: st})
		switch st {
		case harness.RowDone:
			body.Done++
		case harness.RowActive:
			if body.Current == "" {
				body.Current = truncate(s.Step, harness.MaxTitle)
			}
		}
	}
	id := "plan-" + p.TurnID
	first, ok := a.planned[id]
	if !ok {
		first = at
		a.planned[id] = at
	}
	x := harness.Item{ID: id, TurnID: p.TurnID, Kind: harness.KindPlan, At: first, Plan: body}
	return []harness.Item{x}, []harness.Op{{Op: harness.OpReplace, Section: harness.SectionPlan, Rows: rows}}
}

func (a *AppServer) diff(params json.RawMessage) ([]harness.Item, []harness.Op) {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Diff     string `json:"diff"`
	}
	if json.Unmarshal(params, &p) != nil || (p.ThreadID != a.main && a.main != "") {
		return nil, nil
	}
	files := parseDiff(p.Diff)
	rows := make([]harness.Row, 0, len(files))
	ti := a.turns[p.TurnID]
	for _, f := range files {
		rows = append(rows, harness.Row{ID: f.Path, Title: f.Path, Subtitle: fmt.Sprintf("+%d -%d", f.Added, f.Removed),
			State: harness.StateOK, Open: &harness.Open{Kind: harness.OpenDiff, Ref: f.Path}})
		if ti != nil {
			ti.files[f.Path] = true
		}
	}
	return nil, []harness.Op{{Op: harness.OpReplace, Section: harness.SectionChanges, Rows: rows}}
}

func (a *AppServer) statusOp() harness.Op {
	s := a.status
	s.Model, s.Effort, s.Mode = a.state["model"], a.state["effort"], modeLabel(a.state["mode"])
	return harness.Op{Op: harness.OpStatus, Status: &s}
}

func modeLabel(mode string) string {
	switch mode {
	case "untrusted":
		return "Ask"
	case "on-request":
		return "Auto"
	case "never":
		return "Full access"
	}
	return mode
}

func (a *AppServer) tokens(params json.RawMessage) []harness.Op {
	var p struct {
		ThreadID   string `json:"threadId"`
		TokenUsage struct {
			Last struct {
				TotalTokens int64 `json:"totalTokens"`
			} `json:"last"`
			ModelContextWindow *int64 `json:"modelContextWindow"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(params, &p) != nil || p.ThreadID != a.main {
		return nil
	}
	if w := p.TokenUsage.ModelContextWindow; w != nil && *w > 0 {
		pct := min(100, float64(p.TokenUsage.Last.TotalTokens)*100/float64(*w))
		a.status.ContextPercent = &pct
	}
	return []harness.Op{a.statusOp()}
}

type rateWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	ResetsAt           *int64  `json:"resetsAt"`
	WindowDurationMins *int64  `json:"windowDurationMins"`
}

func windowLabel(mins *int64) string {
	switch {
	case mins == nil:
		return "Limit"
	case *mins == 10080:
		return "Weekly limit"
	case *mins%1440 == 0:
		return strconv.FormatInt(*mins/1440, 10) + " day limit"
	case *mins%60 == 0:
		return strconv.FormatInt(*mins/60, 10) + " hour limit"
	}
	return strconv.FormatInt(*mins, 10) + " minute limit"
}

func (a *AppServer) limits(params json.RawMessage) []harness.Op {
	var p struct {
		RateLimits struct {
			Primary   *rateWindow `json:"primary"`
			Secondary *rateWindow `json:"secondary"`
		} `json:"rateLimits"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil
	}
	var meters []harness.Meter
	for i, w := range []*rateWindow{p.RateLimits.Primary, p.RateLimits.Secondary} {
		if w == nil {
			continue
		}
		m := harness.Meter{ID: []string{"primary", "secondary"}[i], Label: windowLabel(w.WindowDurationMins), Percent: w.UsedPercent}
		if w.ResetsAt != nil {
			m.Resets = ptr(secs(*w.ResetsAt))
		}
		meters = append(meters, m)
	}
	a.status.Meters = meters
	return []harness.Op{a.statusOp()}
}

func (a *AppServer) threadStatus(params json.RawMessage) ([]harness.Item, []harness.Op) {
	var p struct {
		ThreadID string `json:"threadId"`
		Status   struct {
			Type string `json:"type"`
		} `json:"status"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil, nil
	}
	if t := a.threads[p.ThreadID]; t != nil && t.parent != "" {
		st := harness.StateIdle
		if p.Status.Type == "active" {
			st = harness.StateRunning
		}
		row := harness.Row{ID: p.ThreadID, State: st, Open: &harness.Open{Kind: harness.OpenChat, Ref: t.parent}}
		return nil, []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &row}}
	}
	if p.ThreadID != a.main {
		return nil, nil
	}
	switch p.Status.Type {
	case "active":
		a.state["working"], a.state["idle"] = "true", "false"
	case "idle", "systemError", "notLoaded":
		a.state["working"], a.state["idle"] = "false", "true"
	}
	return nil, nil
}

// applySettings records model, effort and approval policy from a resume
// result or a settings update.
func (a *AppServer) applySettings(model string, effort *string, policy json.RawMessage) {
	if model != "" {
		a.state["model"] = model
	}
	if effort != nil {
		a.state["effort"] = *effort
	}
	var s string
	if json.Unmarshal(policy, &s) == nil && s != "" {
		a.state["mode"] = s
	} else if len(policy) > 0 && string(policy) != "null" {
		a.state["mode"] = "custom"
	}
}

func (a *AppServer) settings(params json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		ThreadID string `json:"threadId"`
		Settings struct {
			Model          string          `json:"model"`
			Effort         *string         `json:"effort"`
			ApprovalPolicy json.RawMessage `json:"approvalPolicy"`
		} `json:"threadSettings"`
	}
	if json.Unmarshal(params, &p) != nil || p.ThreadID != a.main {
		return nil, nil
	}
	before := a.state["model"]
	a.applySettings(p.Settings.Model, p.Settings.Effort, p.Settings.ApprovalPolicy)
	var items []harness.Item
	if before != "" && p.Settings.Model != "" && before != p.Settings.Model {
		items = append(items, notice("model-"+at.Format(time.RFC3339Nano), "", at, "info", harness.NoticeModel, "Model changed to "+p.Settings.Model))
	}
	return items, []harness.Op{a.statusOp()}
}

func notice(id, turn string, at time.Time, level, code, text string) harness.Item {
	return harness.Item{ID: id, TurnID: turn, Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: level, Code: code, Text: truncate(text, 400)}}
}

func (a *AppServer) compacted(params json.RawMessage, at time.Time) []harness.Item {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
	}
	if json.Unmarshal(params, &p) != nil || p.ThreadID != a.main {
		return nil
	}
	return []harness.Item{notice("compacted-"+p.TurnID, p.TurnID, at, "info", harness.NoticeCompacted, "Context compacted")}
}

func (a *AppServer) rerouted(params json.RawMessage, at time.Time) []harness.Item {
	var p struct {
		ThreadID  string `json:"threadId"`
		TurnID    string `json:"turnId"`
		ToModel   string `json:"toModel"`
		FromModel string `json:"fromModel"`
	}
	if json.Unmarshal(params, &p) != nil || p.ToModel == "" {
		return nil
	}
	return []harness.Item{notice("rerouted-"+p.TurnID, p.TurnID, at, "info", harness.NoticeModel, "Model switched to "+p.ToModel)}
}

func (a *AppServer) errorNotice(params json.RawMessage, at time.Time) []harness.Item {
	var p struct {
		Error struct {
			Message   string          `json:"message"`
			ErrorInfo json.RawMessage `json:"codexErrorInfo"`
		} `json:"error"`
		WillRetry bool   `json:"willRetry"`
		ThreadID  string `json:"threadId"`
		TurnID    string `json:"turnId"`
	}
	if json.Unmarshal(params, &p) != nil || p.Error.Message == "" {
		return nil
	}
	level, code := "error", harness.NoticeError
	if p.WillRetry {
		level = "warning"
	}
	var info string
	if json.Unmarshal(p.Error.ErrorInfo, &info) == nil && (info == "usageLimitExceeded" || info == "rateLimited") {
		code = harness.NoticeRateLimit
	}
	x := notice("error-"+p.TurnID+"-"+strconv.FormatInt(at.UnixMilli(), 10), p.TurnID, at, level, code, p.Error.Message)
	x.Parent = a.parentOf(p.ThreadID)
	return []harness.Item{x}
}

// request handles a server request: questions become question items and
// needs rows; approval requests mark their tool waiting (the approval
// itself reaches the phone through the PermissionRequest hook).
func (a *AppServer) request(m msg, at time.Time) ([]harness.Item, []harness.Op) {
	key := rpc.RequestKey(m.ID)
	switch m.Method {
	case "item/tool/requestUserInput":
		return a.question(key, m.Params, at)
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval":
		var p struct {
			ItemID string `json:"itemId"`
		}
		if json.Unmarshal(m.Params, &p) != nil {
			return nil, nil
		}
		x, ok := a.tools[p.ItemID]
		if !ok {
			return nil, nil
		}
		body := *x.Tool
		body.State = harness.StateWaiting
		x.Tool = &body
		a.tools[p.ItemID] = x
		return []harness.Item{x}, nil
	case "mcpServer/elicitation/request":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Server   string `json:"serverName"`
		}
		json.Unmarshal(m.Params, &p)
		text := "An MCP server is asking for input in the terminal"
		if p.Server != "" {
			text = p.Server + " is asking for input in the terminal"
		}
		return []harness.Item{notice(key, p.TurnID, at, "warning", harness.NoticeMode, text)}, nil
	}
	return nil, nil
}

func (a *AppServer) question(key string, params json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		ThreadID  string `json:"threadId"`
		TurnID    string `json:"turnId"`
		Questions []struct {
			ID       string `json:"id"`
			Header   string `json:"header"`
			Question string `json:"question"`
			IsOther  bool   `json:"isOther"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.Questions) == 0 {
		return nil, nil
	}
	qs := make([]harness.Question, 0, len(p.Questions))
	for _, q := range p.Questions {
		hq := harness.Question{ID: q.ID, Header: q.Header, Prompt: q.Question, AllowText: q.IsOther || len(q.Options) == 0}
		for _, o := range q.Options {
			hq.Options = append(hq.Options, harness.Choice{ID: o.Label, Label: o.Label, Detail: o.Description})
		}
		qs = append(qs, hq)
	}
	x := harness.Item{ID: key, TurnID: p.TurnID, Parent: a.parentOf(p.ThreadID), Kind: harness.KindQuestion, At: at,
		Question: &harness.QuestionBody{Questions: qs}}
	a.tools[key] = x // kept until resolved
	title := qs[0].Header
	if title == "" {
		title = qs[0].Prompt
	}
	row := harness.Row{ID: key, Title: truncate(title, harness.MaxTitle), Subtitle: truncate(qs[0].Prompt, harness.MaxTitle),
		State: harness.StateWaiting, Started: ptr(at), Actions: []harness.ActionRef{{ID: harness.ActionAnswer, Label: "Answer", Role: harness.RolePrimary}}}
	return []harness.Item{x}, []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionNeeds, Row: &row}}
}

// resolved closes a question answered here or at the desk.
func (a *AppServer) resolved(params json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.RequestID) == 0 {
		return nil, nil
	}
	key := rpc.RequestKey(p.RequestID)
	x, ok := a.tools[key]
	if !ok || x.Question == nil {
		return nil, nil
	}
	delete(a.tools, key)
	body := *x.Question
	body.Answered = map[string][]string{}
	for _, q := range body.Questions {
		body.Answered[q.ID] = []string{}
	}
	x.Question = &body
	return []harness.Item{x}, []harness.Op{{Op: harness.OpRemove, Section: harness.SectionNeeds, ID: key}}
}

// dropQuestions closes every open question when the connection ends: the
// server requests behind them went with it, so they can no longer be
// answered here, and a later request may reuse their ids.
func (a *AppServer) dropQuestions() ([]harness.Item, []harness.Op) {
	var items []harness.Item
	var ops []harness.Op
	for _, key := range sortedKeys(a.tools) {
		x := a.tools[key]
		if x.Question == nil {
			continue
		}
		delete(a.tools, key)
		body := *x.Question
		body.Answered = map[string][]string{}
		for _, q := range body.Questions {
			body.Answered[q.ID] = []string{}
		}
		x.Question = &body
		items = append(items, x)
		ops = append(ops, harness.Op{Op: harness.OpRemove, Section: harness.SectionNeeds, ID: key})
	}
	return items, ops
}

func sortedKeys(m map[string]harness.Item) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
