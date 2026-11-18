package redact

import (
	"regexp"
	"strings"
)

// A harness form (OpenCode 2.x: the question tool, an MCP server's input
// request, a form made through the form API) names each field with "key",
// and a field's condition names the field it depends on the same way. The
// reply must repeat those names, so they are not the provider keys the bare
// "key" rule removes. They are kept only where the shape is a form's: an
// object whose id starts with "frm_" and whose "fields" is a list, a field
// that says its type, a condition that says its comparison. The name must
// be a plain one and still passes String, so a key-shaped value is hidden
// as anywhere else.

// formFieldTypes are the field types of a form (OpenCode's form schema).
var formFieldTypes = map[string]bool{"string": true, "number": true, "integer": true, "boolean": true, "multiselect": true, "external": true}

// fieldName is what a form field may be called: what a reply can name.
var fieldName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// FieldName is a form field's name and the object that carries it.
type FieldName struct {
	In   map[string]any
	Name string
}

// Restore puts the name back into its object after redaction.
func (f FieldName) Restore() { f.In["key"] = String(f.Name) }

// FormFieldNames lists the field names of x when x is a form, read before
// x is redacted so they can be restored afterwards. Anything that is not a
// form has none.
func FormFieldNames(x map[string]any) []FieldName {
	id, _ := x["id"].(string)
	fields, ok := x["fields"].([]any)
	if !ok || !strings.HasPrefix(id, "frm_") {
		return nil
	}
	var keep []FieldName
	note := func(m map[string]any) {
		if name, ok := m["key"].(string); ok && fieldName.MatchString(name) {
			keep = append(keep, FieldName{m, name})
		}
	}
	for _, el := range fields {
		f, ok := el.(map[string]any)
		if kind, _ := f["type"].(string); !ok || !formFieldTypes[kind] {
			continue
		}
		note(f)
		conds, _ := f["when"].([]any)
		for _, c := range conds {
			if cond, ok := c.(map[string]any); ok && (cond["op"] == "eq" || cond["op"] == "neq") {
				note(cond)
			}
		}
	}
	return keep
}
