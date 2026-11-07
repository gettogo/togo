package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/wire"
)

// optionArgv holds the JSON array of Launch.Argv for a session that runs
// an agent. The plain shell has none.
const optionArgv = "@togo_argv"

// The wrapper scripts run the agent and then report its exit status to the
// daemon. They are constant: the helper path and the agent argv reach them
// only as positional parameters, so no input is ever read as shell text.
// Both wrappers put $TOGO_SHIM first on PATH after the login shell set
// PATH up, so the agent's open and xdg-open are the shims that send web
// pages to the phone.
// In posixWrapper $0 is the literal "togo", $1 the helper path (empty to
// skip the report) and the rest the agent argv; fishWrapper gets the same
// list without $0. Both end in a login shell so the pane stays usable.
const (
	posixWrapper = `t=$1; shift; [ -n "$TOGO_SHIM" ] && PATH="$TOGO_SHIM:$PATH"; "$@"; s=$?; [ -n "$t" ] && "$t" session-exit "$s" 2>/dev/null; exec "${SHELL:-/bin/sh}" -l`
	fishWrapper  = `set -l t $argv[1]; test -n "$TOGO_SHIM"; and set -gx PATH $TOGO_SHIM $PATH; command $argv[2..-1]; set -l s $status; test -n "$t"; and $t session-exit $s 2>/dev/null; exec $SHELL -l`
)

// posixShells take -lic and treat the first argument after the script as
// $0. Login shells not listed (tcsh, nu, ...) cannot run posixWrapper, so
// the agent starts under /bin/sh instead.
var posixShells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "ksh": true, "mksh": true,
	"dash": true, "ash": true, "yash": true,
}

var programName = regexp.MustCompile(wire.ProgramNamePattern)

// Launch is the argv of a session's agent before the wrapper: the program
// then its arguments. The zero Launch is the plain shell.
type Launch struct {
	Program string
	Args    []string
	// Env is extra environment for this run only (an endpoint's server
	// password); it is never stored with the session.
	Env map[string]string
}

// Argv returns the program followed by its arguments, or nil for the plain
// shell.
func (l Launch) Argv() []string {
	if l.Program == "" {
		return nil
	}
	return append([]string{l.Program}, l.Args...)
}

// ValidateLaunch checks a session.create agent, program and args against
// the protocol limits and finds the program on loginPath, then in fallbacks
// (see agents.FallbackDirs). A program found only in fallbacks is launched by
// its absolute path, since the login shell would not find it by name. It returns the
// agent the session runs (a made-up entry for wire.AgentCustom, named after
// the program) and its Launch. Rejections are *InvalidParamsError with
// Reason wire.ReasonArgs, wire.ReasonProgram or wire.ReasonNotInstalled;
// an unknown agent id has no Reason.
func ValidateLaunch(agentID, program string, args []string, loginPath string, fallbacks []string) (agents.Agent, Launch, error) {
	if agentID == "" {
		agentID = "shell"
	}
	var a agents.Agent
	if agentID == wire.AgentCustom {
		if program == "" {
			return fail(wire.ReasonProgram, "agent custom needs a program")
		}
	} else {
		var ok bool
		if a, ok = agents.Lookup(agentID); !ok {
			return agents.Agent{}, Launch{}, &InvalidParamsError{Msg: fmt.Sprintf("unknown agent %q", agentID)}
		}
		if program != "" {
			return fail(wire.ReasonProgram, "program is only allowed with agent custom")
		}
		if a.Command == "" && len(args) > 0 {
			return fail(wire.ReasonArgs, "the shell takes no arguments")
		}
	}
	if err := validateArgs(args); err != nil {
		return agents.Agent{}, Launch{}, err
	}
	if agentID == wire.AgentCustom {
		if _, err := findProgram(program, loginPath, fallbacks, true); err != nil {
			return agents.Agent{}, Launch{}, err
		}
		a = agents.Agent{ID: wire.AgentCustom, Name: programDisplayName(program), Command: program}
	} else if a.Command != "" {
		p, off, ok := agents.Find(a.Command, loginPath, fallbacks)
		if !ok {
			return fail(wire.ReasonNotInstalled, fmt.Sprintf("agent %q is not installed", a.ID))
		}
		if off {
			return a, Launch{Program: p, Args: slices.Clone(args)}, nil
		}
	}
	if a.Command == "" {
		return a, Launch{}, nil
	}
	return a, Launch{Program: a.Command, Args: slices.Clone(args)}, nil
}

