package ports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

func TestDescendantsFromPsOutput(t *testing.T) {
	const ps = `    1     0
  100     1
  200   100
  201   100
  300   200
  400     1
bad line
  500   300
  300   500
`
	tests := []struct {
		name  string
		roots []int
		want  []int
	}{
		{"root alone", []int{400}, []int{400}},
		{"whole subtree breadth first", []int{100}, []int{100, 200, 201, 300, 500}},
		{"several roots", []int{201, 400}, []int{201, 400}},
		{"unknown root is kept", []int{999}, []int{999}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := descendants(parsePS(ps), tt.roots); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("descendants(%v) = %v, want %v", tt.roots, got, tt.want)
			}
		})
	}
}

func TestParseLsofF(t *testing.T) {
	const out = "p4242\ncnode\nn127.0.0.1:3000\nn*:8080\np77\ncpython3\nn[::1]:5173\nnbogus\n"
	want := []Listener{
		{Port: 3000, Address: "127.0.0.1", PID: 4242, Command: "node"},
		{Port: 8080, Address: "*", PID: 4242, Command: "node"},
		{Port: 5173, Address: "::1", PID: 77, Command: "python3"},
	}
	if got := parseLsofF(out); !reflect.DeepEqual(got, want) {
		t.Errorf("parseLsofF = %+v, want %+v", got, want)
	}
}

func TestParseProcNetTCP(t *testing.T) {
	const v4 = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 11111 1 0000000000000000 100 0 0 10 0
   1: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 22222 1 0000000000000000 100 0 0 10 0
   2: 0100007F:C350 0100007F:0BB8 01 00000000:00000000 00:00000000 00000000  1000        0 33333 1 0000000000000000 100 0 0 10 0
`
	const v6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:1435 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 44444 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000000000000:0050 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 55555 1 0000000000000000 100 0 0 10 0
`
	if got, want := parseProcNetTCP(v4), []procSocket{{"127.0.0.1", 3000, 11111}, {"0.0.0.0", 8080, 22222}}; !reflect.DeepEqual(got, want) {
		t.Errorf("tcp = %+v, want %+v", got, want)
	}
	if got, want := parseProcNetTCP(v6), []procSocket{{"::1", 5173, 44444}, {"::", 80, 55555}}; !reflect.DeepEqual(got, want) {
		t.Errorf("tcp6 = %+v, want %+v", got, want)
	}
}

func TestNormalizeSortsByPortAndDropsDuplicates(t *testing.T) {
	in := []Listener{
		{Port: 8080, Address: "*", PID: 2, Command: "b"},
		{Port: 3000, Address: "127.0.0.1", PID: 1, Command: "a"},
		{Port: 8080, Address: "*", PID: 2, Command: "b"},
	}
	want := []Listener{in[1], in[0]}
	if got := normalize(in); !reflect.DeepEqual(got, want) {
		t.Errorf("normalize = %+v, want %+v", got, want)
	}
	if got := normalize(nil); got == nil || len(got) != 0 {
		t.Errorf("normalize(nil) = %#v, want empty non-nil slice", got)
	}
}

// TestMain lets the test binary double as the listener process that the
// session test starts in a tmux pane.
func TestMain(m *testing.M) {
	if out := os.Getenv("PORTS_TEST_STUB_OUT"); out != "" {
		runStub(out)
		return
	}
	os.Exit(m.Run())
}

func runStub(out string) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(1)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(out+".tmp", []byte(strconv.Itoa(port)), 0o600); err != nil {
		os.Exit(1)
	}
	if err := os.Rename(out+".tmp", out); err != nil {
		os.Exit(1)
	}
	time.Sleep(time.Minute)
}

type fakeResolver struct{ sess session.Session }

func (f fakeResolver) Resolve(_ context.Context, id string) (session.Session, error) {
	if id != f.sess.ID {
		return session.Session{}, session.ErrNotFound
	}
	return f.sess, nil
}

func TestListFindsListenerInSession(t *testing.T) {
	c := tmuxtest.New(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "port")
	const id = "s_portstest1"
	err = c.NewSession(context.Background(), tmux.NewSessionOpts{
		Name:    id,
		Dir:     t.TempDir(),
		Command: []string{"/bin/sh", "-c", fmt.Sprintf("PORTS_TEST_STUB_OUT=%s %s; sleep 60", out, exe)},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	port := waitForPort(t, out)
	svc := &Service{Sessions: fakeResolver{session.Session{ID: id, Managed: true}}, Panes: c}

	got, err := svc.List(context.Background(), id)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var hit *Listener
	for i := range got {
		if got[i].Port == port {
			hit = &got[i]
		}
	}
	if hit == nil {
		t.Fatalf("port %d not in %+v", port, got)
	}
	if hit.Address != "127.0.0.1" || hit.PID < 1 || hit.Command == "" {
		t.Errorf("listener = %+v, want loopback address, a pid and a command", *hit)
	}

	res, err := svc.Methods()["ports.list"](context.Background(), json.RawMessage(`{"id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("ports.list: %v", err)
	}
	raw, _ := json.Marshal(res)
	if !strings.Contains(string(raw), fmt.Sprintf(`"port":%d`, port)) {
		t.Errorf("ports.list result %s lacks port %d", raw, port)
	}
}

func TestListUnknownAndUnmanagedSessions(t *testing.T) {
	svc := &Service{Sessions: fakeResolver{session.Session{ID: "u_other", Managed: false}}}
	if _, err := svc.List(context.Background(), "s_missing"); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("unknown session error = %v, want ErrNotFound", err)
	}
	if _, err := svc.List(context.Background(), "u_other"); !errors.Is(err, session.ErrNotManaged) {
		t.Errorf("unmanaged session error = %v, want ErrNotManaged", err)
	}
}

func waitForPort(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			if p, err := strconv.Atoi(string(b)); err == nil {
				return p
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("listener stub did not report a port")
	return 0
}

type stalledPanes struct{}

func (stalledPanes) PanePIDs(ctx context.Context, _ string) ([]int, error) {
	<-ctx.Done()
	return nil, fmt.Errorf("tmux: %w", ctx.Err())
}

func TestSlowLookupReportsTimeout(t *testing.T) {
	saved := lookupTimeout
	lookupTimeout = 50 * time.Millisecond
	t.Cleanup(func() { lookupTimeout = saved })
	s := &Service{Sessions: fakeResolver{session.Session{ID: "s_abcdefghij", Managed: true}}, Panes: stalledPanes{}}
	_, err := s.List(context.Background(), "s_abcdefghij")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}
