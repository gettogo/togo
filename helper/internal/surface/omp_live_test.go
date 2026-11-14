package surface_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/surface"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// This opt-in check runs a real OMP binary with an isolated home and no model
// request. ACKs below simulate display for transport testing, not app coverage.
func TestOriginalOMPTransport(t *testing.T) {
	binary := os.Getenv("TOGO_TEST_OMP")
	if binary == "" {
		t.Skip("TOGO_TEST_OMP is not set")
	}
	root := t.TempDir()
	for _, key := range []string{"HOME", "PI_CONFIG_DIR", "PI_CODING_AGENT_DIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		dir := filepath.Join(root, key)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, dir)
	}
	for _, key := range []string{"OMP_PROFILE", "PI_PROFILE", "PI_TUI_NATIVE"} {
		t.Setenv(key, "")
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "config.yml"), []byte("setupVersion: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tmux := tmuxtest.New(t)
	sessions := &session.Service{Tmux: tmux, Shell: "/bin/sh", Path: filepath.Dir(binary) + ":" + os.Getenv("PATH"), Home: os.Getenv("HOME"), LockFile: filepath.Join(root, "bib.lock")}
	sessions.Recent = &state.Recent{File: filepath.Join(root, "recent.json")}
	sessions.Manifest = &session.Manifest{Path: filepath.Join(root, "sessions.json")}
	home := filepath.Join(root, "togo")
	companions := &surface.Companions{Home: home}
	sessions.PrepareLaunch = companions.PrepareLaunch
	created, err := sessions.Create(ctx, session.CreateOpts{Agent: "omp", Cwd: root, Args: []string{"--no-skills", "--no-context-files", "--model", "opencode/iceman-free"}, Cols: 100, Rows: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer sessions.Kill(context.Background(), created.ID)
	var config surface.CompanionConfig
	var before surface.CompanionState
	for {
		config, err = companions.Current(created.ID)
		if err == nil {
			before, err = companions.State(config)
		}
		if err == nil && before.Available {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("original companion did not become available", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
	svc := surface.NewService(home, sessions, t.Logf)
	defer svc.Stop()
	owner := &rpc.Conn{}
	options := surface.RenderOptions{Cols: 100, Dark: true, Kinds: []string{"col", "row", "text", "md", "ansi", "rows", "editor", "input", "button", "card", "overlay", "list", "item", "picker", "tabs", "spinner", "badge", "kbd", "rule", "spacer", "field", "buttons", "select", "icon", "image", "meter", "status", "toast"}}
	lease, err := svc.Open(ctx, owner, created.ID, options)
	if err != nil {
		st, stateErr := companions.State(config)
		t.Logf("companion native=%v available=%v reason=%q epoch=%d seq=%d read=%v", st.Native, st.Available, st.Reason, st.Epoch, st.Seq, stateErr)
		t.Fatal(err)
	}
	var scene surface.Scene
	for {
		chunk, err := svc.Read(owner, surface.ReadParams{ID: created.ID, Lease: lease.Token})
		if err != nil {
			st, _ := companions.State(config)
			t.Logf("document read failed: native=%v available=%v reason=%q seq=%d", st.Native, st.Available, st.Reason, st.Seq)
			t.Fatal(err)
		}
		if chunk.Total > surface.ReadChunkBytes {
			t.Fatal("unexpectedly large empty interface")
		}
		if json.Unmarshal(chunk.Data, &scene) == nil && scene.Program != "" {
			ready := false
			for _, v := range scene.Views {
				ready = ready || (!v.Closed && v.Document.Sequence > 0)
			}
			if ready {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("no original OMP document")
		case <-time.After(50 * time.Millisecond):
		}
	}
	for _, v := range scene.Views {
		if v.Closed {
			continue
		}
		if err := svc.Ack(ctx, owner, surface.AckParams{ID: created.ID, Lease: lease.Token, Surface: v.ID, Instance: v.Instance, Sequence: v.Document.Sequence}); err != nil {
			t.Fatal(err)
		}
	}
	var editor *surface.Node
	var editorView *surface.View
	for _, v := range scene.Views {
		if !v.Closed {
			if node := findNativeNode(v.Document.Root, func(n *surface.Node) bool { return n.ID == v.Document.Focus && n.Kind == "editor" }); node != nil {
				editor, editorView = node, v
			}
		}
	}
	if editor == nil || editor.Props["text"] != "" {
		t.Fatal("fresh original editor is not ready")
	}
	command, _ := json.Marshal(map[string]any{"ev": "send", "sf": editorView.ID, "id": editor.ID, "text": "/resume"})
	empty := ""
	if _, err := svc.Input(ctx, owner, surface.InputParams{ID: created.ID, Lease: lease.Token, Surface: editorView.ID, Instance: editorView.Instance, Event: command, ExpectedText: &empty}); err != nil {
		t.Fatal(err)
	}
	for {
		chunk, err := svc.Read(owner, surface.ReadParams{ID: created.ID, Lease: lease.Token})
		if err != nil {
			t.Fatal(err)
		}
		var next surface.Scene
		if json.Unmarshal(chunk.Data, &next) == nil {
			picker := false
			for _, v := range next.Views {
				if v.Closed {
					continue
				}
				if err := svc.Ack(ctx, owner, surface.AckParams{ID: created.ID, Lease: lease.Token, Surface: v.ID, Instance: v.Instance, Sequence: v.Document.Sequence}); err != nil {
					t.Fatal(err)
				}
				picker = picker || findNativeNode(v.Document.Root, func(n *surface.Node) bool { return n.Kind == "picker" }) != nil
			}
			if picker {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("native resume command did not open the original picker")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err := svc.Close(ctx, owner, surface.LeaseRef{ID: created.ID, Lease: lease.Token}); err != nil {
		t.Fatal(err)
	}
	// Reacquire after the observer has stopped. Close APCs may have been missed;
	// the held inline document must still be adoptable on a fresh handshake.
	second, err := svc.Open(ctx, owner, created.ID, options)
	if err != nil {
		t.Fatal("native reopen failed", err)
	}
	for {
		chunk, err := svc.Read(owner, surface.ReadParams{ID: created.ID, Lease: second.Token})
		if err != nil {
			t.Fatal(err)
		}
		var current surface.Scene
		if json.Unmarshal(chunk.Data, &current) == nil {
			ready := false
			for _, v := range current.Views {
				ready = ready || !v.Closed && v.Document.Sequence > 0
			}
			if ready {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("reopened OMP did not restore a live document")
		case <-time.After(30 * time.Millisecond):
		}
	}
	if err := svc.Close(ctx, owner, surface.LeaseRef{ID: created.ID, Lease: second.Token}); err != nil {
		t.Fatal(err)
	}
	after, err := companions.State(config)
	if err != nil || after.Native || before.PID != after.PID || before.Conversation != after.Conversation {
		t.Fatalf("original runtime not restored: %v", err)
	}
	t.Logf("original process retained; native resume picker opened and renderer released")
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
