package opencode

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/redact"
)

// A form field becomes one question. What the field says beyond its type
// is kept where the question model can say it exactly: required, the
// default, and a condition on one earlier field. A condition it cannot
// say exactly is never approximated, because OpenCode refuses a reply that
// answers a field that is not active or leaves out one that is: such a
// form is shown and answered in the terminal.

// when2 is one condition of a field: all of a field's conditions must
// hold. Against a list, "eq" means the selection includes the value.
type when2 struct {
	Key   string          `json:"key"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

// maxDefault is the longest text the answer action takes for one answer; a
// longer default could not be sent back, and half of one is a wrong answer.
const maxDefault = 4 << 10

// shown2 is a field that became a question, and that question's id.
type shown2 struct {
	field field2
	id    string
}

// formQuestions maps a form's visible fields; answerable is false when a
// reply built from the questions could not be a complete, valid one.
func formQuestions(f *form2) (qs []harness.Question, answerable bool) {
	answerable = true
	question := f.Metadata["kind"] == "question"
	shown := map[string]shown2{}
	for i, fl := range f.Fields {
		if fl.Key == "" && question {
			// The question tool names its fields q0, q1... (seen on
			// 2.2.5); kept for a record whose names were stripped.
			fl.Key = "q" + strconv.Itoa(i)
		}
		if fl.Hidden {
			continue
		}
		if len(qs) == harness.MaxQuestions {
			// More than an app shows: the rest is not cut off silently.
			// The form is shown as far as it goes and answered in the
			// terminal, since a reply without these fields is refused.
			answerable = false
			break
		}
		if !replyKey.MatchString(fl.Key) {
			// A reply cannot name this field, so it cannot be complete.
			answerable = false
			fl.Key = "?" + strconv.Itoa(i)
		}
		required := fl.Required // OpenCode says for every field: sent as true or false, never left out
		q := harness.Question{ID: fl.Key, Header: fl.Title, Prompt: fl.Description, Required: &required}
		if q.Prompt == "" {
			q.Prompt, q.Header = fl.Title, ""
		}
		for _, o := range fl.Options {
			label := o.Label
			if label == "" {
				label = o.Value
			}
			q.Options = append(q.Options, harness.Choice{ID: o.Value, Label: label, Detail: o.Description})
		}
		switch fl.Type {
		case "multiselect":
			q.ID += "[]"
			q.Multi = true
			q.AllowText = fl.Custom != nil && *fl.Custom
		case "boolean":
			q.ID += "!"
			q.Options = []harness.Choice{{ID: "true", Label: "Yes"}, {ID: "false", Label: "No"}}
		case "number", "integer":
			q.ID += "#"
			q.AllowText = true
		case "external":
			q.Prompt = strings.TrimSpace(q.Prompt + " " + fl.URL)
		default:
			q.AllowText = len(q.Options) == 0 || fl.Custom == nil || *fl.Custom
		}
		q.Default = fieldDefault(fl)
		if len(fl.When) > 0 {
			if q.When = fieldWhen(fl.When, shown); q.When == nil {
				answerable = false
			}
		}
		shown[fl.Key] = shown2{fl, q.ID}
		qs = append(qs, q)
	}
	return qs, answerable
}

// fieldDefault is the answer a field starts with, as the answer action
// takes it: option values, "true" or "false", a number, or one text. A
// default of the wrong type is ignored, and a field named like a secret
// shows none.
func fieldDefault(fl field2) []string {
	if len(fl.Default) == 0 || redact.SecretKey(fl.Key) {
		return nil
	}
	switch fl.Type {
	case "multiselect":
		var list []string
		if json.Unmarshal(fl.Default, &list) != nil || len(list) == 0 || len(list) > harness.MaxChoices {
			return nil
		}
		return list
	case "boolean", "number", "integer":
		v, ok := scalarText(fl.Default)
		if !ok || fl.Default[0] == '"' || (fl.Type == "boolean") != (v == "true" || v == "false") {
			return nil
		}
		return []string{v}
	case "string":
		var s string
		if json.Unmarshal(fl.Default, &s) != nil || s == "" || len(s) > maxDefault {
			return nil
		}
		return []string{s}
	}
	return nil
}

// fieldWhen turns a field's conditions into the one a question can carry:
// a single condition on an earlier shown field. "eq" is kept as it is
// ("includes" on a list, which is what Applies checks). "neq" also needs
// the field answered, so it is kept only where the other answers can be
// listed: a yes/no, or a choice that takes no typed text. Nil means the
// conditions cannot be said exactly.
func fieldWhen(conds []when2, shown map[string]shown2) *harness.QuestionWhen {
	if len(conds) != 1 {
		return nil
	}
	c := conds[0]
	on, ok := shown[c.Key]
	val, isScalar := scalarText(c.Value)
	if !ok || !isScalar {
		return nil
	}
	switch c.Op {
	case "eq":
		return &harness.QuestionWhen{Question: on.id, Equals: []string{val}}
	case "neq":
		var others []string
		switch {
		case on.field.Type == "boolean" && val == "true":
			others = []string{"false"}
		case on.field.Type == "boolean" && val == "false":
			others = []string{"true"}
		case on.field.Type == "string" && on.field.Custom != nil && !*on.field.Custom:
			for _, o := range on.field.Options {
				if o.Value != val {
					others = append(others, o.Value)
				}
			}
		}
		if len(others) > 0 {
			return &harness.QuestionWhen{Question: on.id, Equals: others}
		}
	}
	return nil
}

// scalarText is a JSON string, boolean or number as an answer spells it:
// a number in its shortest form (2.50 is "2.5").
func scalarText(raw json.RawMessage) (string, bool) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	}
	return "", false
}
