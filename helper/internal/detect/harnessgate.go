package detect

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Prompts of Vibe, Copilot CLI, Cline, Cursor CLI and Droid whose roles or
// keys the generic shapes get wrong, read from real captures
// (adapters/<agent>/testdata/screens):
//
//   - Vibe's tool permission (harnessask.go's box): digits 1-4, Allow once
//     approve, for this session and always approve_always, Deny deny.
//     Its questions are picked with their digit alone.
//   - Copilot's plan review: a round box titled "Plan Ready for Review",
//     numbered actions that wrap, picked by digit.
//   - Cline's permission: "Cline needs permission", "Approve tool
//     call?", the tool and its preview, then "[y] Approve   [n] Deny".
//   - Cursor's AskQuestion ("Question N of M", "› [ ] Label", Space then
//     Enter), its plan "Ready to build?" and "Run this command?", each
//     row naming its own key in parentheses.
//   - Droid's AskUser and spec pickers (digits), its permission (a box of
//     unnumbered rows, the cursor colour only, on the first row) and its
//     folder trust.
//
// matchHarness runs before the generic shapes.

var (
	keyParen     = regexp.MustCompile(`^(.*?)\s*\(([^()]*)\)\s*$`)
	cursorAskRow = regexp.MustCompile(`^(\s*)(›\s+)?\[([ xX])\]\s+(\S.*)$`)
	cursorQOf    = regexp.MustCompile(`^Question \d+ of \d+$`)
	cursorAskFt  = regexp.MustCompile(`Space select · Enter next/submit`)
	leadNumber   = regexp.MustCompile(`^\d+\.\s+`)
	clineButtons = regexp.MustCompile(`^\s*\[y\] Approve\s+\[n\] Deny\s*$`)
	droidPermFt  = regexp.MustCompile(`^\s*↑↓ navigate\s+Enter select\s+Esc cancel`)
	droidTrustFt = regexp.MustCompile(`^\s*Enter to confirm · Esc to exit\s*$`)
	copilotPlan  = "Plan Ready for Review"
	planRow      = regexp.MustCompile(`^\s*(❯\s*)?(\d)\.\s+(\S.*)$`)
	planParen    = regexp.MustCompile(`^(.*?)\s+\((.*)\)\s*$`)
	planFoot     = regexp.MustCompile(`↑/↓ to navigate · enter to select`)
)

// matchHarness reads the prompts above.
func matchHarness(screen string, raw, lines []string) (Prompt, bool) {
	for _, f := range []func(raw, lines []string) (Prompt, bool){
		clinePermission, cursorAsk, cursorKeyed, droidPermission, droidTrust, copilotField,
	} {
		if p, ok := f(raw, lines); ok {
			return p, true
		}
	}
	if c, ok := copilotPlanChoice(raw, lines); ok {
		p := digitPrompt(c, KindQuestion)
		for i := range p.Options {
			switch i {
			case 1:
				p.Options[i].Role = wire.RoleApprove
			case 2:
				p.Options[i].Role = wire.RoleDeny
			default:
				p.Options[i].Role = wire.RoleOther
			}
		}
		return p, true
	}
	c, ok := parseHarnessChoice(screen)
	if !ok || !c.Digits {
		// Copilot's and Cline's lists move a visible cursor: the generic
		// agent choice reads them.
		return Prompt{}, false
	}
	if c.Typing || c.Review {
		return Prompt{Kind: KindWaiting, Title: c.Prompt, Keys: map[int][]string{}, ReadOnly: true}, true
	}
	kind := KindQuestion
	if strings.HasPrefix(c.Prompt, "Permission for the ") {
		kind = KindPermission
	}
	p := digitPrompt(c, kind)
	switch {
	case kind == KindPermission:
		for i := range p.Options {
			if strings.HasPrefix(p.Options[i].Label, "Allow for remainder") {
				p.Options[i].Role = wire.RoleApproveAlways
			}
		}
		if d := lineAboveRows(lines, c); d != "" && d != c.Prompt {
			p.Subject = NewSubject(wire.SubjectCommand, d)
		}
	case strings.HasPrefix(c.Prompt, "Propose Specification"):
		for i := range p.Options {
			p.Options[i].Role = wire.RoleOther
			switch p.Options[i].Label {
			case "Proceed with the proposal":
				p.Options[i].Role = wire.RoleApprove
			case "No and explain why":
				p.Options[i].Role = wire.RoleDeny
			}
		}
	case c.Prompt == droidAutonomyPrompt:
		for i := range p.Options {
			p.Options[i].Role = wire.RoleOther
		}
		p.Options[0].Role = wire.RoleApprove
	}
	return p, true
}

