package omp

import (
	"encoding/xml"
	"regexp"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// From 18.20 omp's ask tool takes one HTML form instead of a questions
// list: arguments {html: "<form>…</form>"}. Its dialog shows one field per
// named control, and the result's details hold
// answers: [{name, question, answer}].
//
// maxFormFields bounds the questions one form becomes.
const maxFormFields = 32

// textTypes are the <input> types omp draws as a plain text field that
// keys can fill and the screen shows as typed.
var textTypes = map[string]bool{"text": true, "email": true, "url": true, "number": true, "tel": true, "search": true}

// otherOption is omp's rule for a group that brings its own Other row
// (html-form/parse.ts): then omp adds none.
var otherOption = regexp.MustCompile(`(?i)^\s*other\b`)

// formField is what formQuestions learns about one named control beyond
// the question: how the phone's answer can reach it.
type formField struct {
	kind     string // radio, checkbox, select, text
	fieldset int    // the fieldset it is in, 0 outside one
	labelled bool   // its prompt came from a legend or a label
	drivable bool   // false for a control keys cannot fill or the screen hides
}

// formQuestions reads the form's named controls as questions: a group of
// radio buttons or checkboxes that share a name (prompted by its fieldset's
// legend), a select, and a text field or textarea (prompted by its label).
// The question id is the control's name, as the result names it. A form
// that cannot be read gives no questions; the tool row still shows.
//
// drivable says the phone can answer it: the form driver (approval's
// formkeys.go) finds every field by its question on screen, so each needs
// a question of its own that the dialog shows as it is read here. Not
// drivable: a password or a date-like input (the screen does not show
// what was typed as typed), a disabled control or option, a fieldset of
// several fields (its fields share one heading), a lone check box, two
// fields with the same question, a field with no label, and a form cut
// at maxFormFields or harness.MaxChoices.
func formQuestions(src string) (qs []harness.Question, drivable bool) {
	d := xml.NewDecoder(strings.NewReader(src))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	var (
		order  []string
		byName = map[string]*harness.Question{}
		info   = map[string]*formField{}
		legend string // the open fieldset's legend
		sets   int    // fieldsets seen
		inSet  int    // the open fieldset, 0 outside one
		label  strings.Builder
		inLbl  int
		inLeg  bool
		// pending is the control inside the open label, named when the
		// label closes and its text is known.
		pending struct {
			name, kind, value string
			checked, disabled bool
		}
		sel      *harness.Question // the open select
		selName  string
		selected bool // the open select has a selected option
		opt      strings.Builder
		optSel   bool
		inOpt    bool
		inArea   string // the open textarea's name
		areaText strings.Builder
		cut      bool
	)
	question := func(name, kind string) *harness.Question {
		if q := byName[name]; q != nil {
			return q
		}
		if len(order) >= maxFormFields {
			cut = true
			return &harness.Question{}
		}
		q := &harness.Question{ID: name, Options: []harness.Choice{}}
		byName[name] = q
		info[name] = &formField{kind: kind, fieldset: inSet, drivable: true}
		order = append(order, name)
		return q
	}
	attr := func(t xml.StartElement, key string) string {
		v, _ := findAttr(t, key)
		return v
	}
	required := func(q *harness.Question, t xml.StartElement) {
		_, req := findAttr(t, "required")
		if q.Required == nil || req {
			q.Required = &req
		}
	}
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch strings.ToLower(t.Name.Local) {
			case "fieldset":
				legend, sets = "", sets+1
				inSet = sets
			case "legend":
				inLeg, legend = true, ""
			case "label":
				inLbl++
				label.Reset()
				pending.name, pending.kind = "", ""
			case "input":
				name, kind := attr(t, "name"), strings.ToLower(attr(t, "type"))
				if name == "" || kind == "hidden" || kind == "submit" || kind == "button" || kind == "reset" || kind == "image" {
					continue
				}
				if kind == "" {
					kind = "text"
				}
				_, checked := findAttr(t, "checked")
				_, disabled := findAttr(t, "disabled")
				if inLbl > 0 {
					pending.name, pending.kind, pending.value, pending.checked, pending.disabled = name, kind, attr(t, "value"), checked, disabled
					q := question(name, choiceKind(kind))
					required(q, t)
					continue
				}
				q := question(name, choiceKind(kind))
				required(q, t)
				f := info[name]
				q.AllowText = kind != "radio" && kind != "checkbox"
				q.Multi = kind == "checkbox"
				if f != nil {
					// An unlabelled radio or box draws no option text the
					// driver can find.
					f.drivable = f.drivable && !disabled && (q.AllowText && textTypes[kind] || false)
				}
				if q.Prompt == "" {
					q.Prompt = firstNonEmpty(legend, attr(t, "placeholder"), name)
					if f != nil {
						f.labelled = legend != ""
					}
				}
				if v := attr(t, "value"); q.AllowText && v != "" {
					q.Default = []string{v}
				}
			case "textarea":
				if name := attr(t, "name"); name != "" {
					q := question(name, "text")
					required(q, t)
					q.AllowText = true
					inArea = name
					areaText.Reset()
					if _, disabled := findAttr(t, "disabled"); disabled && info[name] != nil {
						info[name].drivable = false
					}
					if inLbl > 0 {
						pending.name, pending.kind = name, "textarea"
						continue
					}
					if q.Prompt == "" {
						q.Prompt = firstNonEmpty(legend, attr(t, "placeholder"), name)
						info[name].labelled = legend != ""
					}
				}
			case "select":
				if name := attr(t, "name"); name != "" {
					sel, selName, selected = question(name, "select"), name, false
					required(sel, t)
					_, sel.Multi = findAttr(t, "multiple")
					_, disabled := findAttr(t, "disabled")
					if f := info[name]; f != nil {
						f.drivable = f.drivable && !disabled && !sel.Multi
					}
					if sel.Prompt == "" {
						lbl := strings.TrimSpace(label.String())
						sel.Prompt = firstNonEmpty(lbl, legend, name)
						if f := info[name]; f != nil {
							f.labelled = lbl != "" || legend != ""
						}
					}
				}
			case "option":
				inOpt = true
				opt.Reset()
				_, optSel = findAttr(t, "selected")
				if _, disabled := findAttr(t, "disabled"); disabled && info[selName] != nil {
					info[selName].drivable = false
				}
			}
		case xml.CharData:
			switch {
			case inOpt:
				opt.Write(t)
			case inArea != "":
				areaText.Write(t)
			case inLeg:
				legend += string(t)
			case inLbl > 0:
				label.Write(t)
			}
		case xml.EndElement:
			switch strings.ToLower(t.Name.Local) {
			case "legend":
				inLeg, legend = false, strings.Join(strings.Fields(legend), " ")
			case "fieldset":
				legend, inSet = "", 0
			case "textarea":
				if q := byName[inArea]; q != nil {
					if v := strings.TrimPrefix(areaText.String(), "\n"); v != "" {
						q.Default = []string{v}
					}
				}
				inArea = ""
			case "option":
				inOpt = false
				if text := strings.Join(strings.Fields(opt.String()), " "); sel != nil && text != "" {
					sel.Options = append(sel.Options, harness.Choice{ID: text, Label: truncate(text, harness.MaxTitle)})
					if optSel && (sel.Multi || !selected) {
						sel.Default, selected = append(sel.Default, text), true
						if !sel.Multi {
							sel.Default = []string{text}
						}
					}
				}
			case "select":
				if sel != nil && !sel.Multi && !selected && len(sel.Options) > 0 {
					// omp picks the first option of a single select.
					sel.Default = []string{sel.Options[0].ID}
				}
				sel = nil
			case "label":
				if inLbl > 0 {
					inLbl--
				}
				text := strings.Join(strings.Fields(label.String()), " ")
				if pending.name == "" {
					continue
				}
				q := question(pending.name, choiceKind(pending.kind))
				f := info[pending.name]
				switch pending.kind {
				case "radio", "checkbox":
					// The label is the option; the fieldset's legend asks.
					q.Multi = pending.kind == "checkbox"
					if q.Prompt == "" {
						q.Prompt = firstNonEmpty(legend, pending.name)
						if f != nil {
							f.labelled = legend != ""
						}
					}
					if text != "" {
						q.Options = append(q.Options, harness.Choice{ID: text, Label: truncate(text, harness.MaxTitle)})
						if pending.checked {
							if !q.Multi {
								q.Default = nil
							}
							q.Default = append(q.Default, text)
						}
					} else if f != nil {
						f.drivable = false
					}
					if pending.disabled && f != nil {
						f.drivable = false
					}
				default:
					q.AllowText = true
					if q.Prompt == "" {
						q.Prompt = firstNonEmpty(text, legend, pending.name)
						if f != nil {
							f.labelled = text != "" || legend != ""
						}
					}
					if f != nil && pending.kind != "textarea" {
						f.drivable = f.drivable && textTypes[pending.kind] && !pending.disabled
					}
					if v := pending.value; v != "" && pending.kind != "textarea" {
						q.Default = []string{v}
					}
				}
				pending.name = ""
			}
		}
	}
	drivable = !cut && len(order) > 0
	prompts := map[string]bool{}
	perSet := map[int]int{}
	for _, name := range order {
		if f := info[name]; f.fieldset > 0 {
			perSet[f.fieldset]++
		}
	}
	out := make([]harness.Question, 0, len(order))
	for _, name := range order {
		q, f := byName[name], info[name]
		key := strings.ToLower(strings.Join(strings.Fields(q.Prompt), " "))
		group := f.kind == "radio" || f.kind == "checkbox"
		if group {
			// omp adds an Other row that takes typed words to a group of
			// two or more options that has none of its own.
			q.AllowText = len(q.Options) >= 2
			for _, o := range q.Options {
				if otherOption.MatchString(o.Label) {
					q.AllowText = false
				}
			}
		}
		if !f.drivable || !f.labelled || prompts[key] || f.fieldset > 0 && perSet[f.fieldset] > 1 ||
			group && len(q.Options) < 2 || len(q.Options) > harness.MaxChoices {
			drivable = false
		}
		prompts[key] = true
		q.Prompt = truncate(q.Prompt, harness.MaxTitle)
		if len(q.Options) > harness.MaxChoices {
			q.Options = q.Options[:harness.MaxChoices]
		}
		if q.Required == nil {
			no := false
			q.Required = &no
		}
		out = append(out, *q)
	}
	return out, drivable
}

// choiceKind names a control's field kind: its group's type, or text.
func choiceKind(kind string) string {
	switch kind {
	case "radio", "checkbox":
		return kind
	}
	return "text"
}

func findAttr(t xml.StartElement, key string) (string, bool) {
	for _, a := range t.Attr {
		if strings.EqualFold(a.Name.Local, key) {
			return a.Value, true
		}
	}
	return "", false
}
