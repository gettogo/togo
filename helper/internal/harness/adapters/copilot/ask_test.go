package copilot

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
)

// hookCall is one ask tool call from testdata/hooks-ask.jsonl (Copilot
// 1.0.108, captured live), normalized the way `togo hook copilot` does.
type hookCall struct {
	tool            string
	input, response json.RawMessage
}

func askCalls(t *testing.T) map[string]hookCall {
	t.Helper()
	f, err := os.Open("testdata/hooks-ask.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, _ := hookstyle.For("copilot")
	calls := map[string]hookCall{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var line struct {
			Event   string          `json:"event"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		canon, norm, err := hookstyle.Normalize(d, line.Event, line.Payload)
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Tool     string          `json:"tool_name"`
			ID       string          `json:"tool_use_id"`
			Input    json.RawMessage `json:"tool_input"`
			Response json.RawMessage `json:"tool_response"`
		}
		_ = json.Unmarshal(norm, &p)
		if p.Tool != toolAskUser && p.Tool != toolExitPlan {
			continue
		}
		c := calls[p.ID]
		c.tool, c.input = p.Tool, p.Input
		if canon == hookstyle.PostToolUse {
			c.response = p.Response
		}
		calls[p.ID] = c
	}
	return calls
}

func TestAskSingleChoice(t *testing.T) {
	c := askCalls(t)["toolu_013rgKASZj8Na7YNFsZELRnr"]
	q, ok := askQuestion(c.tool, c.input)
	if !ok || len(q.Questions) != 1 {
		t.Fatalf("question %+v %v", q, ok)
	}
	got := q.Questions[0]
	if got.ID != "color" || got.Header != "README title color" || got.Prompt != "Which color should I use for the README title?" ||
		got.Multi || !got.AllowText || len(got.Options) != 3 || got.Options[1].ID != "Green" {
		t.Fatalf("question %+v", got)
	}
	if a := askAnswered(c.tool, c.input, c.response); !reflect.DeepEqual(a, map[string][]string{"color": {"Green"}}) {
		t.Fatalf("answered %v", a)
	}
}

// Three fields in one form: a multi-select array, a boolean drawn Yes / No,
// and a typed string, answered "platforms=iOS, Web, caching=false,
// name=demo-app".
func TestAskBatch(t *testing.T) {
	c := askCalls(t)["toolu_012KgCZbyeYHRFRxuryPZ2db"]
	q, ok := askQuestion(c.tool, c.input)
	if !ok || len(q.Questions) != 3 {
		t.Fatalf("question %+v %v", q, ok)
	}
	p, b, n := q.Questions[0], q.Questions[1], q.Questions[2]
	if p.ID != "platforms" || !p.Multi || len(p.Options) != 3 || p.Header != "Platforms" {
		t.Errorf("platforms %+v", p)
	}
	if b.ID != "caching" || b.Multi || len(b.Options) != 2 || b.Options[0].ID != "Yes" || b.Options[1].ID != "No" {
		t.Errorf("caching %+v", b)
	}
	if n.ID != "name" || len(n.Options) != 0 || !n.AllowText || n.Header != "Project name" {
		t.Errorf("name %+v", n)
	}
	want := map[string][]string{"platforms": {"iOS", "Web"}, "caching": {"No"}, "name": {"demo-app"}}
	if a := askAnswered(c.tool, c.input, c.response); !reflect.DeepEqual(a, want) {
		t.Fatalf("answered %v, want %v", a, want)
	}
}

// oneOf options show their titles; the result names the const.
func TestAskOneOf(t *testing.T) {
	calls := askCalls(t)
	c := calls["toolu_01FwnVFiPutT3pT2RE7Ny4sn"]
	q, ok := askQuestion(c.tool, c.input)
	if !ok || len(q.Questions[0].Options) != 2 || q.Questions[0].Options[0].ID != "Debug" {
		t.Fatalf("question %+v %v", q, ok)
	}
	if a := askAnswered(c.tool, c.input, c.response); !reflect.DeepEqual(a, map[string][]string{"level": {"Debug"}}) {
		t.Fatalf("answered %v", a)
	}
	// Other: the typed text comes back as it was typed.
	o := calls["toolu_017iXjCVUXUV9apq9vaQMbbw"]
	if a := askAnswered(o.tool, o.input, o.response); !reflect.DeepEqual(a, map[string][]string{"level": {"trace please"}}) {
		t.Fatalf("other %v", a)
	}
}

// Decline (ctrl+d) and cancel (esc) close the ask with no answer.
func TestAskDeclineCancel(t *testing.T) {
	calls := askCalls(t)
	for _, id := range []string{"toolu_0165x4gAnBSJv3HyLCySvdq6", "toolu_014sXz7aWNo6qCGNxqPUX3HD"} {
		c := calls[id]
		if a := askAnswered(c.tool, c.input, c.response); !reflect.DeepEqual(a, map[string][]string{"level": {}}) {
			t.Errorf("%s: answered %v", id, a)
		}
	}
}

func TestPlanReview(t *testing.T) {
	c := askCalls(t)["toolu_01UuASthgKbirazNcomAXBuF"]
	q, ok := askQuestion(c.tool, c.input)
	if !ok || len(q.Questions) != 1 || len(q.Questions[0].Options) != 4 {
		t.Fatalf("question %+v %v", q, ok)
	}
	if q.Approve != planActions[1].ID || q.Deny != planActions[2].ID {
		t.Errorf("approve %q deny %q", q.Approve, q.Deny)
	}
	// The owner picked "Exit to Interactive execution mode without running the plan".
	if a := askAnswered(c.tool, c.input, c.response); !reflect.DeepEqual(a, map[string][]string{"0": {planActions[2].ID}}) {
		t.Fatalf("answered %v", a)
	}
}

func TestAskOtherTools(t *testing.T) {
	if _, ok := askQuestion("bash", json.RawMessage(`{"command":"ls"}`)); ok {
		t.Fatal("bash read as a question")
	}
	if _, ok := askQuestion(toolAskUser, json.RawMessage(`{"message":"x"}`)); ok {
		t.Fatal("an ask with no fields read as a question")
	}
	if a := askAnswered(toolAskUser, json.RawMessage(`{}`), nil); a != nil {
		t.Fatalf("no response: %v", a)
	}
}

// A multi-select value holding ", " still maps to its option.
func TestAskMultiValueWithComma(t *testing.T) {
	in := json.RawMessage(`{"message":"m","requestedSchema":{"properties":{"a":{"type":"array","items":{"enum":["x, y","z"]}},"b":{"type":"string"}}}}`)
	resp := json.RawMessage(`{"resultType":"success","textResultForLlm":"User responded: a=x, y, z, b=hi, there"}`)
	want := map[string][]string{"a": {"x, y", "z"}, "b": {"hi, there"}}
	if a := askAnswered(toolAskUser, in, resp); !reflect.DeepEqual(a, want) {
		t.Fatalf("answered %v, want %v", a, want)
	}
}
