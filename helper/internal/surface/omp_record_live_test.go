package surface_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/surface"
)

// recordLiveScreens runs each command in the original program and saves the
// scene it settles on, for mapping tests. It is a recorder, not a check: a
// command that shows nothing, or a screen that will not close, is logged.
func recordLiveScreens(t *testing.T, ctx context.Context, svc *surface.Service, owner *rpc.Conn, lease surface.Lease, dir string, commands []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	read := func() (surface.Scene, []byte) {
		scene := readLiveScene(t, ctx, svc, owner, lease)
		for _, v := range scene.Views {
			if !v.Closed {
				_ = liveAck(ctx, svc, owner, surface.AckParams{ID: lease.ID, Lease: lease.Token, Surface: v.ID, Instance: v.Instance, Sequence: v.Document.Sequence})
			}
		}
		raw, _ := json.MarshalIndent(scene, "", " ")
		return scene, raw
	}
	focusedEditor := func(scene surface.Scene) (*surface.View, *surface.Node) {
		for _, v := range scene.Views {
			if v.Closed || !v.Listen || v.Document.Suspended {
				continue
			}
			if n := findNativeNode(v.Document.Root, func(n *surface.Node) bool {
				return n.Kind == "editor" && n.ID == v.Document.Focus && n.Props["sendable"] == true
			}); n != nil {
				return v, n
			}
		}
		return nil, nil
	}
	settle := func(quiet, limit time.Duration) (surface.Scene, []byte) {
		scene, raw := read()
		last, deadline := time.Now(), time.Now().Add(limit)
		for time.Since(last) < quiet && time.Now().Before(deadline) && ctx.Err() == nil {
			time.Sleep(60 * time.Millisecond)
			next, nextRaw := read()
			if string(nextRaw) != string(raw) {
				scene, raw, last = next, nextRaw, time.Now()
			}
		}
		return scene, raw
	}
	_, idle := settle(500*time.Millisecond, 3*time.Second)
	_ = os.WriteFile(filepath.Join(dir, "idle.json"), idle, 0o600)
	for _, command := range commands {
		scene, _ := settle(300*time.Millisecond, 2*time.Second)
		view, editor := focusedEditor(scene)
		if editor == nil {
			t.Logf("%s: no sendable editor in focus; stopping", command)
			return
		}
		text, _ := editor.Props["text"].(string)
		event, _ := json.Marshal(map[string]any{"ev": "send", "sf": view.ID, "id": editor.ID, "text": command})
		result, err := liveInput(ctx, svc, owner, surface.InputParams{ID: lease.ID, Lease: lease.Token, Surface: view.ID, Instance: view.Instance, Event: event, ExpectedText: &text})
		if err != nil {
			t.Logf("%s: not sent: %v", command, err)
			continue
		}
		shown, raw := settle(900*time.Millisecond, 6*time.Second)
		name := strings.NewReplacer("/", "", " ", "_", ":", "-").Replace(command)
		_ = os.WriteFile(filepath.Join(dir, name+".json"), raw, 0o600)
		kinds := map[string]int{}
		for _, v := range shown.Views {
			if !v.Closed {
				findNativeNode(v.Document.Root, func(n *surface.Node) bool { kinds[n.Kind]++; return false })
			}
		}
		_, stillEditor := focusedEditor(shown)
		t.Logf("%s: consumed=%v views=%d editorFocused=%v kinds=%v bytes=%d", command, result.Consumed != nil, len(shown.Views), stillEditor != nil, kinds, len(raw))
		// Scripted follow-ups: each step sends one event to the first node of
		// a kind (or to the focused node) and saves the scene it settles on.
		var steps []struct {
			On    string         `json:"on"`
			Event map[string]any `json:"event"`
			Text  *string        `json:"expectedText"`
		}
		if raw := os.Getenv("TOGO_SURFACE_RECORD_SCRIPT"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &steps); err != nil {
				t.Fatal(err)
			}
		}
		for i, step := range steps {
			now, _ := settle(300*time.Millisecond, 2*time.Second)
			var view *surface.View
			var target *surface.Node
			for _, v := range now.Views {
				if v.Closed || !v.Listen {
					continue
				}
				if n := findNativeNode(v.Document.Root, func(n *surface.Node) bool {
					if strings.HasPrefix(step.On, "id:") {
						return n.ID == strings.TrimPrefix(step.On, "id:")
					}
					if step.On == "focus" {
						return n.ID == v.Document.Focus
					}
					return n.Kind == step.On
				}); n != nil {
					view, target = v, n
				}
			}
			if target == nil {
				t.Logf("%s step %d: no %q node", command, i, step.On)
				break
			}
			event := map[string]any{"sf": view.ID, "id": target.ID}
			for k, v := range step.Event {
				event[k] = v
			}
			data, _ := json.Marshal(event)
			result, err := liveInput(ctx, svc, owner, surface.InputParams{ID: lease.ID, Lease: lease.Token, Surface: view.ID, Instance: view.Instance, Event: data, ExpectedText: step.Text})
			after, afterRaw := settle(900*time.Millisecond, 5*time.Second)
			_ = os.WriteFile(filepath.Join(dir, name+"."+string(rune('1'+i))+".json"), afterRaw, 0o600)
			stepKinds := map[string]int{}
			for _, v := range after.Views {
				if !v.Closed {
					findNativeNode(v.Document.Root, func(n *surface.Node) bool { stepKinds[n.Kind]++; return false })
				}
			}
			_, backAtEditor := focusedEditor(after)
			t.Logf("%s step %d %s on %s(%s): dispatched=%v err=%v held=%v editorFocused=%v kinds=%v", command, i, data, target.Kind, target.ID, result.Dispatched, err, svc.Held(lease.ID), backAtEditor, stepKinds)
		}
		if len(steps) > 0 {
			return
		}
		// Close whatever opened: the program's own close action on each
		// open view's interactive root, until the editor has focus again.
		for attempt := 0; attempt < 6 && ctx.Err() == nil; attempt++ {
			now, _ := settle(250*time.Millisecond, 1500*time.Millisecond)
			if _, e := focusedEditor(now); e != nil {
				break
			}
			for _, v := range now.Views {
				if v.Closed || !v.Listen {
					continue
				}
				target := findNativeNode(v.Document.Root, func(n *surface.Node) bool {
					return n.Kind == "picker" || n.Kind == "prefs" || n.Kind == "overlay" || n.ID == v.Document.Focus
				})
				if target == nil {
					continue
				}
				for _, act := range []string{"close", "cancel", "back"} {
					data, _ := json.Marshal(map[string]any{"ev": "action", "sf": v.ID, "id": target.ID, "act": act})
					_, _ = liveInput(ctx, svc, owner, surface.InputParams{ID: lease.ID, Lease: lease.Token, Surface: v.ID, Instance: v.Instance, Event: data})
				}
			}
		}
	}
}
