package tape

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// exports caches the tape of each pair for the tests of this file: one
// run of a scenario is the slow part, and two runs of the same pair must
// not overlap (the session id is fixed, so the tape is the same bytes).
var (
	exportMu sync.Mutex
	exports  = map[string]*exported{}
)

type exported struct {
	once sync.Once
	tape *Tape
	err  error
}

func export(t *testing.T, sc Scenario, agent string) (*Tape, error) {
	t.Helper()
	key := sc.ID + "/" + agent
	exportMu.Lock()
	e, ok := exports[key]
	if !ok {
		e = &exported{}
		exports[key] = e
	}
	exportMu.Unlock()
	e.once.Do(func() {
		// A failed export ends its goroutine: later callers must see that.
		e.err = fmt.Errorf("the export of %s did not finish", key)
		e.tape, e.err = Export(t, sc, agent)
	})
	return e.tape, e.err
}

// representative is one agent of each way the engine reads a session: a
// file-fed one (omp), a hook-fed one (cursor), a database-fed one
// (crush), both generations of the API-fed one (opencode, opencode2) and
// Codex. Every scenario runs for each, restarts included. The long run
// (TOGO_E2E_LONG=1) takes every registered profile.
var representative = []string{"omp", "cursor", "crush", "opencode", "opencode2", "codex"}

func testAgents() []string {
	if support.LongMode() {
		return sessiongen.Names()
	}
	return representative
}

// schema is the parts of protocol/v1/schema.json the tape check reads.
type schema struct {
	Defs map[string]json.RawMessage `json:"$defs"`
}

type object struct {
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
	Extra      *bool                      `json:"additionalProperties"`
	Enum       []string                   `json:"enum"`
	Pattern    string                     `json:"pattern"`
	Ref        string                     `json:"$ref"`
}

