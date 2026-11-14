package surface

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
)

func TestCompanionLaunchPreservesArgumentsAndKeepsSecretsOutOfArgv(t *testing.T) {
	c := &Companions{Home: t.TempDir()}
	old := session.Launch{Program: "omp", Args: []string{"--profile", "work", "--", "literal"}, Env: map[string]string{"EXISTING": "kept"}}
	got, err := c.PrepareLaunch(context.Background(), "s_abcdefghij", "omp", old)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old.Args, []string{"--profile", "work", "--", "literal"}) || got.Env["EXISTING"] != "kept" {
		t.Fatal("companion preparation modified the original launch")
	}
	if len(got.Args) != 6 || !reflect.DeepEqual(got.Args[:2], old.Args[:2]) || got.Args[2] != "-e" || got.Args[4] != "--" || got.Args[5] != "literal" {
		t.Fatalf("launch arguments changed: %v", got.Args)
	}
	current, err := c.Current("s_abcdefghij")
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Token) != 64 || len(current.Generation) != 32 || got.Env["TOGO_SURFACE_CONFIG"] != current.Path {
		t.Fatal("missing private per-run configuration")
	}
	for _, arg := range got.Args {
		if arg == current.Token {
			t.Fatal("companion secret leaked into argv")
		}
	}
	info, err := os.Stat(current.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("configuration permissions: %v %v", info, err)
	}
}

func TestCompanionSkipsNoninteractiveAndOtherHarnessLaunches(t *testing.T) {
	c := &Companions{Home: t.TempDir()}
	for _, args := range [][]string{{"--mode", "rpc"}, {"--mode=rpc"}, {"--print", "hello"}, {"config", "list"}} {
		old := session.Launch{Program: "omp", Args: args}
		got, err := c.PrepareLaunch(context.Background(), "s_abcdefghij", "omp", old)
		if err != nil || !reflect.DeepEqual(got, old) {
			t.Fatalf("noninteractive launch changed: %v %v", args, err)
		}
	}
	old := session.Launch{Program: "pi"}
	got, err := c.PrepareLaunch(context.Background(), "s_abcdefghij", "pi", old)
	if err != nil || !reflect.DeepEqual(got, old) {
		t.Fatal("another harness received the OMP companion")
	}
}

func TestCompanionRejectsForeignAndOversizedState(t *testing.T) {
	c := &Companions{Home: t.TempDir()}
	_, err := c.PrepareLaunch(context.Background(), "s_abcdefghij", "omp", session.Launch{Program: "omp"})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := c.Current("s_abcdefghij")
	raw, _ := json.Marshal(CompanionState{Generation: "foreign", PID: os.Getpid(), Pane: "%0", Available: true})
	os.WriteFile(filepath.Join(filepath.Dir(run.Path), "state.json"), raw, 0o600)
	if _, err := c.State(run); err == nil {
		t.Fatal("foreign run state accepted")
	}
	os.WriteFile(filepath.Join(filepath.Dir(run.Path), "state.json"), make([]byte, MaxCompanionBytes+1), 0o600)
	if _, err := c.State(run); err == nil {
		t.Fatal("oversized companion state accepted")
	}
}

func TestOMPCompanionControlAndLease(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	cmd := exec.Command(node, "--test", "testdata/omp-companion.test.mjs")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("companion control: %v\n%s", err, out)
	}
}

func TestCompanionDispatchBoundsEncodedTextAndWaitsForAcceptance(t *testing.T) {
	c := &Companions{Home: t.TempDir()}
	_, err := c.PrepareLaunch(context.Background(), "s_abcdefghij", "omp", session.Launch{Program: "omp"})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := c.Current("s_abcdefghij")
	before := CompanionState{Version: 1, Generation: run.Generation, PID: os.Getpid(), Pane: "%0",
		Conversation: "one", Available: true, Native: true, Owner: "renderer", Seq: 1}
	file := filepath.Join(filepath.Dir(run.Path), "command.json")
	result := make(chan error, 1)
	body := json.RawMessage(`{"ev":"edit","sf":"one","id":"input","text":"` + strings.Repeat("<", 60<<10) + `"}`)
	go func() { result <- c.Dispatch(context.Background(), run, before, before.Owner, body) }()
	deadline := time.Now().Add(CompanionWait)
	var command []byte
	for {
		command, err = os.ReadFile(file)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command was not written")
		}
		time.Sleep(time.Millisecond)
	}
	if len(command) < 6*(60<<10) || len(command) > MaxCompanionBytes+6*MaxAPCBytes {
		t.Fatal("escaped text exceeded the companion bound", len(command))
	}
	var received struct {
		Event json.RawMessage `json:"event"`
	}
	if json.Unmarshal(command, &received) != nil {
		t.Fatal("invalid command")
	}
	var event struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(received.Event, &event) != nil || event.Text != strings.Repeat("<", 60<<10) {
		t.Fatal("command changed literal text")
	}
	select {
	case err := <-result:
		t.Fatal("dispatch completed before acceptance", err)
	default:
	}
	before.Seq++
	if err := writeJSON(filepath.Join(filepath.Dir(run.Path), "state.json"), before); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
