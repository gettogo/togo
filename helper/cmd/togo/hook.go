package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gettogo/togo/helper/internal/logfile"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	// hookStdinWait bounds how long a hook waits for its payload; Claude Code
	// writes it at once and closes stdin.
	hookStdinWait = 3 * time.Second
	// hookCallTimeout bounds a non-blocking report to the daemon.
	hookCallTimeout = 2 * time.Second
	// hookAskSeconds is how long a blocking permission hook waits for an
	// answer: well under Claude Code's 600 s default command hook timeout,
	// so the hook returns before Claude kills it.
	hookAskSeconds = 540
	hookAskSlack   = 10 * time.Second
)

// hookAdapter reduces one agent hook invocation to a HookEvent. text is
// printed to stdout (the status line); errors never quote the payload.
type hookAdapter func(ctx context.Context, event string, payload []byte, env map[string]string) (ev wire.HookEvent, text string, err error)

// payloadAgents are the hookAdapters agents whose raw payload is
// forwarded with the summary, for their harness adapter's hook feed.
var payloadAgents = map[string]bool{"claude": true, "codex": true}

// hookAdapters maps the agent argument of `togo hook` to its adapter. Other
// agents add themselves from their own file's init.
var hookAdapters = map[string]hookAdapter{
	"claude": func(_ context.Context, event string, payload []byte, env map[string]string) (wire.HookEvent, string, error) {
		ev, out, err := claude.Map(event, payload, env)
		return ev, out.Text, err
	},
}

func init() {
	register("hook", func(ctx context.Context, args []string, env map[string]string, stdin io.Reader, stdout, stderr io.Writer) int {
		if then, rest, ok := cutThen(args); ok {
			return runStatuslineChain(ctx, rest, then, env, stdin, stdout, stderr)
		}
		return runHook(ctx, args, env, stdin, stdout)
	})
}

// runHook is what agent hooks run. It never fails the agent: every problem
// exits 0 with nothing on stdout (so the agent keeps its own behaviour) and
// one line in $TOGO_HOME/log/hook.log that names the event, never its
// payload.
func runHook(ctx context.Context, args []string, env map[string]string, stdin io.Reader, stdout io.Writer) int {
	block := false
	var pos []string
	for _, a := range args {
		if a == "--block" {
			block = true
		} else {
			pos = append(pos, a)
		}
	}
	log := hookLogger(env)
	if len(pos) != 2 {
		log("hook: expected <agent> <event>")
		return exitOK
	}
	agent, event := pos[0], pos[1]
	adapt, ok := hookAdapters[agent]
	fwd, forwards := forwardAdapters[agent]
	if !ok && !forwards {
		log("hook: unknown agent")
		return exitOK
	}
	if owner := env["TOGO_AGENT"]; local.Foreign(owner, agent) {
		// Another harness ran this agent's hook file (Copilot runs
		// .claude/settings*.json hooks). Print nothing, so that harness
		// keeps its own behaviour, and report nothing.
		log("hook %s %s: session runs %s, ignored", agent, clipLog(event), clipLog(owner))
		return exitOK
	}
	payload, err := readBounded(stdin, claude.MaxPayload, hookStdinWait)
	if err != nil {
		log("hook %s %s: %v", agent, clipLog(event), err)
		return exitOK
	}
	var (
		ev       wire.HookEvent
		text     string
		params   any
		blocks   = agent == "claude" && event == "PermissionRequest"
		decision = claude.Decision
	)
	if forwards {
		var f wire.HookForward
		f, err = fwd.reduce(event, payload, env)
		ev, params = f.HookEvent, f
		blocks, decision = fwd.blocks(event), fwd.decision
	} else {
		ev, text, err = adapt(ctx, event, payload, env)
		params = ev
		if payloadAgents[agent] {
			// The harness feeds read the agent's own payload, redacted
			// here; the summary fields still drive status and push.
			params = claude.Forward(ev, event, payload)
		}
	}
	if err != nil {
		log("hook %s %s: %v", agent, clipLog(event), err)
		return exitOK
	}
	if text != "" {
		fmt.Fprintln(stdout, text)
	}
	sock, err := tapSocketPath(env)
	if err != nil {
		log("hook %s %s: %v", agent, event, err)
		return exitOK
	}
	if agent == "claude" && event == "PermissionRequest" {
		if questions, prompts, ok := claudeAsk(payload); ok {
			// Questions always wait for the phone, with or without --block:
			// answering them never grants a permission.
			if ev.SessionID != nil {
				claudeQuestion(ctx, sock, ev, questions, prompts, stdout, log)
			}
			return exitOK
		}
	}
	if blocks && (block || env["TOGO_HOOK_BLOCK"] == "1") {
		hookBlock(ctx, sock, agent, ev, decision, stdout, log)
		return exitOK
	}
	if ev.SessionID == nil {
		// Outside a managed session there is nothing to report.
		return exitOK
	}
	callCtx, cancel := context.WithTimeout(ctx, hookCallTimeout)
	defer cancel()
	if err := localCall(callCtx, sock, wire.MethodLocalHook, params, nil); err != nil {
		log("hook %s %s: %v", agent, event, err)
	}
	return exitOK
}

