package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// fakeTypedChild answers like omp --mode rpc: a ready frame, unrelated
// events, then one response per command. Run as the test binary itself
// when TOGO_FAKE_TYPED_CHILD is set.
func fakeTypedChild(in io.Reader, out io.Writer) {
	fmt.Fprintln(out, `{"type":"ready","protocolVersion":1}`)
	fmt.Fprintln(out, `{"type":"extension_ui_request","id":"x1","method":"setWidget"}`)
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		var cmd map[string]any
		if json.Unmarshal(sc.Bytes(), &cmd) != nil {
			continue
		}
		id, _ := cmd["id"].(string)
		switch cmd["type"] {
		case "get_available_commands":
			fmt.Fprintln(out, `{"type":"available_commands_update","commands":[]}`)
			fmt.Fprintf(out, `{"id":"other","type":"response","command":"x","success":true,"data":1}`+"\n")
			fmt.Fprintf(out, `{"id":%q,"type":"response","command":"get_available_commands","success":true,"data":{"commands":[{"name":"switch","description":"Switch model for this session only","input":{"hint":"[model]"}}]}}`+"\n", id)
		default:
			fmt.Fprintf(out, `{"id":%q,"type":"response","command":%q,"success":false,"error":"Unknown command"}`+"\n", id, cmd["type"])
		}
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("TOGO_FAKE_TYPED_CHILD") == "1" {
		fakeTypedChild(os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCallTypedSkipsEventsAndMatchesID(t *testing.T) {
	cr, cw := io.Pipe() // client writes, child reads
	sr, sw := io.Pipe() // child writes, client reads
	go fakeTypedChild(cr, sw)
	tr := Lines(sr, cw, func() error { cw.Close(); sr.Close(); return nil })
	defer tr.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := CallTyped(ctx, tr, "get_available_commands", nil)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Commands []struct{ Name string } `json:"commands"`
	}
	if err := json.Unmarshal(data, &got); err != nil || len(got.Commands) != 1 || got.Commands[0].Name != "switch" {
		t.Fatalf("data = %s (%v)", data, err)
	}
}

func TestCallTypedFailureHidesMessage(t *testing.T) {
	cr, cw := io.Pipe()
	sr, sw := io.Pipe()
	go fakeTypedChild(cr, sw)
	tr := Lines(sr, cw, func() error { cw.Close(); sr.Close(); return nil })
	defer tr.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := CallTyped(ctx, tr, "nope", nil)
	if !errors.Is(err, errTypedFailed) || strings.Contains(err.Error(), "Unknown") {
		t.Fatalf("err = %v", err)
	}
}

func TestCallTypedTimesOut(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	tr := Lines(r, io.Discard, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := CallTyped(ctx, tr, "get_state", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunnerFetchTypedChild(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOGO_FAKE_TYPED_CHILD", "1")
	b := spec.RPC{Method: "get_available_commands", Child: []string{exe}, Framing: spec.FramingTyped}
	raw, _ := json.Marshal(b)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := (&Runner{}).Fetch(ctx, harness.SourceSpec{Name: "commands", Kind: spec.KindRPC, Raw: raw}, harness.Env{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"switch"`) {
		t.Fatalf("out = %s", out)
	}
}
