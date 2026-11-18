package surface_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/surface"
)

func openNextLivePicker(t *testing.T, ctx context.Context, svc *surface.Service, owner *rpc.Conn, lease surface.Lease, command string) {
	t.Helper()
	closed := false
	for {
		scene := readLiveScene(t, ctx, svc, owner, lease)
		var picker, editor *surface.Node
		var pickerView, editorView *surface.View
		for _, v := range scene.Views {
			if v.Closed {
				continue
			}
			if n := findNativeNode(v.Document.Root, func(n *surface.Node) bool { return n.Kind == "picker" }); n != nil {
				picker, pickerView = n, v
			}
			if n := findNativeNode(v.Document.Root, func(n *surface.Node) bool { return n.Kind == "editor" && n.ID == v.Document.Focus }); n != nil {
				editor, editorView = n, v
			}
			if err := liveAck(ctx, svc, owner, surface.AckParams{ID: lease.ID, Lease: lease.Token, Surface: v.ID,
				Instance: v.Instance, Sequence: v.Document.Sequence}); err != nil {
				t.Fatal(err)
			}
		}
		if picker != nil && !closed {
			data, _ := json.Marshal(map[string]any{"ev": "action", "sf": pickerView.ID, "id": picker.ID, "act": "close"})
			if _, err := liveInput(ctx, svc, owner, surface.InputParams{ID: lease.ID, Lease: lease.Token, Surface: pickerView.ID,
				Instance: pickerView.Instance, Event: data}); err != nil {
				t.Fatal(err)
			}
			closed = true
		} else if picker == nil && editor != nil {
			empty := ""
			data, _ := json.Marshal(map[string]any{"ev": "send", "sf": editorView.ID, "id": editor.ID, "text": command})
			if _, err := liveInput(ctx, svc, owner, surface.InputParams{ID: lease.ID, Lease: lease.Token, Surface: editorView.ID,
				Instance: editorView.Instance, ExpectedText: &empty, Event: data}); err != nil {
				t.Fatal(err)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("original editor did not return after closing picker")
		case <-time.After(20 * time.Millisecond):
		}
	}
	for {
		scene := readLiveScene(t, ctx, svc, owner, lease)
		for _, v := range scene.Views {
			if !v.Closed && findNativeNode(v.Document.Root, func(n *surface.Node) bool { return n.Kind == "picker" }) != nil {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("original command did not open picker", command)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func checkLivePickerQuery(t *testing.T, ctx context.Context, svc *surface.Service, owner *rpc.Conn, lease surface.Lease) {
	t.Helper()
	for _, text := range []string{"100%! <div> /resume 🧑🏽‍💻", ""} {
		current := readLiveScene(t, ctx, svc, owner, lease)
		var picker *surface.Node
		var view *surface.View
		for _, v := range current.Views {
			if !v.Closed {
				if n := findNativeNode(v.Document.Root, func(n *surface.Node) bool { return n.Kind == "picker" }); n != nil {
					picker, view = n, v
				}
			}
		}
		if picker == nil {
			t.Fatal("original picker absent before query")
		}
		beforeRows := pickerVisibleRows(picker)
		event, _ := json.Marshal(map[string]any{"ev": "action", "act": "togo.query", "sf": view.ID,
			"id": picker.ID, "before": picker.Props["query"], "query": text})
		if _, err := liveInput(ctx, svc, owner, surface.InputParams{ID: lease.ID, Lease: lease.Token, Surface: view.ID,
			Instance: view.Instance, Event: event}); err != nil {
			t.Fatal("original picker query refused", err)
		}
		for {
			next := readLiveScene(t, ctx, svc, owner, lease)
			var shown *surface.Node
			for _, v := range next.Views {
				if v.Closed {
					continue
				}
				if n := findNativeNode(v.Document.Root, func(n *surface.Node) bool { return n.ID == picker.ID }); n != nil {
					shown = n
				}
				if err := liveAck(ctx, svc, owner, surface.AckParams{ID: lease.ID, Lease: lease.Token, Surface: v.ID,
					Instance: v.Instance, Sequence: v.Document.Sequence}); err != nil {
					t.Fatal(err)
				}
			}
			if shown != nil && shown.Props["query"] == text {
				rows := pickerVisibleRows(shown)
				if text != "" && rows != 0 {
					t.Fatal("literal unmatched query kept visible rows", rows)
				}
				t.Logf("original picker query %q: visible rows %d -> %d", text, beforeRows, rows)
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("original picker query did not change")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	t.Log("original picker query updated and cleared as literal text")
}

func pickerVisibleRows(picker *surface.Node) int {
	n := 0
	if order, ok := picker.Props["order"].([]any); ok {
		for _, entry := range order {
			if _, ok := entry.(string); ok {
				n++
			}
		}
	} else if items, ok := picker.Props["items"].([]any); ok {
		n = len(items)
	}
	return n
}

func findNativeNode(n *surface.Node, match func(*surface.Node) bool) *surface.Node {
	if match(n) {
		return n
	}
	for _, child := range n.Children {
		if found := findNativeNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func readLiveScene(t *testing.T, ctx context.Context, svc *surface.Service, owner *rpc.Conn, lease surface.Lease) surface.Scene {
	t.Helper()
	for {
		chunk, err := svc.Read(owner, surface.ReadParams{ID: lease.ID, Lease: lease.Token})
		if err != nil {
			t.Fatal(err)
		}
		if chunk.Pending {
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(5 * time.Millisecond):
			}
			continue
		}
		data := append([]byte(nil), chunk.Data...)
		for chunk.NextOffset < chunk.Total {
			chunk, err = svc.Read(owner, surface.ReadParams{ID: lease.ID, Lease: lease.Token, Snapshot: chunk.Snapshot, Offset: chunk.NextOffset})
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, chunk.Data...)
		}
		var scene surface.Scene
		if err := json.Unmarshal(data, &scene); err != nil {
			t.Fatal(err)
		}
		return scene
	}
}

// Explicit cache-pending refusals precede dispatch. Match the app's bounded
// admission wait; every uncertain or post-dispatch error still stops the check.
func livePending(ctx context.Context, call func() error) error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := call()
		if !errors.Is(err, surface.ErrCachePending) || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
func liveInput(ctx context.Context, svc *surface.Service, owner *rpc.Conn, p surface.InputParams) (result surface.DispatchResult, err error) {
	err = livePending(ctx, func() error { var e error; result, e = svc.Input(ctx, owner, p); return e })
	return
}
func liveAck(ctx context.Context, svc *surface.Service, owner *rpc.Conn, p surface.AckParams) error {
	return livePending(ctx, func() error { return svc.Ack(ctx, owner, p) })
}