func fail(reason, msg string) (agents.Agent, Launch, error) {
	return agents.Agent{}, Launch{}, &InvalidParamsError{Msg: msg, Reason: reason}
}

// validateArgs enforces the args limits: at most wire.MaxArgs entries, each
// 1 to wire.MaxArgBytes bytes of UTF-8 without control characters, and
// wire.MaxArgsBytes in total.
func validateArgs(args []string) error {
	if len(args) > wire.MaxArgs {
		return &InvalidParamsError{Reason: wire.ReasonArgs, Msg: fmt.Sprintf("%d arguments, the limit is %d", len(args), wire.MaxArgs)}
	}
	total := 0
	for i, arg := range args {
		switch {
		case arg == "" || len(arg) > wire.MaxArgBytes:
			return &InvalidParamsError{Reason: wire.ReasonArgs, Msg: fmt.Sprintf("argument %d must be 1-%d bytes", i+1, wire.MaxArgBytes)}
		case !utf8.ValidString(arg):
			return &InvalidParamsError{Reason: wire.ReasonArgs, Msg: fmt.Sprintf("argument %d is not valid UTF-8", i+1)}
		case strings.ContainsFunc(arg, unicode.IsControl):
			return &InvalidParamsError{Reason: wire.ReasonArgs, Msg: fmt.Sprintf("argument %d contains a control character", i+1)}
		}
		total += len(arg)
	}
	if total > wire.MaxArgsBytes {
		return &InvalidParamsError{Reason: wire.ReasonArgs, Msg: fmt.Sprintf("arguments are %d bytes, the limit is %d", total, wire.MaxArgsBytes)}
	}
	return nil
}

// findProgram resolves program, a bare name looked up on loginPath or a
// clean absolute path, to the file that would run. A program that cannot
// be named that way, or an absolute path to something other than a regular
// executable file, is wire.ReasonProgram. A program that is simply absent
// is wire.ReasonNotInstalled when missingIsError, else "" and nil.
func findProgram(program, loginPath string, fallbacks []string, missingIsError bool) (string, error) {
	notInstalled := func() (string, error) {
		if !missingIsError {
			return "", nil
		}
		return "", &InvalidParamsError{Reason: wire.ReasonNotInstalled, Msg: "the program is not installed"}
	}
	if programName.MatchString(program) {
		if p, _, ok := agents.Find(program, loginPath, fallbacks); ok {
			return p, nil
		}
		return notInstalled()
	}
	if !validProgramPath(program) {
		return "", &InvalidParamsError{Reason: wire.ReasonProgram, Msg: "program must be a bare name or a clean absolute path"}
	}
	if _, err := os.Stat(program); errors.Is(err, fs.ErrNotExist) {
		return notInstalled()
	}
	if !agents.Executable(program) {
		return "", &InvalidParamsError{Reason: wire.ReasonProgram, Msg: "program is not an executable file"}
	}
	return program, nil
}

func validProgramPath(p string) bool {
	return len(p) <= wire.MaxProgramPath && filepath.IsAbs(p) && filepath.Clean(p) == p &&
		utf8.ValidString(p) && !strings.ContainsFunc(p, unicode.IsControl)
}

// programDisplayName is the default session name for a custom program: its
// base name, trimmed and cut to the name limit.
func programDisplayName(program string) string {
	name := strings.TrimSpace(filepath.Base(program))
	if utf8.RuneCountInString(name) > maxNameRunes {
		name = strings.TrimSpace(string([]rune(name)[:maxNameRunes]))
	}
	if name == "" || name == "/" {
		return wire.AgentCustom
	}
	return name
}

