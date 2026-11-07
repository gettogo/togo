package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// MethodResume is the Codex app-server call that joins a thread. A feed
// whose method it is follows threads: with a threadId it joins that thread;
// without one it joins every thread the server has loaded (the terminal
// client's thread and its subagents' threads), and keeps looking for new
// ones, so a /new or /resume at the desk is followed too.
const MethodResume = "thread/resume"

const methodLoaded = "thread/loaded/list"

// callRecord is how a call result enters the feed: {"method", "result"}.
func callRecord(method string, res json.RawMessage) Message {
	params, _ := json.Marshal(map[string]json.RawMessage{"result": res})
	return Message{Method: method, Params: params, ID: nil}
}

// methodLimits reads the account's rate limits; methodLimitsUpdated is the
// notification that carries the same snapshot after each turn.
const (
	methodLimits        = "account/rateLimits/read"
	methodLimitsUpdated = "account/rateLimits/updated"
)

// readLimits reads the rate limits once at connect and feeds them as the
// update notification, so the meters show before the first turn ends. A
// logged-out account answers with an error, and the meters wait.
func readLimits(ctx context.Context, c *Conn, push func(Message)) {
	res, err := c.Call(ctx, methodLimits, nil)
	if err != nil || len(res) == 0 || string(res) == "null" {
		return
	}
	push(Message{Method: methodLimitsUpdated, Params: res})
}

// attach runs the feed's call on a fresh connection.
func (r *Runner) attach(ctx context.Context, c *Conn, b spec.RPC, push func(Message)) {
	p := params(b.Params)
	if b.Method != MethodResume {
		res, err := c.Call(ctx, b.Method, p)
		if err == nil {
			push(callRecord(b.Method, res))
		}
		return
	}
	go readLimits(ctx, c, push)
	if id := p["threadId"]; id != "" {
		for ctx.Err() == nil {
			if r.resume(ctx, c, id, push) {
				return
			}
			if !r.sleep(ctx, c, r.Poll) {
				return
			}
		}
		return
	}
	joined := map[string]bool{}
	for {
		for _, id := range loaded(ctx, c) {
			if !joined[id] && r.resume(ctx, c, id, push) {
				joined[id] = true
			}
		}
		if !r.sleep(ctx, c, r.Poll) {
			return
		}
	}
}

func (r *Runner) sleep(ctx context.Context, c *Conn, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-c.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (r *Runner) resume(ctx context.Context, c *Conn, id string, push func(Message)) bool {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := c.Call(cctx, MethodResume, map[string]string{"threadId": id})
	if err != nil {
		// A thread the terminal has started but not yet written ("no
		// rollout found") is retried on the next poll.
		return false
	}
	push(callRecord(MethodResume, res))
	return true
}

func loaded(ctx context.Context, c *Conn) []string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := c.Call(cctx, methodLoaded, map[string]any{})
	if err != nil {
		return nil
	}
	var out struct {
		Data []string `json:"data"`
	}
	if json.Unmarshal(res, &out) != nil {
		return nil
	}
	return out.Data
}

// runChild starts the source's child program (no shell), calls the method
// once on its stdio and stops it.
func runChild(ctx context.Context, b spec.RPC, env harness.Env) (json.RawMessage, error) {
	if len(b.Child) == 0 || b.Child[0] == "" {
		return nil, errors.New("rpc: child has no program")
	}
	prog := b.Child[0]
	if !strings.Contains(prog, "/") {
		if p, err := lookPath(prog, env.Vars["path"]); err == nil {
			prog = p
		}
	}
	cmd := exec.CommandContext(ctx, prog, b.Child[1:]...)
	cmd.Dir = env.Cwd
	if path := env.Vars["path"]; path != "" {
		cmd.Env = append(cmd.Environ(), "PATH="+path)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	// Its own process group, so a timeout stops what the child started.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stop := func() error {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		_ = cmd.Wait()
		return nil
	}
	if b.Framing == spec.FramingTyped {
		t := Lines(stdout, stdin, stop)
		defer t.Close()
		return CallTyped(ctx, t, b.Method, params(b.Params))
	}
	c := NewConn(Lines(stdout, stdin, stop), Handler{})
	defer c.Close()
	if err := Initialize(ctx, c); err != nil {
		return nil, err
	}
	return c.Call(ctx, b.Method, params(b.Params))
}

// lookPath finds prog in a PATH list without consulting the helper's own
// environment.
func lookPath(prog, path string) (string, error) {
	if path == "" {
		return exec.LookPath(prog)
	}
	for _, dir := range strings.Split(path, ":") {
		if dir == "" {
			continue
		}
		cand := dir + "/" + prog
		if p, err := exec.LookPath(cand); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}
