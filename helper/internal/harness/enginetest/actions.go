package enginetest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// The app's harness.* calls, through the daemon's method table
// (harness.Methods), so their parameter checks run too.

func jsonInto(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// Send is harness.send; mode is harness.SendNow or harness.SendQueue.
func (r *Rig) Send(text, mode string) (harness.SendResult, error) {
	var res harness.SendResult
	raw, err := r.Call("harness.send", harness.SendRequest{Session: r.ID, Text: text, Mode: mode})
	if err != nil {
		return res, err
	}
	return res, jsonInto(raw, &res)
}

// Act is harness.action. args are encoded to JSON as the app sends them.
func (r *Rig) Act(action, target string, args map[string]any) (harness.ActionResult, error) {
	var res harness.ActionResult
	req := harness.ActionRequest{Session: r.ID, Action: action, Target: target}
	if len(args) > 0 {
		req.Args = map[string]json.RawMessage{}
		for k, v := range args {
			b, err := json.Marshal(v)
			if err != nil {
				r.T.Fatal(err)
			}
			req.Args[k] = b
		}
	}
	raw, err := r.Call("harness.action", req)
	if err != nil {
		return res, err
	}
	return res, jsonInto(raw, &res)
}

// TakeBack cancels a queued prompt, as the app's stop on a queued item.
func (r *Rig) TakeBack(itemID string) (harness.ActionResult, error) {
	return r.Act(harness.ActionStop, itemID, nil)
}

// Complete is harness.complete (harness.CompleteCommand or CompleteFile).
func (r *Rig) Complete(kind, query string) (harness.CompleteResult, error) {
	var res harness.CompleteResult
	raw, err := r.Call("harness.complete", harness.CompleteRequest{Session: r.ID, Kind: kind, Query: query})
	if err != nil {
		return res, err
	}
	return res, jsonInto(raw, &res)
}

// Describe is harness.describe.
func (r *Rig) Describe() harness.Descriptor {
	r.T.Helper()
	var d harness.Descriptor
	raw, err := r.Call("harness.describe", map[string]string{"id": r.ID})
	if err != nil {
		r.T.Fatalf("describe: %v", err)
	}
	if err := jsonInto(raw, &d); err != nil {
		r.T.Fatal(err)
	}
	return d
}

// Screen is harness.screen.
func (r *Rig) Screen(name string, query map[string]string) (harness.Screen, error) {
	var sc harness.Screen
	raw, err := r.Call("harness.screen", map[string]any{"id": r.ID, "screen": name, "query": query})
	if err != nil {
		return sc, err
	}
	return sc, jsonInto(raw, &sc)
}

// Spec is the agent's adapter file as parsed; with several (one per major
// version) the newest by name.
func (r *Rig) Spec() *spec.Adapter {
	r.T.Helper()
	paths, _ := filepath.Glob(filepath.Join(sourceDir(), "harness", "adapters", r.Opt.Agent, "*.yaml"))
	sort.Strings(paths)
	if len(paths) == 0 {
		r.T.Fatalf("no adapter file for %s", r.Opt.Agent)
	}
	b, err := os.ReadFile(paths[len(paths)-1])
	if err != nil {
		r.T.Fatal(err)
	}
	a, err := spec.Parse(b)
	if err != nil {
		r.T.Fatal(err)
	}
	return a
}

// ActionNames lists the actions the adapter declares, sorted.
func (r *Rig) ActionNames() []string { return ActionNames(r.Opt.Agent) }

// ActionNames lists the actions an agent's adapter file declares, sorted;
// nil when it has none or no file.
func ActionNames(agent string) []string {
	paths, _ := filepath.Glob(filepath.Join(sourceDir(), "harness", "adapters", agent, "*.yaml"))
	sort.Strings(paths)
	if f, ok := adapterFiles[agent]; ok {
		paths = []string{filepath.Join(sourceDir(), "harness", "adapters", agent, f)}
	}
	if len(paths) == 0 {
		return nil
	}
	b, err := os.ReadFile(paths[len(paths)-1])
	if err != nil {
		return nil
	}
	a, err := spec.Parse(b)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(a.Actions))
	for k := range a.Actions {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CLICalls lists the agent binary's calls an action made (the version
// check aside), one argv joined by spaces per call. The fake binary
// fails each of them, as an unknown subcommand does.
func (r *Rig) CLICalls() []string {
	b, err := os.ReadFile(filepath.Join(r.bin, "calls.log"))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}
