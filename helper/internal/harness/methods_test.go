package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
)

const sid = "s_abcdefghij"

func call(t *testing.T, e Engine, method, params string) (any, error) {
	t.Helper()
	m, ok := Methods(e)[method]
	if !ok {
		t.Fatalf("no method %s", method)
	}
	return m(context.Background(), json.RawMessage(params))
}

func TestNopDescribeHasNoAdapter(t *testing.T) {
	got, err := call(t, Nop{}, MethodDescribe, `{"id":"`+sid+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	want := `{"adapter":null,"capabilities":[],"screens":[],"modes":[],"mode":"","degraded":[]}`
	if string(b) != want {
		t.Fatalf("got %s\nwant %s", b, want)
	}
}

func TestNopOpenIsRefused(t *testing.T) {
	_, err := call(t, Nop{}, MethodOpen, `{"id":"`+sid+`"}`)
	var ce *api.CodeError
	if !errors.As(err, &ce) || ce.Code != rpc.CodeRefused {
		t.Fatalf("err %v, want -32008", err)
	}
}

func TestBoundaryValidation(t *testing.T) {
	for name, c := range map[string][2]string{
		"bad session id":    {MethodDescribe, `{"id":"../x"}`},
		"screen name":       {MethodScreen, `{"id":"` + sid + `","screen":"a b"}`},
		"query key":         {MethodScreen, `{"id":"` + sid + `","screen":"model","query":{"a b":"x"}}`},
		"action name":       {MethodAction, `{"id":"` + sid + `","action":""}`},
		"empty text":        {MethodSend, `{"id":"` + sid + `","text":""}`},
		"escape in text":    {MethodSend, `{"id":"` + sid + `","text":"hi\u001b[201~"}`},
		"send mode":         {MethodSend, `{"id":"` + sid + `","text":"hi","mode":"later"}`},
		"complete kind":     {MethodComplete, `{"id":"` + sid + `","kind":"emoji","query":""}`},
		"history before":    {MethodHistory, `{"id":"` + sid + `","before":0}`},
		"open after":        {MethodOpen, `{"id":"` + sid + `","after":-1}`},
		"anchor needle":     {MethodLocate, `{"id":"` + sid + `","anchor":{"epoch":"e","bytes":0,"line":0,"needle":"` + strings.Repeat("x", MaxNeedle+1) + `"}}`},
		"params not object": {MethodDescribe, `[1]`},
		"target as flag":    {MethodAction, `{"id":"` + sid + `","action":"stop","target":"--force"}`},
		"target with space": {MethodAction, `{"id":"` + sid + `","action":"stop","target":"a b"}`},
		"target with break": {MethodAction, `{"id":"` + sid + `","action":"stop","target":"a\nb"}`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := call(t, Nop{}, c[0], c[1])
			var inv *session.InvalidParamsError
			if !errors.As(err, &inv) {
				t.Fatalf("err %v, want invalid params", err)
			}
		})
	}
}

// Picker items are ids the harness made: omp model selectors hold "/" and
// ":", OpenCode's are compact JSON. They must reach the action.
func TestModelIDsAreTargets(t *testing.T) {
	for _, target := range []string{"apple/on-device", "llama.cpp/qwen3.8-27b:low", `{"id":"gpt-5","providerID":"openai"}`} {
		raw, _ := json.Marshal(map[string]string{"id": sid, "action": "set_model", "target": target})
		var got ActionRequest
		if _, err := call(t, actSpy{fn: func(r ActionRequest) { got = r }}, MethodAction, string(raw)); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if got.Target != target {
			t.Fatalf("target %q, want %q", got.Target, target)
		}
	}
}

func TestSendDefaultsToQueue(t *testing.T) {
	var got SendRequest
	e := sendSpy{fn: func(r SendRequest) { got = r }}
	if _, err := call(t, e, MethodSend, `{"id":"`+sid+`","text":"run the tests"}`); err != nil {
		t.Fatal(err)
	}
	if got.Mode != SendQueue || got.Text != "run the tests" {
		t.Fatalf("got %+v", got)
	}
}

func TestErrorMapping(t *testing.T) {
	for name, c := range map[string]struct {
		err  error
		code int
	}{
		"busy":     {ErrBusy, rpc.CodeConflict},
		"fallback": {&FallbackError{Reason: "source"}, rpc.CodeRefused},
	} {
		t.Run(name, func(t *testing.T) {
			var ce *api.CodeError
			if err := mapErr(c.err); !errors.As(err, &ce) || ce.Code != c.code {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// Items carry their body under the key named by their kind.
func TestItemEncodesBodyUnderKind(t *testing.T) {
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	it := Item{ID: "t1", Seq: 3, TurnID: "u1", Kind: KindTool, At: at,
		Tool: &ToolBody{CallID: "c1", Name: "Bash", Title: "Run tests", Category: ToolShell, State: StateRunning, Started: at}}
	b, err := json.Marshal(it)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	if _, ok := m["tool"]; !ok || m["kind"] == nil || len(m) != 6 {
		t.Fatalf("encoded %s", b)
	}
}

type sendSpy struct {
	Nop
	fn func(SendRequest)
}

func (s sendSpy) Send(_ context.Context, r SendRequest) (SendResult, error) {
	s.fn(r)
	return SendResult{}, nil
}

type actSpy struct {
	Nop
	fn func(ActionRequest)
}

func (s actSpy) Act(_ context.Context, r ActionRequest) (ActionResult, error) {
	s.fn(r)
	return ActionResult{Result: ResultApplied}, nil
}

// Every method served here is declared in the protocol schema, as is the
// notification method.
func TestSchemaDeclaresMethods(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/v1/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	for m := range Methods(Nop{}) {
		if !bytes.Contains(schema.Defs["Request"], []byte(`{ "const": "`+m+`" }`)) {
			t.Errorf("schema Request lacks %s", m)
		}
	}
	if !bytes.Contains(schema.Defs["Notification"], []byte(`{ "const": "`+NotificationMethod+`" }`)) {
		t.Errorf("schema Notification lacks %s", NotificationMethod)
	}
	if !bytes.Contains(schema.Defs["HelloResult"], []byte(`\"`+Capability+`\"`)) {
		t.Errorf("schema HelloResult does not document %s", Capability)
	}
}

func TestRegistryRefusesDuplicates(t *testing.T) {
	RegisterMapper("test.dup", func(Env) Mapper { return nil })
	if _, ok := LookupMapper("test.dup"); !ok {
		t.Fatal("mapper not found")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("second registration did not panic")
		}
	}()
	RegisterMapper("test.dup", func(Env) Mapper { return nil })
}
