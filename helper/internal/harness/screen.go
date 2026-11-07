package harness

import (
	"encoding/json"
	"time"
)

// The seven primitives every screen is drawn with.
const (
	PrimPicker    = "picker"
	PrimSettings  = "settings"
	PrimChecklist = "checklist"
	PrimLiveList  = "liveList"
	PrimDetail    = "detail"
	PrimConfirm   = "confirm"
	PrimStatus    = "status"
)

// Primitives lists every primitive.
var Primitives = []string{PrimPicker, PrimSettings, PrimChecklist, PrimLiveList, PrimDetail, PrimConfirm, PrimStatus}

// Screen is a harness.screen result: one primitive filled with what the
// adapter read. Exactly the field named by Primitive is set.
type Screen struct {
	ID        string    `json:"id"`
	Primitive string    `json:"primitive"`
	Title     string    `json:"title"`
	FetchedAt time.Time `json:"fetchedAt"`
	// Stale is true when the source failed and this is the last good copy.
	Stale bool `json:"stale"`

	Picker    *Picker      `json:"picker,omitempty"`
	Settings  *Settings    `json:"settings,omitempty"`
	Checklist *Checklist   `json:"checklist,omitempty"`
	LiveList  *LiveList    `json:"liveList,omitempty"`
	Detail    *Detail      `json:"detail,omitempty"`
	Confirm   *Confirm     `json:"confirm,omitempty"`
	Status    *StatusBlock `json:"status,omitempty"`

	// Actions are screen-level actions (toolbar or footer).
	Actions []ActionRef `json:"actions"`
}

// Picker is a searchable, grouped list with one selection.
type Picker struct {
	Items    []PickerItem `json:"items"`
	Selected string       `json:"selected,omitempty"`
	// Searchable lets the app filter Items by title, subtitle and group.
	Searchable bool     `json:"searchable"`
	Filters    []Filter `json:"filters,omitempty"`
	// OnSelect is the action run with args {"id": item id}.
	OnSelect string `json:"onSelect"`
	// ItemActions are offered on each item (swipe and context menu), run
	// with args {"id": item id}.
	ItemActions []ActionRef `json:"itemActions,omitempty"`
}

// PickerItem is one choice in a Picker.
type PickerItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Badge    string `json:"badge,omitempty"`
	Group    string `json:"group,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	// Facets are values the filters match on, by filter id.
	Facets map[string]string `json:"facets,omitempty"`
}

// Filter kinds.
const (
	FilterToggle = "toggle"
	FilterChoice = "choice"
)

// Filter narrows a Picker on the phone.
type Filter struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	Options []Choice `json:"options,omitempty"`
	Default string   `json:"default,omitempty"`
}

// Settings is a form of typed fields in sections.
type Settings struct {
	Sections []SettingsSection `json:"sections"`
}

// SettingsSection groups fields.
type SettingsSection struct {
	Title  string  `json:"title,omitempty"`
	Footer string  `json:"footer,omitempty"`
	Fields []Field `json:"fields"`
}

// Field kinds.
const (
	FieldToggle = "toggle"
	FieldChoice = "choice"
	FieldText   = "text"
	FieldNumber = "number"
	FieldSecret = "secret"
)

// Field is one setting. A secret field never carries its value: Value is
// null and IsSet says whether one exists.
type Field struct {
	ID          string          `json:"id"`
	Label       string          `json:"label"`
	Help        string          `json:"help,omitempty"`
	Kind        string          `json:"kind"`
	Value       json.RawMessage `json:"value"`
	IsSet       bool            `json:"isSet,omitempty"`
	Options     []Choice        `json:"options,omitempty"`
	Min         *float64        `json:"min,omitempty"`
	Max         *float64        `json:"max,omitempty"`
	Placeholder string          `json:"placeholder,omitempty"`
	ReadOnly    bool            `json:"readOnly,omitempty"`
	// Action is run with args {"value": new value} when the user commits.
	Action string `json:"action,omitempty"`
}

// Checklist is a plan or todo list.
type Checklist struct {
	Items []CheckItem `json:"items"`
	Done  int         `json:"done"`
	Total int         `json:"total"`
}

// CheckItem is one checklist entry. State is a Row state: pending, active,
// done or skipped.
type CheckItem struct {
	ID       string      `json:"id"`
	Text     string      `json:"text"`
	State    string      `json:"state"`
	Children []CheckItem `json:"children,omitempty"`
}

// LiveList is a list of running things that refreshes while shown.
type LiveList struct {
	Rows []Row `json:"rows"`
	// RefreshSeconds is how often the app asks again while the screen shows;
	// 0 means the rows only change through mission deltas.
	RefreshSeconds int `json:"refreshSeconds"`
}

// Body kinds for a Detail card.
const (
	BodyMarkdown = "markdown"
	BodyDiff     = "diff"
	BodyKV       = "kv"
	BodyText     = "text"
)

// Detail is a card with a body and actions.
type Detail struct {
	Subtitle string      `json:"subtitle,omitempty"`
	Body     Body        `json:"body"`
	Actions  []ActionRef `json:"actions"`
}

// Body is a Detail card's content. Diff is a unified diff, shown monospace.
type Body struct {
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
	KV   []Fact `json:"kv,omitempty"`
}

// Confirm asks one or more questions, or a plain yes or no when Questions
// is empty.
type Confirm struct {
	Message     string     `json:"message,omitempty"`
	Questions   []Question `json:"questions"`
	Destructive bool       `json:"destructive"`
	// Submit is the action run with args {"answers": {question id: [option
	// ids or text]}}, or {} for a plain confirmation.
	Submit string `json:"submit"`
}