func loadSchema(t *testing.T) schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(internalDir(), "..", "..", "protocol", "v1", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// check validates one value against a definition's shape: the keys it
// requires and allows, enums and patterns of strings, and references.
func (s schema) check(t *testing.T, where string, def json.RawMessage, v any) {
	t.Helper()
	var o object
	if err := json.Unmarshal(def, &o); err != nil {
		t.Fatalf("%s: schema: %v", where, err)
	}
	if o.Ref != "" {
		name := o.Ref[len("#/$defs/"):]
		s.check(t, where, s.Defs[name], v)
		return
	}
	switch x := v.(type) {
	case string:
		if len(o.Enum) > 0 && !slices.Contains(o.Enum, x) {
			t.Errorf("%s: %q is not one of %v", where, x, o.Enum)
		}
		if o.Pattern != "" && !regexp.MustCompile(o.Pattern).MatchString(x) {
			t.Errorf("%s: %q does not match %s", where, x, o.Pattern)
		}
	case map[string]any:
		for _, k := range o.Required {
			if _, ok := x[k]; !ok {
				t.Errorf("%s: missing %q", where, k)
			}
		}
		for k, e := range x {
			p, ok := o.Properties[k]
			if !ok {
				if o.Extra != nil && !*o.Extra {
					t.Errorf("%s: %q is not in the schema", where, k)
				}
				continue
			}
			// Items and the mission delta have their own tests; the
			// notification's own shape is what a tape must keep.
			if k == "items" || k == "mission" {
				continue
			}
			s.check(t, where+"."+k, p, e)
		}
	}
}

// checkTape is what every tape holds.
func checkTape(t *testing.T, s schema, tp *Tape) {
	t.Helper()
	m := tp.Manifest
	for k, v := range map[string]string{"scenarioId": m.ScenarioID, "agent": m.Agent, "adapter": m.Adapter, "sessionId": m.SessionID} {
		if v == "" {
			t.Errorf("manifest has no %s", k)
		}
	}
	type frame struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      *int            `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	requests := map[int]string{}
	answered := map[int]bool{}
	var methods []string // requests and eofs in order
	var opens []int64    // after of each open, in order
	eofs := 0
	for i, l := range tp.Lines {
		var f frame
		if err := json.Unmarshal(l, &f); err != nil {
			t.Fatalf("line %d is not JSON: %v", i+1, err)
		}
		where := fmt.Sprintf("line %d", i+1)
		if f.JSONRPC != "2.0" {
			t.Errorf("%s: jsonrpc %q", where, f.JSONRPC)
		}
		switch {
		case f.ID != nil && f.Method != "":
			if _, dup := requests[*f.ID]; dup {
				t.Errorf("%s: request id %d is used twice", where, *f.ID)
			}
			requests[*f.ID] = f.Method
			methods = append(methods, f.Method)
			if f.Method == harness.MethodOpen {
				var p struct {
					After *int64 `json:"after"`
					Limit *int   `json:"limit"`
				}
				if err := json.Unmarshal(f.Params, &p); err != nil || p.After == nil || p.Limit != nil {
					t.Errorf("%s: open params %s: want id and after, no limit", where, f.Params)
				} else {
					opens = append(opens, *p.After)
				}
			}
		case f.ID != nil:
			if _, ok := requests[*f.ID]; !ok {
				t.Errorf("%s: response %d answers no request", where, *f.ID)
			}
			if answered[*f.ID] {
				t.Errorf("%s: request %d has two responses", where, *f.ID)
			}
			answered[*f.ID] = true
			if (f.Result == nil) == (f.Error == nil) {
				t.Errorf("%s: a response holds one of result and error", where)
			}
		case f.Method == harness.NotificationMethod:
			var p map[string]any
			if err := json.Unmarshal(f.Params, &p); err != nil {
				t.Fatalf("%s: %v", where, err)
			}
			s.check(t, where, s.Defs["HarnessNotification"], p)
			if p["sessionId"] != m.SessionID {
				t.Errorf("%s: notification for session %v", where, p["sessionId"])
			}
		case f.Method == "replay.eof":
			eofs++
			methods = append(methods, f.Method)
			var p struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(f.Params, &p) != nil || p.ID != m.SessionID {
				t.Errorf("%s: replay.eof params %s", where, f.Params)
			}
		default:
			t.Errorf("%s: unexpected frame %s", where, l)
		}
	}
	for id, m := range requests {
		if !answered[id] {
			t.Errorf("request %d (%s) has no response", id, m)
		}
	}
	if eofs > 0 {
		// One eof, then the two reopen exchanges: the previous cursor,
		// then from the start.
		at := slices.Index(methods, "replay.eof")
		if eofs != 1 || at < 0 || !slices.Equal(methods[at+1:], []string{harness.MethodOpen, harness.MethodOpen}) {
			t.Errorf("restart tape: %d eofs, then %v", eofs, methods[at+1:])
		}
		if n := len(opens); n < 3 || opens[n-1] != 0 || opens[n-2] == 0 {
			t.Errorf("restart tape: opens after %v, want the cursor and then 0", opens)
		}
	}
	if !bytes.HasSuffix(tp.Bytes(), []byte("\n")) {
		t.Error("the tape does not end with a newline")
	}
}

// Every tape the catalogue can make, for every registered profile, is
// valid; a pair that does not apply is skipped with its reason.
func TestEveryTapeIsValid(t *testing.T) {
	s := loadSchema(t)
	for _, sc := range Catalogue() {
		for _, agent := range testAgents() {
			t.Run(sc.ID+"/"+agent, func(t *testing.T) {
				t.Parallel()
				if sc.Applies(agent) == "" {
					support.Heavy(t)
				}
				tp, err := export(t, sc, agent)
				if sk, ok := err.(Skip); ok {
					t.Skip(string(sk))
				}
				if err != nil {
					t.Fatal(err)
				}
				checkTape(t, s, tp)
				dir := t.TempDir()
				if err := tp.Write(dir, sc.ID, Source{Revision: "0123456789abcdef0123456789abcdef01234567"}); err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(filepath.Join(dir, sc.ID+".json"))
				if err != nil {
					t.Fatal(err)
				}
				var m map[string]any
				if err := json.Unmarshal(raw, &m); err != nil {
					t.Fatal(err)
				}
				for _, k := range []string{"scenarioId", "agent", "adapter", "sessionId", "helperRevision"} {
					if v, ok := m[k].(string); !ok || v == "" {
						t.Errorf("manifest key %s = %v", k, m[k])
					}
				}
			})
		}
	}
}
