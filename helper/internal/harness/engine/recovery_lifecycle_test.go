package engine

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/session"
)

// This runs in a separate helper process. No in-memory endpoint or child
// handle is available to it; only the private runtime record survives.
func TestRecoveredEngineProcess(t *testing.T) {
	home := os.Getenv("TOGO_RECOVERY_TEST_HOME")
	if home == "" {
		return
	}
	e := New(Config{TogoHome: home})
	const id = "s_abcdefghij"
	ep, ok := e.endpointFor(id)
	if !ok || checkHealth(context.Background(), ep, "initialize") != nil {
		t.Fatal("replacement process could not use the original server")
	}
	e.Forget(id)
}

func TestRecoveryAcrossHelperProcessesKeepsOtherSession(t *testing.T) {
	const first, second = "s_abcdefghij", "s_abcdefghik"
	e := fakeServerEngine(t, "serve")
	for _, id := range []string{first, second} {
		if e.EndpointArgs("fakesrv", id, "") == nil {
			t.Fatal("server did not start")
		}
		t.Cleanup(func() { e.Forget(id) })
	}
	other, _ := e.endpointFor(second)
	record, err := e.readEndpointRecord(first)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestRecoveredEngineProcess$", "-test.timeout=20s")
	child.Env = append(os.Environ(), "TOGO_RECOVERY_TEST_HOME="+e.cfg.TogoHome)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("replacement helper: %v\n%s", err, out)
	}
	waitFor(t, func() bool { return !record.Server.alive() })
	if checkHealth(context.Background(), other, "initialize") != nil {
		t.Fatal("removing one recovered session stopped the other")
	}
}

func TestRecoveryRejectsAReusedPID(t *testing.T) {
	const id = "s_abcdefghij"
	e := fakeServerEngine(t, "serve")
	if e.EndpointArgs("fakesrv", id, "") == nil {
		t.Fatal("server did not start")
	}
	t.Cleanup(func() { e.Forget(id) })
	record, err := e.readEndpointRecord(id)
	if err != nil {
		t.Fatal(err)
	}
	record.Server.Birth += "-different-process"
	if err := e.writeEndpointRecord(record); err != nil {
		t.Fatal(err)
	}
	next := New(e.cfg)
	if _, ok := next.endpointFor(id); ok {
		t.Fatal("accepted a PID with a different kernel birth identity")
	}
	next.Forget(id)
	e.srv.mu.Lock()
	owner := e.srv.owners[id]
	e.srv.mu.Unlock()
	if !owner.alive() {
		t.Fatal("signaled a process using a stale PID identity")
	}
}

func TestRecoveryDoesNotUnlinkAReplacementSocket(t *testing.T) {
	const id = "s_abcdefghij"
	e := fakeServerEngine(t, "serve")
	if e.EndpointArgs("fakesrv", id, "") == nil {
		t.Fatal("server did not start")
	}
	t.Cleanup(func() { e.Forget(id) })
	ep, _ := e.endpointFor(id)
	if err := os.Remove(ep.socket); err != nil {
		t.Fatal(err)
	}
	other, err := net.Listen("unix", ep.socket)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	key := fileIdentity(ep.socket)
	next := New(e.cfg)
	if _, ok := next.endpointFor(id); ok {
		t.Fatal("recovered a different socket at the old path")
	}
	next.Forget(id)
	if fileIdentity(ep.socket) != key {
		t.Fatal("removed an unowned replacement socket")
	}
}

func TestRecoveryRejectsNonprivateOrInvalidRecords(t *testing.T) {
	for _, kind := range []string{"permissions", "corrupt", "symlink", "oversize", "adapter"} {
		t.Run(kind, func(t *testing.T) {
			const id = "s_abcdefghij"
			e := fakeServerEngine(t, "serve")
			if e.EndpointArgs("fakesrv", id, "") == nil {
				t.Fatal("server did not start")
			}
			t.Cleanup(func() { e.Forget(id) })
			path := e.recoveryPath(id)
			switch kind {
			case "permissions":
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				if err := os.WriteFile(path, make([]byte, (16<<10)+1), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "metadata")
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "adapter":
				record, err := e.readEndpointRecord(id)
				if err != nil {
					t.Fatal(err)
				}
				record.Adapter = "missing"
				if err := e.writeEndpointRecord(record); err != nil {
					t.Fatal(err)
				}
			}
			next := New(e.cfg)
			if _, ok := next.endpointFor(id); ok {
				t.Fatal("accepted an invalid endpoint record")
			}
		})
	}
}