// hookBlock asks the user through the daemon and prints the agent's
// permission decision. Timeout, an unknown answer or a missing daemon print
// nothing, so the agent shows its own prompt.
func hookBlock(ctx context.Context, sock, agent string, ev wire.HookEvent, decision func(string) []byte, stdout io.Writer, log func(string, ...any)) {
	ctx, cancel := context.WithTimeout(ctx, hookAskSeconds*time.Second+hookAskSlack)
	defer cancel()
	var res wire.AskResult
	if err := localCall(ctx, sock, wire.MethodLocalAsk, claude.AskFor(ev, hookAskSeconds), &res); err != nil {
		log("hook %s PermissionRequest: %v", agent, err)
		return
	}
	if res.Decision == nil {
		log("hook %s PermissionRequest: no answer before timeout", agent)
		return
	}
	if out := decision(*res.Decision); out != nil {
		fmt.Fprintln(stdout, string(out))
	}
}

var errStdinTimeout = errors.New("payload not received in time")

// readBounded reads at most limit bytes, giving up after wait so a writer
// that never closes the pipe cannot hold the hook.
func readBounded(r io.Reader, limit int64, wait time.Duration) ([]byte, error) {
	type result struct {
		b   []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		b, err := io.ReadAll(io.LimitReader(r, limit+1))
		done <- result{b, err}
	}()
	select {
	case res := <-done:
		if res.err != nil {
			return nil, errors.New("read payload failed")
		}
		if int64(len(res.b)) > limit {
			return nil, errors.New("payload too large")
		}
		return res.b, nil
	case <-time.After(wait):
		return nil, errStdinTimeout
	}
}

// hookLogger appends to $TOGO_HOME/log/hook.log. Callers pass only event
// names and error categories: payload text is never logged.
func hookLogger(env map[string]string) func(format string, args ...any) {
	return func(format string, args ...any) {
		sock, err := tapSocketPath(env)
		if err != nil {
			return
		}
		dir := filepath.Join(filepath.Dir(filepath.Dir(sock)), "log")
		if os.MkdirAll(dir, 0o700) != nil {
			return
		}
		f, err := logfile.Open(filepath.Join(dir, "hook.log"), logfile.MaxSize)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
	}
}

// clipLog keeps an untrusted argument short and printable in the log.
func clipLog(s string) string {
	out := make([]rune, 0, 40)
	for _, r := range s {
		if len(out) == 40 {
			break
		}
		if r >= 0x20 && r != 0x7f {
			out = append(out, r)
		}
	}
	return string(out)
}

// daemonError is a protocol error answered by the daemon. Only the code is
// kept: error data may echo request content.
type daemonError struct{ Code int }

func (e *daemonError) Error() string { return fmt.Sprintf("daemon error %d", e.Code) }

// errUnreachable means no daemon answered on the socket.
var errUnreachable = errors.New("togo daemon is not reachable")

// localCall makes one call on the daemon socket and decodes the result into
// out (which may be nil). The call is bounded by ctx. A failure to connect
// or to complete hello is errUnreachable; a protocol error is *daemonError.
func localCall(ctx context.Context, sock, method string, params, out any) error {
	conn, err := sockd.Dial(ctx, sock)
	if err != nil {
		return errUnreachable
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	hello := `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":1}}`
	if rpc.WriteFrame(conn, []byte(hello)) != nil {
		return errUnreachable
	}
	if _, err := rpc.ReadFrame(conn); err != nil {
		return errUnreachable
	}
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": method, "params": params})
	if err != nil {
		return err
	}
	if err := rpc.WriteFrame(conn, req); err != nil {
		return fmt.Errorf("send %s: %w", method, err)
	}
	for {
		frame, err := rpc.ReadFrame(conn)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read %s reply: %w", method, err)
		}
		var resp struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *rpc.Error      `json:"error"`
		}
		if json.Unmarshal(frame, &resp) != nil || string(resp.ID) != "2" {
			continue // a notification, or nothing we asked for
		}
		if resp.Error != nil {
			return &daemonError{Code: resp.Error.Code}
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(resp.Result, out)
	}
}