// digitPrompt is a prompt whose options, the choice's rows but the text
// row, are each picked by its digit alone; a multi-select or a batch is
// ReadOnly (its question item answers it).
func digitPrompt(c Choice, kind string) Prompt {
	p := Prompt{Kind: kind, Title: c.Prompt, Keys: map[int][]string{}, ReadOnly: c.Multi || c.Tabs > 1}
	for i, o := range c.Options {
		if o.Text {
			continue
		}
		n := len(p.Options) + 1
		p.Options = append(p.Options, wire.Option{N: n, Label: o.Label, Role: roleOf(o.Label)})
		p.Keys[n] = []string{strconv.Itoa(i + 1)}
	}
	return p
}

// lineAboveRows is the content line right above the first of c's rows:
// the command Vibe's permission is about.
func lineAboveRows(lines []string, c Choice) string {
	if len(c.Options) == 0 {
		return ""
	}
	for i, l := range lines {
		m := numberedRow.FindStringSubmatch(l)
		if m == nil || m[3] != "1" || !strings.HasPrefix(strings.TrimSpace(m[5]), c.Options[0].Label) {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if t := strings.TrimSpace(lines[j]); t != "" && !isBorder(t) {
				return t
			}
		}
	}
	return ""
}

// copilotPlanChoice reads Copilot's plan review: digits pick an action;
// a label's parenthesis (wrapped onto the next lines) is its detail. The
// actions are read up from the footer, so a numbered list in the plan
// above them is not taken for one.
func copilotPlanChoice(raw, lines []string) (Choice, bool) {
	foot := footerAt(lines, planFoot, 3)
	if foot < 0 {
		return Choice{}, false
	}
	top := boxTopAbove(raw, foot)
	if top < 0 || top+1 >= len(lines) || strings.TrimSpace(lines[top+1]) != copilotPlan {
		return Choice{}, false
	}
	c := Choice{Prompt: copilotPlan, Cursor: -1, TextRow: -1, Digits: true}
	var opts []ChoiceOption
	var below []string
	cursor := -1
	want := 0
	for i := foot - 1; i > top+1; i-- {
		t := strings.TrimSpace(lines[i])
		m := planRow.FindStringSubmatch(lines[i])
		if m == nil {
			if t != "" && !isBorder(t) {
				below = append([]string{t}, below...)
			} else if len(opts) == 0 {
				below = nil
			}
			continue
		}
		n, _ := strconv.Atoi(m[2])
		if want != 0 && n != want {
			break
		}
		l := recommended.ReplaceAllString(strings.Join(append([]string{strings.TrimSpace(m[3])}, below...), " "), "")
		o := ChoiceOption{Label: l}
		if pm := planParen.FindStringSubmatch(l); pm != nil {
			o.Label, o.Detail = pm[1], pm[2]
		}
		opts = append([]ChoiceOption{o}, opts...)
		if m[1] != "" {
			cursor = n - 1
		}
		below, want = nil, n-1
		if n == 1 {
			break
		}
	}
	if want != 0 || len(opts) < 2 {
		return Choice{}, false
	}
	c.Options, c.Cursor = opts, cursor
	return c, true
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// clinePermission reads Cline's tool approval; y approves, n denies.
func clinePermission(raw, lines []string) (Prompt, bool) {
	n := len(lines)
	btn := -1
	for i := n - 1; i >= 0 && i >= n-6; i-- {
		if clineButtons.MatchString(lines[i]) {
			btn = i
			break
		}
	}
	if btn < 0 {
		return Prompt{}, false
	}
	head := -1
	for i := btn - 1; i >= 0 && i >= btn-20; i-- {
		if strings.TrimSpace(lines[i]) == "Cline needs permission" {
			head = i
			break
		}
	}
	if head < 0 || head+1 >= btn {
		return Prompt{}, false
	}
	p := Prompt{Kind: KindPermission, Title: strings.TrimSpace(lines[head+1]),
		Options: []wire.Option{{N: 1, Label: "Approve", Role: wire.RoleApprove}, {N: 2, Label: "Deny", Role: wire.RoleDeny}},
		Keys:    map[int][]string{1: {"y"}, 2: {"n"}}}
	tool := ""
	if head+2 < btn {
		tool = strings.TrimSpace(lines[head+2])
	}
	for i := head + 3; i < btn; i++ {
		if t := strings.TrimSpace(lines[i]); strings.HasPrefix(t, "$ ") {
			p.Subject = NewSubject(wire.SubjectCommand, strings.TrimPrefix(t, "$ "))
			break
		} else if t != "" && p.Subject == nil {
			p.Subject = NewSubject(SubjectKindForTool(tool), t)
		}
	}
	return p, true
}

// cursorAsk reads Cursor's AskQuestion box: a row is checked with Space
// and Enter moves on (submitting on the last question); "Other" takes
// typing and is left to the terminal.
func cursorAsk(raw, lines []string) (Prompt, bool) {
	foot := footerAt(lines, cursorAskFt, 3)
	if foot < 0 {
		return Prompt{}, false
	}
	q := -1
	for i := foot - 1; i >= 0 && i >= foot-30; i-- {
		if cursorQOf.MatchString(strings.TrimSpace(lines[i])) {
			q = i
			break
		}
	}
	if q < 0 {
		return Prompt{}, false
	}
	title, multi, cursor := "", false, -1
	var rows []string
	for i := q + 1; i < foot; i++ {
		t := strings.TrimSpace(lines[i])
		if m := cursorAskRow.FindStringSubmatch(lines[i]); m != nil {
			if m[2] != "" {
				cursor = len(rows)
			}
			rows = append(rows, strings.TrimSpace(m[4]))
			continue
		}
		switch {
		case t == "":
		case t == "(multi-select)":
			multi = true
		case title == "" && len(rows) == 0:
			title = leadNumber.ReplaceAllString(t, "")
		}
	}
	if title == "" || len(rows) < 2 || cursor < 0 {
		return Prompt{}, false
	}
	p := Prompt{Kind: KindQuestion, Title: title, Keys: map[int][]string{}, ReadOnly: multi}
	for i, l := range rows {
		if strings.HasPrefix(strings.ToLower(l), "other:") {
			continue
		}
		n := len(p.Options) + 1
		p.Options = append(p.Options, wire.Option{N: n, Label: l, Role: wire.RoleOther})
		keys := moveKeys(i - cursor)
		p.Keys[n] = append(keys[:len(keys)-1:len(keys)-1], "Space", "Enter")
	}
	return p, len(p.Options) >= 2
}

// cursorKeyed reads Cursor's "Ready to build?" and "Run this command?":
// every row ends with the key that picks it, "(y)", "(tab)", "(b)", "(esc
// or n)".
func cursorKeyed(raw, lines []string) (Prompt, bool) {
	title := -1
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-12; i-- {
		if t := strings.TrimSpace(lines[i]); t == "Ready to build?" || t == "Run this command?" {
			title = i
			break
		}
	}
	if title < 0 {
		return Prompt{}, false
	}
	t := strings.TrimSpace(lines[title])
	p := Prompt{Kind: KindPermission, Title: t, Keys: map[int][]string{}}
	if t == "Ready to build?" {
		p.Kind = KindQuestion
	}
	for i := title + 1; i < len(lines); i++ {
		l := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "→"))
		l = leadNumber.ReplaceAllString(strings.TrimSpace(l), "")
		m := keyParen.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		key, ok := cursorKey(m[2])
		if !ok {
			continue
		}
		if strings.TrimSpace(m[1]) == "" && i > title+1 {
			// The row wrapped: its words are on the line above its key.
			m[1] = leadNumber.ReplaceAllString(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i-1]), "→")), "")
			if n := len(p.Options); n > 0 && p.Options[n-1].Label == strings.TrimSpace(m[1]) {
				continue
			}
		}
		n := len(p.Options) + 1
		role := wire.RoleOther
		switch {
		case key == "y" || key == "b":
			role = wire.RoleApprove
		case key == "Tab" || key == "BTab":
			role = wire.RoleApproveAlways
		case key == "n" || key == "p":
			role = wire.RoleDeny
		}
		p.Options = append(p.Options, wire.Option{N: n, Label: strings.TrimSpace(m[1]), Role: role})
		p.Keys[n] = []string{key}
	}
	if len(p.Options) < 2 {
		return Prompt{}, false
	}
	if p.Kind == KindPermission {
		for i := title - 1; i >= 0 && i >= title-3; i-- {
			if l := strings.TrimSpace(lines[i]); strings.HasPrefix(l, "$") {
				p.Subject = NewSubject(wire.SubjectCommand, strings.TrimSpace(strings.TrimPrefix(l, "$")))
				break
			}
		}
	}
	return p, true
}

