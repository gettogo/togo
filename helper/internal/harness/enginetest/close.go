package enginetest

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// What a rig leaves behind when its test ends decides whether the test's
// folders can be removed: a feed still polling, a sqlite3 or fake agent
// process still holding a file in them, or a goroutine still writing there
// makes t.TempDir's removal fail, or leaves a folder behind. Stop and the
// test's cleanup therefore wait for those to end before they return.

// drainLimit bounds how long a rig waits for what it started to end.
const drainLimit = 5 * time.Second

var (
	// openEngines counts the engines running now, over all rigs.
	openEngines atomic.Int32
	// baseline is how many engine goroutines there were when the first
	// engine started: what other code in the test binary keeps running.
	baseMu   sync.Mutex
	baseline int
)

// Goroutines lists the stacks of the goroutines running the engine, its
// feeds, the approvals or any code of the harness packages. The test
// functions' own goroutines are not listed.
func Goroutines() []string {
	var out []string
	for _, g := range stacks() {
		switch {
		case !strings.Contains(g, "/helper/internal/"):
		// The agent's server outlives the engine, as the agent does.
		case strings.Contains(g, "enginetest.(*APIServer)"), strings.Contains(g, "enginetest.(*CodexServer)"):
		case strings.Contains(g, "testing.tRunner"), strings.Contains(g, "testing.(*M)"),
			strings.Contains(g, "testing.runTests"), strings.Contains(g, "enginetest.Goroutines"):
		default:
			out = append(out, g)
		}
	}
	return out
}

// stacks is the stack of every goroutine, one string each.
func stacks() []string {
	var buf []byte
	for n := 1 << 16; ; n *= 2 {
		buf = make([]byte, n)
		if got := runtime.Stack(buf, true); got < n {
			buf = buf[:got]
			break
		}
	}
	return strings.Split(string(buf), "\n\n")
}

// Children lists the processes that descend from this test process, as
// "pid command".
func Children() []string {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil
	}
	parent := map[int]int{}
	cmd := map[int]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		if e1 != nil || e2 != nil {
			continue
		}
		parent[pid] = ppid
		cmd[pid] = strings.Join(f[2:], " ")
	}
	self := os.Getpid()
	var res []string
	for pid := range parent {
		for p, n := pid, 0; p > 1 && n < 64; p, n = parent[p], n+1 {
			if parent[p] == self {
				if cmd[pid] != "ps -axo pid=,ppid=,command=" {
					res = append(res, strconv.Itoa(pid)+" "+cmd[pid])
				}
				break
			}
		}
	}
	return res
}

// ownChildren lists the descendants of this process whose command line
// names one of dirs.
func ownChildren(dirs []string) []int {
	var pids []int
	for _, c := range Children() {
		for _, d := range dirs {
			if d != "" && strings.Contains(c, d) {
				if pid, err := strconv.Atoi(strings.Fields(c)[0]); err == nil {
					pids = append(pids, pid)
				}
				break
			}
		}
	}
	return pids
}

// engineStarted records that an engine is running.
func engineStarted() {
	baseMu.Lock()
	defer baseMu.Unlock()
	if openEngines.Add(1) == 1 {
		baseline = len(Goroutines())
	}
}

// engineStopped is called once an engine has been told to stop. It waits
// for the processes the rig started that name its folders to end (killing
// the ones that outlast a second), and, when it is the last engine running
// in the test binary, for the engine's goroutines to end.
func (r *Rig) engineStopped(dirs []string) {
	deadline := time.Now().Add(drainLimit)
	for time.Now().Before(deadline) {
		pids := ownChildren(dirs)
		if len(pids) == 0 {
			break
		}
		if time.Until(deadline) < drainLimit-time.Second {
			for _, p := range pids {
				_ = syscall.Kill(p, syscall.SIGKILL)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	baseMu.Lock()
	last := openEngines.Add(-1) == 0
	base := baseline
	baseMu.Unlock()
	if !last {
		return
	}
	for time.Now().Before(deadline) {
		if len(Goroutines()) <= base {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if g := Goroutines(); len(g) > base {
		r.T.Logf("%d engine goroutines still running after the rig stopped (%d at its start):\n%s", len(g), base, strings.Join(g, "\n\n"))
	}
}
