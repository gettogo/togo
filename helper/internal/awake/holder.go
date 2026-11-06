package awake

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Holder keeps a sleep block in place between Hold and Release.
type Holder interface {
	// Hold starts the block; holding twice is a no-op.
	Hold() error
	// Release ends it; releasing twice is a no-op.
	Release() error
	// Err reports why a held block ended on its own, or nil.
	Err() error
}

// Process is a started child.
type Process interface {
	Wait() error
	Kill() error
}

// Child is a Holder that runs a process for as long as the block is held.
// The commands chosen by Caffeinate and Inhibitor also watch the daemon's
// pid, so a crashed daemon cannot leave them holding.
type Child struct {
	Argv []string
	// Start runs argv. Nil runs it with os/exec.
	Start func(argv []string) (Process, error)

	mu      sync.Mutex
	proc    Process
	exitErr error
}

// Caffeinate holds PreventSystemSleep (-s) and PreventUserIdleSystemSleep
// (-i) until released or until pid exits.
func Caffeinate(pid int) *Child {
	return &Child{Argv: []string{"/usr/bin/caffeinate", "-s", "-i", "-w", strconv.Itoa(pid)}}
}

// Inhibitor holds a logind sleep, idle and lid-switch block until released
// or until pid exits. inhibit is systemd-inhibit and tail is a GNU tail,
// whose --pid makes it exit with the daemon.
func Inhibitor(pid int, inhibit, tail string) *Child {
	return &Child{Argv: []string{
		inhibit, "--what=sleep:idle:handle-lid-switch", "--mode=block",
		"--who=Togo", "--why=Togo sessions are working",
		tail, "--pid=" + strconv.Itoa(pid), "-f", "/dev/null",
	}}
}

// Hold implements Holder.
func (c *Child) Hold() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc != nil {
		return nil
	}
	start := c.Start
	if start == nil {
		start = execStart
	}
	p, err := start(c.Argv)
	if err != nil {
		return err
	}
	c.proc, c.exitErr = p, nil
	go c.wait(p)
	return nil
}

func (c *Child) wait(p Process) {
	err := p.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc != p {
		return // released
	}
	if err == nil {
		err = errors.New(c.Argv[0] + " exited")
	}
	c.proc, c.exitErr = nil, err
}

// Release implements Holder.
func (c *Child) Release() error {
	c.mu.Lock()
	p := c.proc
	c.proc, c.exitErr = nil, nil
	c.mu.Unlock()
	if p == nil {
		return nil
	}
	return p.Kill()
}

// Err implements Holder.
func (c *Child) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitErr
}

// execProcess reports the child's first line of stderr as its exit error,
// which is where systemd-inhibit says why logind refused.
type execProcess struct {
	cmd    *exec.Cmd
	stderr *limitedBuffer
}

func execStart(argv []string) (Process, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	// Its own process group, so Kill reaches the commands it runs too
	// (the tail under systemd-inhibit).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	buf := &limitedBuffer{max: 1024}
	cmd.Stderr = buf
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execProcess{cmd: cmd, stderr: buf}, nil
}

func (p *execProcess) Wait() error {
	err := p.cmd.Wait()
	if err == nil {
		return nil
	}
	if line, _, _ := strings.Cut(strings.TrimSpace(p.stderr.String()), "\n"); line != "" {
		return errors.New(line)
	}
	return err
}

func (p *execProcess) Kill() error {
	err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	return nil
}

// limitedBuffer keeps the first max bytes written to it.
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.max - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