func TestRecoveredEndpointRelaunchReplacesOnlyItsServer(t *testing.T) {
	const id = "s_abcdefghij"
	e := fakeServerEngine(t, "serve")
	if e.EndpointArgs("fakesrv", id, "") == nil {
		t.Fatal("server did not start")
	}
	before, err := e.readEndpointRecord(id)
	if err != nil {
		t.Fatal(err)
	}
	next := New(e.cfg)
	t.Cleanup(func() { next.Forget(id) })
	if _, ok := next.endpointFor(id); !ok {
		t.Fatal("did not recover")
	}
	if next.EndpointArgs("fakesrv", id, "") == nil {
		t.Fatal("did not relaunch")
	}
	after, err := next.readEndpointRecord(id)
	if err != nil {
		t.Fatal(err)
	}
	if before.Generation == after.Generation || before.Server.alive() || !after.Server.alive() {
		t.Fatal("relaunch kept old ownership or lost the replacement server")
	}
	e.Forget(id)
	if _, err := next.readEndpointRecord(id); err != nil || !after.Server.alive() || fileIdentity(after.Socket) != after.SocketKey {
		t.Fatal("old helper removed its successor's instance")
	}
}

func TestEndpointOperationsReleaseIdleLocks(t *testing.T) {
	var ops endpointOperations
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { unlock := ops.lock("same-session"); unlock() })
	}
	wg.Wait()
	if len(ops.byID) != 0 {
		t.Fatal("idle ownership locks accumulated")
	}
}

func TestRecoveryKeepsRelaunchedConversationArguments(t *testing.T) {
	const id = "s_abcdefghij"
	e := fakeServerEngine(t, "serve")
	argv := []string{"fakesrv", "--resume", "another-conversation"}
	e.noteRelaunch(id, argv)
	if e.EndpointArgs("fakesrv", id, "") == nil {
		t.Fatal("server did not start")
	}
	t.Cleanup(func() { e.Forget(id) })
	next := New(e.cfg)
	env := next.envFor(session.Session{ID: id, Agent: "fakesrv", Managed: true, Command: []string{"fakesrv"}})
	if !slices.Equal(env.Argv, argv) {
		t.Fatal("replacement helper followed the original conversation arguments")
	}
}

func TestEndpointPersistenceFailureLeavesNoServer(t *testing.T) {
	const id = "s_abcdefghij"
	e := fakeServerEngine(t, "serve")
	path := e.recoveryPath(id)
	// A failed atomic rename after server startup must stop that server.
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "block-rename"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if e.EndpointArgs("fakesrv", id, "") != nil {
		t.Fatal("published an unrecoverable endpoint")
	}
	if fileIdentity(filepath.Join(filepath.Dir(path), id+".sock")) != "" {
		t.Fatal("failed launch left its socket")
	}
	e.srv.mu.Lock()
	left := len(e.srv.byID)
	e.srv.mu.Unlock()
	if left != 0 {
		t.Fatal("failed launch left a server owner")
	}
}

func TestRecoveryFollowsTheServerOwnedSocketSymlink(t *testing.T) {
	const id = "s_abcdefghij"
	e := fakeServerEngine(t, "serve")
	if e.EndpointArgs("fakesrv", id, "") == nil {
		t.Fatal("server did not start")
	}
	t.Cleanup(func() { e.Forget(id) })
	record, err := e.readEndpointRecord(id)
	if err != nil {
		t.Fatal(err)
	}
	target := record.Socket + ".target"
	if err := os.Rename(record.Socket, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, record.Socket); err != nil {
		t.Fatal(err)
	}
	record.SocketKey = fileIdentity(record.Socket)
	if err := e.writeEndpointRecord(record); err != nil {
		t.Fatal(err)
	}
	next := New(e.cfg)
	if _, ok := next.endpointFor(id); !ok {
		t.Fatal("lost a live endpoint whose owned path is a socket symlink")
	}
	next.Forget(id)
	if _, err := os.Lstat(record.Socket); !os.IsNotExist(err) {
		t.Fatal("kept owned socket symlink")
	}
}

type recoverySessions func(context.Context, string) (session.Session, error)

func (f recoverySessions) Resolve(ctx context.Context, id string) (session.Session, error) {
	return f(ctx, id)
}

func TestForgetDuringRecoveryClearsRelaunchArguments(t *testing.T) {
	const id = "s_abcdefghij"
	e := fakeServerEngine(t, "serve")
	e.noteRelaunch(id, []string{"fakesrv", "--resume", "conversation"})
	if e.EndpointArgs("fakesrv", id, "") == nil {
		t.Fatal("server did not start")
	}
	t.Cleanup(func() { e.Forget(id) })
	entered, release := make(chan struct{}), make(chan struct{})
	cfg := e.cfg
	cfg.Sessions = recoverySessions(func(context.Context, string) (session.Session, error) {
		close(entered)
		<-release
		return session.Session{ID: id, Agent: "fakesrv", Managed: true}, nil
	})
	next := New(cfg)
	restored, forgotten := make(chan struct{}), make(chan struct{})
	go func() { next.endpointFor(id); close(restored) }()
	<-entered
	go func() { next.Forget(id); close(forgotten) }()
	waitFor(t, func() bool {
		next.endpointOps.mu.Lock()
		defer next.endpointOps.mu.Unlock()
		return next.endpointOps.byID[id].users == 2
	})
	close(release)
	<-restored
	<-forgotten
	if _, ok := next.relaunchedArgv(id); ok {
		t.Fatal("removal retained arguments published by concurrent recovery")
	}
}