// cursorKey turns a row's key hint into a tmux key.
func cursorKey(hint string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(hint)) {
	case "y":
		return "y", true
	case "tab":
		return "Tab", true
	case "shift+tab":
		return "BTab", true
	case "esc or n":
		return "n", true
	case "b", "c":
		return strings.ToLower(strings.TrimSpace(hint)), true
	case "p or esc":
		return "p", true
	}
	return "", false
}

// droidPermission reads Droid's tool approval: a box of rows that start
// with Yes or No (a long one wraps), the first one focused.
func droidPermission(raw, lines []string) (Prompt, bool) {
	foot := footerAt(lines, droidPermFt, 2)
	if foot < 0 {
		return Prompt{}, false
	}
	end := foot - 1
	for end >= 0 && isBorder(lines[end]) {
		end--
	}
	top := boxTopAbove(raw, end)
	if top < 0 {
		return Prompt{}, false
	}
	var labels []string
	for i := top + 1; i <= end; i++ {
		t := strings.TrimSpace(lines[i])
		switch {
		case t == "":
		case strings.HasPrefix(t, "Yes") || strings.HasPrefix(t, "No"):
			labels = append(labels, t)
		case len(labels) > 0:
			labels[len(labels)-1] += " " + t
		}
	}
	if len(labels) < 2 {
		return Prompt{}, false
	}
	// The title: the header above the preview box, or the line above.
	title := ""
	for i := top - 1; i >= 0 && i >= top-12; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" || isBorder(t) || unbox(raw[i]) != raw[i] {
			continue
		}
		title = t
		break
	}
	if title == "" {
		title = "Droid asks to continue"
	}
	p := Prompt{Kind: KindPermission, Title: title, Keys: map[int][]string{}}
	// A command: "Command to approve · low risk" over "↳ touch x.txt".
	if cmd, ok := strings.CutPrefix(title, "↳ "); ok {
		p.Subject = NewSubject(wire.SubjectCommand, cmd)
		for i := top - 1; i >= 1 && i >= top-12; i-- {
			if strings.TrimSpace(lines[i]) == title {
				if h := strings.TrimSpace(lines[i-1]); h != "" {
					p.Title = h
				}
				break
			}
		}
	}
	for i, l := range labels {
		n := i + 1
		role := wire.RoleApprove
		switch {
		case strings.HasPrefix(l, "No"):
			role = wire.RoleDeny
		case strings.HasPrefix(l, "Yes, and always") || strings.HasPrefix(l, "Yes, allow all"):
			role = wire.RoleApproveAlways
		}
		p.Options = append(p.Options, wire.Option{N: n, Label: l, Role: role})
		p.Keys[n] = moveKeys(i)
		if role == wire.RoleDeny {
			p.Keys[n] = []string{"Escape"}
		}
	}
	return p, true
}