// launchWrapper returns the pane argv that runs l under the user's login
// shell and reports its exit through togoPath, or nil for the plain shell,
// which tmux starts itself.
func launchWrapper(shell, togoPath string, l Launch) []string {
	argv := l.Argv()
	if argv == nil {
		return nil
	}
	switch base := filepath.Base(shell); {
	case posixShells[base]:
		return append([]string{shell, "-lic", posixWrapper, "togo", togoPath}, argv...)
	case base == "fish":
		return append([]string{shell, "-lic", fishWrapper, togoPath}, argv...)
	default:
		return append([]string{"/bin/sh", "-lc", posixWrapper, "togo", togoPath}, argv...)
	}
}

// encodeArgv is the optionArgv value for argv. JSON escapes every control
// character, so the value never holds tmux's list separators.
func encodeArgv(argv []string) (string, error) {
	b, err := json.Marshal(argv)
	return string(b), err
}

// withCommand sets sess.Command from its optionArgv value. Anyone who can
// run tmux on the socket can rewrite the option, so the value is checked
// against the limits Create enforces; one that fails is logged, without
// its content, and the session is listed with no command.
func (s *Service) withCommand(sess Session, raw string) Session {
	argv, err := decodeArgv(raw)
	if err != nil && s.Logf != nil {
		s.Logf("session %s: ignoring %s: %v", sess.ID, optionArgv, err)
	}
	sess.Command = argv
	return sess
}

// decodeArgv reads an optionArgv value. Unset is nil; a value Create could
// not have written is an error.
func decodeArgv(v string) ([]string, error) {
	if v == "" {
		return nil, nil
	}
	var argv []string
	if json.Unmarshal([]byte(v), &argv) != nil || len(argv) == 0 {
		return nil, errors.New("not a non-empty JSON array of strings")
	}
	if !programName.MatchString(argv[0]) && !validProgramPath(argv[0]) {
		return nil, errors.New("the program is not a bare name or a clean absolute path")
	}
	if err := validateArgs(argv[1:]); err != nil {
		return nil, err
	}
	return argv, nil
}

// sessionEnv is the environment tmux gives a new session's pane.
// TOGO_AGENT names the agent the session was started for, so a hook can
// tell when it fires inside another agent: Copilot CLI also runs the hooks
// in .claude/settings*.json.
func (s *Service) sessionEnv(id, bib, agent string) map[string]string {
	env := map[string]string{"TOGO_SESSION": id, "TOGO_BIB": bib}
	if agent != "" {
		env["TOGO_AGENT"] = agent
	}
	for k, v := range agentEnv[agent] {
		env[k] = v
	}
	if s.ShimDir != "" {
		env["TOGO_SHIM"] = s.ShimDir
		env["BROWSER"] = filepath.Join(s.ShimDir, "togo-browser")
	}
	return env
}

// agentEnv is extra environment some agents need for the live harness
// view. Crush's TUI joins its client-server mode, which a second client
// can follow, only with CRUSH_CLIENT_SERVER=1. Fixed here, never read from
// adapter files.
var agentEnv = map[string]map[string]string{
	"crush": {"CRUSH_CLIENT_SERVER": "1"},
}

// EndpointArgs, when set, returns arguments a harness adapter adds to an
// agent's argv for one run, such as the port or socket the helper picked
// for the live harness API. They are not stored with the session's argv:
// every start asks again. The daemon sets it once at start.
var EndpointArgs func(agentID, sessionID string) []string

// EndpointEnv, when set, returns the environment that goes with the
// endpoint arguments EndpointArgs just picked. The daemon sets it once at
// start.
var EndpointEnv func(sessionID string) map[string]string

// WithEndpoint returns l with the endpoint arguments for this run
// appended. The plain shell gets none.
func (l Launch) WithEndpoint(agentID, sessionID string) Launch {
	if l.Program == "" || EndpointArgs == nil {
		return l
	}
	extra := EndpointArgs(agentID, sessionID)
	if len(extra) == 0 {
		return l
	}
	if validateArgs(append(slices.Clone(l.Args), extra...)) != nil {
		return l
	}
	l.Args = append(slices.Clone(l.Args), extra...)
	if EndpointEnv != nil {
		l.Env = EndpointEnv(sessionID)
	}
	return l
}
