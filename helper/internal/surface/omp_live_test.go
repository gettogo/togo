package surface_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

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
	limit := 30 * time.Second
	if os.Getenv("TOGO_SURFACE_RECORD_DIR") != "" {
		limit = 4 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
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
	checkUnstartedReplacement(t, ctx, companions, created.ID, binary, config.Generation)
	svc := surface.NewService(home, sessions, t.Logf)
	t.Logf("original OMP pid=%d", before.PID)
	defer svc.Stop()
	defer func() {
		if t.Failed() {
			st, stateErr := companions.State(config)
			t.Logf("final companion native=%v available=%v reason=%q epoch=%d seq=%d read=%v", st.Native, st.Available, st.Reason, st.Epoch, st.Seq, stateErr)
		}
	}()
	owner := &rpc.Conn{}
	// Match the app's implemented vocabulary, including the model preview's
	// structured facts. A deliberately smaller profile cannot prove this flow.
	options := surface.RenderOptions{Cols: 100, Dark: true, Kinds: []string{
		"col", "row", "card", "section", "rule", "spacer", "text", "md", "code", "diff", "badge", "kbd",
		"icon", "spinner", "progress", "list", "item", "tabs", "editor", "input", "button", "status", "seg",
		"overlay", "toast", "picker", "prefs", "image", "meter", "kv", "table",
	}}
	if !svc.Available(ctx, created.ID) || svc.Held(created.ID) {
		t.Fatal("live availability probe acquired or missed the original runtime")
	}
	lease, err := svc.Open(ctx, owner, created.ID, options)
	if err != nil {
		st, stateErr := companions.State(config)
		t.Logf("companion native=%v available=%v reason=%q epoch=%d seq=%d read=%v", st.Native, st.Available, st.Reason, st.Epoch, st.Seq, stateErr)
		t.Fatal(err)
	}
	if !svc.Held(created.ID) {
		t.Fatal("active native lease not visible to typing guard")
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
		if err := liveAck(ctx, svc, owner, surface.AckParams{ID: created.ID, Lease: lease.Token, Surface: v.ID, Instance: v.Instance, Sequence: v.Document.Sequence}); err != nil {
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
	copyMode := func(args ...string) {
		t.Helper()
		argv := append(append([]string{}, tmux.Socket...), args...)
		if out, err := exec.CommandContext(ctx, tmux.Bin, argv...).CombinedOutput(); err != nil {
			t.Fatalf("owned pane mode change: %v: %.200s", err, out)
		}
	}
	copyMode("copy-mode", "-t", before.Pane)
	if ready, err := tmux.SurfaceReplyReady(ctx, before.Pane, lease.Token); err != nil || ready {
		t.Fatalf("copy mode must refuse raw handshake input: %v", err)
	}
	previous := ""
	for _, size := range []int{20 << 10, 60 << 10, 0} {
		started := time.Now()
		text := ""
		if size > 0 {
			text = strings.Repeat("x", size) + "🍍"
			if size > 20<<10 {
				text = strings.Repeat("an example line\n", size/16) + "🍍"
			}
		}
		body, _ := json.Marshal(map[string]any{"ev": "edit", "sf": editorView.ID, "id": editor.ID, "from": 0,
			"to": len(utf16.Encode([]rune(previous))), "len": len(utf16.Encode([]rune(previous))),
			"text": text, "cursor": len(utf16.Encode([]rune(text)))})
		if _, err := liveInput(ctx, svc, owner, surface.InputParams{ID: created.ID, Lease: lease.Token, Surface: editorView.ID, Instance: editorView.Instance, Event: body}); err != nil {
			t.Fatalf("native edit of %d bytes failed: %v", len(text), err)
		}
		t.Logf("edit %d bytes dispatched in %v", len(text), time.Since(started))
		for {
			current := readLiveScene(t, ctx, svc, owner, lease)
			var actual *surface.Node
			for _, v := range current.Views {
				if v.Closed {
					continue
				}
				actual = findNativeNode(v.Document.Root, func(n *surface.Node) bool { return n.ID == editor.ID })
				if err := liveAck(ctx, svc, owner, surface.AckParams{ID: created.ID, Lease: lease.Token, Surface: v.ID, Instance: v.Instance, Sequence: v.Document.Sequence}); err != nil {
					t.Fatal(err)
				}
			}
			if actual != nil && actual.Props["text"] == text {
				t.Logf("edit %d bytes observed in %v", len(text), time.Since(started))
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("native edit did not reach the original editor")
			case <-time.After(20 * time.Millisecond):
			}
		}
		previous = text
	}
	copyMode("send-keys", "-t", before.Pane, "-X", "cancel")
	if dir := os.Getenv("TOGO_SURFACE_RECORD_DIR"); dir != "" {
		// A saved conversation to list, copied into this session's own
		// folder with its working folder rewritten to this test's.
		if seed := os.Getenv("TOGO_SURFACE_SEED_SESSION"); seed != "" {
			now, _ := companions.State(config)
			raw, err := os.ReadFile(seed)
			if err != nil || now.SessionFile == "" {
				t.Fatalf("seed session: %v (session file %q)", err, now.SessionFile)
			}
			if from := os.Getenv("TOGO_SURFACE_SEED_CWD"); from != "" {
				raw = []byte(strings.ReplaceAll(string(raw), from, root))
			}
			if err := os.MkdirAll(filepath.Dir(now.SessionFile), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(now.SessionFile), filepath.Base(seed)), raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		recordLiveScreens(t, ctx, svc, owner, lease, dir, strings.Fields(os.Getenv("TOGO_SURFACE_RECORD_COMMANDS")))
		return
	}
	command, _ := json.Marshal(map[string]any{"ev": "send", "sf": editorView.ID, "id": editor.ID, "text": "/resume"})
	empty := ""
	sent, err := liveInput(ctx, svc, owner, surface.InputParams{ID: created.ID, Lease: lease.Token, Surface: editorView.ID, Instance: editorView.Instance, Event: command, ExpectedText: &empty})
	// The program's own editor submitted the command: the receipt, which
	// arrives whether or not the picker has been drawn yet.
	if err == nil && (sent.Consumed == nil || !*sent.Consumed) {
		t.Errorf("/resume was dispatched without the program's submit receipt: %+v", sent)
	}
	if err != nil {
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
				if err := liveAck(ctx, svc, owner, surface.AckParams{ID: created.ID, Lease: lease.Token, Surface: v.ID, Instance: v.Instance, Sequence: v.Document.Sequence}); err != nil {
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
	checkLivePickerQuery(t, ctx, svc, owner, lease)
	for _, command := range []string{"/switch", "/model"} {
		openNextLivePicker(t, ctx, svc, owner, lease, command)
		checkLivePickerQuery(t, ctx, svc, owner, lease)
		t.Logf("original %s picker query checked", command)
	}
	if err := svc.Close(ctx, owner, surface.LeaseRef{ID: created.ID, Lease: lease.Token}); err != nil {
		t.Fatal(err)
	}
	if svc.Held(created.ID) || !svc.Available(ctx, created.ID) {
		t.Fatal("close did not release typing guard while preserving original runtime")
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
	checkLiveColorReplyHandback(t, ctx, tmux, companions, config)
	if err := svc.Close(ctx, owner, surface.LeaseRef{ID: created.ID, Lease: second.Token}); err != nil {
		t.Fatal(err)
	}
	after, err := companions.State(config)
	if err != nil || after.Native || before.PID != after.PID || before.Conversation != after.Conversation {
		t.Fatalf("original runtime not restored: %v", err)
	}
	t.Logf("original process retained; native resume picker opened and renderer released")
}