// droidTrust reads Droid's "Trust this folder?": the cursor row is
// confirmed with Enter, Esc exits without trusting.
func droidTrust(raw, lines []string) (Prompt, bool) {
	foot := footerAt(lines, droidTrustFt, 2)
	if foot < 0 {
		return Prompt{}, false
	}
	rows, cursor, top := numberedRows(lines, foot-1)
	if len(rows) != 2 || cursor < 0 {
		return Prompt{}, false
	}
	title := ""
	for i := top - 1; i >= 0 && i >= top-20; i-- {
		if t := strings.TrimSpace(lines[i]); t == "Trust this folder?" {
			title = t
		}
	}
	if title == "" {
		return Prompt{}, false
	}
	p := Prompt{Kind: KindPermission, Title: title, Keys: map[int][]string{}}
	for i, o := range rows {
		n := i + 1
		role := wire.RoleApprove
		keys := moveKeys(i - cursor)
		if i == 1 {
			role, keys = wire.RoleDeny, []string{"Escape"}
		}
		p.Options = append(p.Options, wire.Option{N: n, Label: o.Label, Role: role})
		p.Keys[n] = keys
	}
	return p, true
}

// copilotField reads a Copilot form field that takes typing only (a
// string or number): its input sits between two rules over the footer.
// The words cannot be sent from here, so the phone is told it waits.
func copilotField(raw, lines []string) (Prompt, bool) {
	foot := -1
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-4; i-- {
		if copilotFoot.MatchString(lines[i]) && !strings.Contains(lines[i], "↑/↓") {
			foot = i
			break
		}
	}
	if foot < 3 || !isBorder(lines[foot-1]) || !strings.HasPrefix(strings.TrimSpace(lines[foot-2]), "❯") || !isBorder(lines[foot-3]) {
		return Prompt{}, false
	}
	for i := foot - 4; i >= 0 && i >= foot-12; i-- {
		if strings.TrimSpace(lines[i]) == copilotHead {
			title := strings.TrimSpace(lines[foot-4])
			return Prompt{Kind: KindWaiting, Title: title, Keys: map[int][]string{}, ReadOnly: true}, true
		}
	}
	return Prompt{}, false
}
