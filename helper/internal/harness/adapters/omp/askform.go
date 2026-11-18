package omp

import (
	"encoding/xml"
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

// formQuestions reads the form's named controls as questions: a group of
// radio buttons or checkboxes that share a name (prompted by its fieldset's
// legend), a select, and a text field or textarea (prompted by its label).
// The question id is the control's name, as the result names it. A form
// that cannot be read gives no questions; the tool row still shows.
func formQuestions(src string) []harness.Question {
	d := xml.NewDecoder(strings.NewReader(src))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	var (
		order  []string
		byName = map[string]*harness.Question{}
		legend string // the open fieldset's legend
		label  strings.Builder
		inLbl  int
		inLeg  bool
		// pending is the control inside the open label, named when the
		// label closes and its text is known.
		pending struct{ name, kind string }
		sel     *harness.Question // the open select
		opt     strings.Builder
		inOpt   bool
	)
	question := func(name string) *harness.Question {
		if q := byName[name]; q != nil {
			return q
		}
		q := &harness.Question{ID: name, Options: []harness.Choice{}}
		byName[name] = q
		order = append(order, name)
		return q
	}
	attr := func(t xml.StartElement, key string) string {
		for _, a := range t.Attr {
			if strings.EqualFold(a.Name.Local, key) {
				return a.Value
			}
		}
		return ""
	}
	for len(order) < maxFormFields {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch strings.ToLower(t.Name.Local) {
			case "fieldset":
				legend = ""
			case "legend":
				inLeg, legend = true, ""
			case "label":
				inLbl++
				label.Reset()
				pending.name, pending.kind = "", ""
			case "input":
				name, kind := attr(t, "name"), strings.ToLower(attr(t, "type"))
				if name == "" || kind == "hidden" || kind == "submit" || kind == "button" {
					continue
				}
				if kind == "" {
					kind = "text"
				}
				if inLbl > 0 {
					pending.name, pending.kind = name, kind
					continue
				}
				q := question(name)
				q.AllowText = kind != "radio" && kind != "checkbox"
				q.Multi = kind == "checkbox"
				if q.Prompt == "" {
					q.Prompt = firstNonEmpty(legend, attr(t, "placeholder"), name)
				}
			case "textarea":
				if name := attr(t, "name"); name != "" {
					if inLbl > 0 {
						pending.name, pending.kind = name, "text"
						continue
					}
					q := question(name)
					q.AllowText = true
					if q.Prompt == "" {
						q.Prompt = firstNonEmpty(legend, attr(t, "placeholder"), name)
					}
				}
			case "select":
				if name := attr(t, "name"); name != "" {
					sel = question(name)
					_, sel.Multi = findAttr(t, "multiple")
					if sel.Prompt == "" {
						sel.Prompt = firstNonEmpty(strings.TrimSpace(label.String()), legend, name)
					}
				}
			case "option":
				inOpt = true
				opt.Reset()
			}
		case xml.CharData:
			switch {
			case inOpt:
				opt.Write(t)
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
				legend = ""
			case "option":
				inOpt = false
				if text := strings.Join(strings.Fields(opt.String()), " "); sel != nil && text != "" {
					sel.Options = append(sel.Options, harness.Choice{ID: text, Label: truncate(text, harness.MaxTitle)})
				}
			case "select":
				sel = nil
			case "label":
				if inLbl > 0 {
					inLbl--
				}
				text := strings.Join(strings.Fields(label.String()), " ")
				if pending.name == "" {
					continue
				}
				q := question(pending.name)
				switch pending.kind {
				case "radio", "checkbox":
					// The label is the option; the fieldset's legend asks.
					q.Multi = pending.kind == "checkbox"
					if q.Prompt == "" {
						q.Prompt = firstNonEmpty(legend, pending.name)
					}
					if text != "" {
						q.Options = append(q.Options, harness.Choice{ID: text, Label: truncate(text, harness.MaxTitle)})
					}
				default:
					q.AllowText = true
					if q.Prompt == "" {
						q.Prompt = firstNonEmpty(text, legend, pending.name)
					}
				}
				pending.name = ""
			}
		}
	}
	out := make([]harness.Question, 0, len(order))
	for _, name := range order {
		q := byName[name]
		q.Prompt = truncate(q.Prompt, harness.MaxTitle)
		if len(q.Options) > harness.MaxChoices {
			q.Options = q.Options[:harness.MaxChoices]
		}
		out = append(out, *q)
	}
	return out
}

func findAttr(t xml.StartElement, key string) (string, bool) {
	for _, a := range t.Attr {
		if strings.EqualFold(a.Name.Local, key) {
			return a.Value, true
		}
	}
	return "", false
}
