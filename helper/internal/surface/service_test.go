package surface

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
)

type fakeRuntime struct {
	mu    sync.Mutex
	info  runtimeInfo
	data  chan []byte
	sent  []Message
	owner string
	valid bool
}

func (f *fakeRuntime) resolve(context.Context, string) (runtimeInfo, error) { return f.info, nil }
func (f *fakeRuntime) observe(ctx context.Context, _ string, _ runtimeInfo, ready func(), receive func([]byte) error) error {
	ready()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case data := <-f.data:
			if err := receive(data); err != nil {
				return err
			}
		}
	}
}
func (f *fakeRuntime) validate(context.Context, string, runtimeInfo) (CompanionState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.valid {
		return CompanionState{}, errRuntimeChanged
	}
	st := f.info.state
	st.Native, st.Owner = f.owner != "", f.owner
	return st, nil
}
func (f *fakeRuntime) control(_ context.Context, _ string, _ runtimeInfo, owner, mode string) error {
	f.mu.Lock()
	if mode == "ansi" {
		f.owner = ""
	} else {
		f.owner = owner
	}
	f.mu.Unlock()
	if mode == "native" {
		f.data <- apc(`q;{"q":"hello","v":[1],"app":"fixture","features":["send","edit"]}`)
		f.data <- apc(`o;{"id":"one","mode":"inline"}`)
		f.data <- apc(`f;{"sf":"one","s":1,"ops":[["add","input","one",null,{"id":"input","k":"input","p":{"text":"","sendable":true}}],["focus","input"]]}`)
	}
	return nil
}
func (f *fakeRuntime) send(_ context.Context, _ string, _ runtimeInfo, data []byte) error {
	var p Parser
	msgs, errs := p.Feed(data, time.Time{})
	if len(errs) != 0 || len(msgs) != 1 {
		return ErrMalformed
	}
	f.mu.Lock()
	f.sent = append(f.sent, msgs...)
	f.mu.Unlock()
	return nil
}

func nativeFixture(t testing.TB) (*Service, *fakeRuntime, *rpc.Conn, Lease) {
	t.Helper()
	root := t.TempDir()
	f := &fakeRuntime{valid: true, data: make(chan []byte, 16), info: runtimeInfo{
		config: CompanionConfig{Generation: "run", Path: filepath.Join(root, "config.json")},
		state:  CompanionState{PID: os.Getpid(), Pane: "%1", Conversation: "conversation"}, birth: "birth",
	}}
	s := newService(f, func(string, ...any) {})
	t.Cleanup(s.Stop)
	owner := &rpc.Conn{}
	lease, err := s.Open(context.Background(), owner, "s_abcdefghij", testScene().Options)
	if err != nil {
		t.Fatal(err)
	}
	for until := time.Now().Add(2 * time.Second); ; {
		chunk, err := s.Read(owner, ReadParams{ID: lease.ID, Lease: lease.Token})
		if err == nil && len(chunk.Data) != 0 {
			var scene Scene
			if json.Unmarshal(chunk.Data, &scene) == nil && len(scene.Views) == 1 && scene.Views[0].Document.Sequence == 1 {
				break
			}
		}
		if time.Now().After(until) {
			t.Fatal("native fixture did not produce a document")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return s, f, owner, lease
}

func BenchmarkUnchangedSurfaceRead(b *testing.B) {
	s, _, owner, lease := nativeFixture(b)
	first, err := s.Read(owner, ReadParams{ID: lease.ID, Lease: lease.Token})
	if err != nil {
		b.Fatal(err)
	}
	p := ReadParams{ID: lease.ID, Lease: lease.Token, Revision: first.Revision}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := s.Read(owner, p)
		if err != nil || !result.Unchanged {
			b.Fatal("unchanged read failed", err)
		}
	}
}

func TestSurfaceLeaseRejectsAnotherConnectionAndDoesNotFakeACK(t *testing.T) {
	s, f, owner, lease := nativeFixture(t)
	if _, err := s.Read(&rpc.Conn{}, ReadParams{ID: lease.ID, Lease: lease.Token}); err == nil {
		t.Fatal("another connection read the native document")
	}
	f.mu.Lock()
	for _, m := range f.sent {
		if m.Verb == 'e' {
			t.Fatal("surface parser sent a rendered ACK")
		}
	}
	f.mu.Unlock()
	if err := s.Ack(context.Background(), owner, AckParams{ID: lease.ID, Lease: lease.Token, Surface: "one", Instance: 1, Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	json.Unmarshal(f.sent[len(f.sent)-1].Body, &body)
	if body["ev"] != "ack" || body["s"] != float64(1) {
		t.Fatalf("renderer ACK did not reach the program: %v", body)
	}
}

func TestSurfaceInputRejectsStaleInstanceAndRuntimeBeforeDispatch(t *testing.T) {
	s, f, owner, lease := nativeFixture(t)
	p := InputParams{ID: lease.ID, Lease: lease.Token, Surface: "one", Instance: 2, Event: json.RawMessage(`{"ev":"send","sf":"one","id":"input","text":"hello"}`)}
	if _, err := s.Input(context.Background(), owner, p); err == nil {
		t.Fatal("stale surface instance accepted input")
	}
	p.Instance = 1
	empty := ""
	p.ExpectedText = &empty
	if _, err := s.Input(context.Background(), owner, p); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.valid = false
	f.mu.Unlock()
	if _, err := s.Input(context.Background(), owner, p); err == nil {
		t.Fatal("replaced runtime received input")
	}
}

func TestSurfaceReadChunksRemainImmutableWhileNewFramesArrive(t *testing.T) {
	s, f, owner, lease := nativeFixture(t)
	first, err := s.Read(owner, ReadParams{ID: lease.ID, Lease: lease.Token})
	if err != nil {
		t.Fatal(err)
	}
	f.data <- apc(`f;{"sf":"one","s":2,"ops":[["set","input",{"text":"new"}]]}`)
	time.Sleep(30 * time.Millisecond)
	old, err := s.Read(owner, ReadParams{ID: lease.ID, Lease: lease.Token, Snapshot: first.Snapshot})
	if err != nil || string(old.Data) != string(first.Data) || old.Revision != first.Revision {
		t.Fatalf("snapshot changed during a read: %v", err)
	}
}

func TestSurfaceCloseRestoresANSIAndRejectsOldLease(t *testing.T) {
	s, f, owner, lease := nativeFixture(t)
	if err := s.Close(context.Background(), owner, LeaseRef{ID: lease.ID, Lease: lease.Token}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	restored := f.owner == ""
	f.mu.Unlock()
	if !restored {
		t.Fatal("desktop renderer was not restored")
	}
	if _, err := s.Read(owner, ReadParams{ID: lease.ID, Lease: lease.Token}); err == nil {
		t.Fatal("closed renderer was readable")
	}
}

func TestSurfaceEditRejectsStaleTextLength(t *testing.T) {
	s, _, owner, lease := nativeFixture(t)
	p := InputParams{ID: lease.ID, Lease: lease.Token, Surface: "one", Instance: 1,
		Event: json.RawMessage(`{"ev":"edit","sf":"one","id":"input","len":5,"from":0,"to":5,"text":"replacement","cursor":11}`)}
	if _, err := s.Input(context.Background(), owner, p); err == nil {
		t.Fatal("stale editor state overwrote the draft")
	}
}

func TestSurfaceSendRequiresTheCurrentEditorText(t *testing.T) {
	s, _, owner, lease := nativeFixture(t)
	p := InputParams{ID: lease.ID, Lease: lease.Token, Surface: "one", Instance: 1,
		Event: json.RawMessage(`{"ev":"send","sf":"one","id":"input","text":"/resume"}`)}
	if _, err := s.Input(context.Background(), owner, p); err == nil {
		t.Fatal("command without a draft precondition was dispatched")
	}
}

func TestSurfaceChunksUseProtocolBase64URL(t *testing.T) {
	raw, err := json.Marshal(ReadResult{Data: []byte{255, 255}})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result["data"] != "__8" {
		t.Fatalf("chunk encoding: %v", result["data"])
	}
}

func waitSurfaceSequence(t *testing.T, s *Service, owner *rpc.Conn, lease Lease, sequence int64) {
	t.Helper()
	for until := time.Now().Add(2 * time.Second); time.Now().Before(until); {
		chunk, err := s.Read(owner, ReadParams{ID: lease.ID, Lease: lease.Token})
		if err != nil {
			t.Fatal("recoverable input ended the renderer", err)
		}
		var scene Scene
		if json.Unmarshal(chunk.Data, &scene) == nil && len(scene.Views) > 0 && scene.Views[0].Document.Sequence == sequence {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("valid frame after invalid input was not applied")
}

func TestSurfaceRecoversAtValidMessagesAfterMalformedInput(t *testing.T) {
	for name, bad := range map[string][]byte{"envelope": apc(`!;{}`), "duplicate": apc(`o;{"id":"one"}`)} {
		t.Run(name, func(t *testing.T) {
			s, f, owner, lease := nativeFixture(t)
			f.data <- append(bad, apc(`f;{"sf":"one","s":2,"ops":[["set","input",{"text":"continued"}]]}`)...)
			waitSurfaceSequence(t, s, owner, lease, 2)
		})
	}
}

func TestSurfaceBoundsRejectedOperationReplies(t *testing.T) {
	s, f, owner, lease := nativeFixture(t)
	body := `f;{"sf":"one","s":2,"ops":[` + strings.TrimSuffix(strings.Repeat(`["bad"],`, MaxOperations), ",") + `]}`
	f.data <- append(apc(body), apc(`f;{"sf":"one","s":3,"ops":[]}`)...)
	waitSurfaceSequence(t, s, owner, lease, 3)
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, m := range f.sent {
		if m.Verb == 'e' {
			count++
		}
	}
	if count > 1 {
		t.Fatalf("one invalid frame produced %d terminal round trips", count)
	}
}

func TestPendingSurfaceReadCountsAsVisibleHeartbeat(t *testing.T) {
	s, _, owner, lease := nativeFixture(t)
	r, _ := s.lookup(owner, lease.ID, lease.Token)
	r.cache.mu.Lock()
	r.cache.pending = true
	r.cache.mu.Unlock()
	r.mu.Lock()
	r.lastRead = time.Now().Add(-RendererLease)
	before := r.lastRead
	r.mu.Unlock()
	result, err := s.Read(owner, ReadParams{ID: lease.ID, Lease: lease.Token})
	if err != nil || !result.Pending {
		t.Fatal("pending read unavailable", err)
	}
	r.mu.Lock()
	after := r.lastRead
	r.mu.Unlock()
	r.cache.mu.Lock()
	r.cache.pending = false
	r.cache.mu.Unlock()
	if !after.After(before) {
		t.Fatal("visible renderer heartbeat was discarded while a frame was committing")
	}
}
