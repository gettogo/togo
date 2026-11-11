// Package tmux is a thin driver for the tmux command line tool. Every call
// execs the binary directly with an argv slice; nothing is ever run through
// a shell.
package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNotInstalled is returned when the tmux binary cannot be found.
var ErrNotInstalled = errors.New("tmux: not installed")

// ErrNoSession is returned when the targeted session does not exist,
// including killing a session while no server is running.
var ErrNoSession = errors.New("tmux: no such session")

// errNoServer is an internal sentinel meaning no tmux server answered on the
// client's socket. Callers translate it per method.
var errNoServer = errors.New("tmux: no server running")

// Client runs tmux commands against one server socket.
type Client struct {
	// Bin is the tmux executable to run.
	Bin string
	// Socket holds leading arguments such as "-L" and a socket label.
	// A nil Socket targets the default tmux server.
	Socket []string
	// Config, when set, is passed as "-f" on every command so a server this
	// client starts loads it. AttachArgv leaves it out: attaching needs a
	// running server, which has already read its config.
	Config string
}

// NewLabel returns a Client bound to the private socket named label.
func NewLabel(label string) *Client {
	return &Client{Bin: "tmux", Socket: []string{"-L", label}}
}

// SessionInfo describes one tmux session.
type SessionInfo struct {
	Name     string
	Created  time.Time
	Attached int
	Dead     bool
	Path     string
	// Bib, Agent, Title and CreatedTag hold the user options @togo_bib,
	// @togo_agent, @togo_name and @togo_created. They are the empty string
	// when the option is unset.
	Bib, Agent, Title, CreatedTag string
	// Muted is true when the user option @togo_muted is "1".
	Muted bool
	// Argv holds the user option @togo_argv, or "" when unset.
	Argv string
	// Exit holds the user option @togo_exit, or "" when unset.
	Exit string
}

// NewSessionOpts configures NewSession.
type NewSessionOpts struct {
	// Name is the session name.
	Name string
	// Dir is the starting directory; empty leaves tmux's default.
	Dir string
	// Cols and Rows size the session; zero leaves tmux's default.
	Cols, Rows int
	// Command is the argv for the initial pane, passed verbatim after "--".
	Command []string
	// Options are user options set after the session is created.
	Options map[string]string
	// Env is set in the session environment with "-e K=V" (tmux 3.2+), so
	// the initial pane already sees it.
	Env map[string]string
}

var versionNum = regexp.MustCompile(`\d+\.\d+`)

// parseVersion extracts the first major.minor number from tmux version
// output. raw is the trimmed output and is always returned; err is non-nil
// only when no version number is present.
func parseVersion(out string) (major, minor int, raw string, err error) {
	raw = strings.TrimSpace(out)
	m := versionNum.FindString(raw)
	if m == "" {
		return 0, 0, raw, fmt.Errorf("tmux: no version number in output %q", raw)
	}
	parts := strings.SplitN(m, ".", 2)
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, raw, fmt.Errorf("tmux: bad version number %q in output %q", m, raw)
	}
	return major, minor, raw, nil
}

// Version runs "tmux -V" and parses the first major.minor it finds.
func (c *Client) Version(ctx context.Context) (major, minor int, raw string, err error) {
	out, err := c.run(ctx, "-V")
	if err != nil {
		return 0, 0, "", err
	}
	return parseVersion(out)
}

// FieldSep and RecSep delimit fields and records in -F list output. They are
// printable ASCII because tmux 3.4 rewrites control bytes such as 0x1f and
// 0x1e in its output as octal text (\037), whatever the locale or -u, while
// older and newer releases pass them through. Printable text is never
// rewritten, so output parses the same on every tmux version. A record is
// "fields...RecSep" plus the newline tmux appends; newlines inside a value
// stay inside the record. Values holding either token are rejected by
// checkOption.
const (
	fieldSep = "|~F~|"
	recSep   = "|~R~|"
)

const listFormat = `#{session_name}` + fieldSep +
	`#{session_created}` + fieldSep +
	`#{session_attached}` + fieldSep +
	`#{pane_dead}` + fieldSep +
	`#{pane_current_path}` + fieldSep +
	`#{@togo_bib}` + fieldSep +
	`#{@togo_agent}` + fieldSep +
	`#{@togo_name}` + fieldSep +
	`#{@togo_created}` + fieldSep +
	`#{@togo_muted}` + fieldSep +
	`#{@togo_argv}` + fieldSep +
	`#{@togo_exit}` + recSep

// ListSessions returns every session on the client's socket. When no server
// is running it returns nil, nil. Output is split on the recSep record
// delimiter, so newlines inside a session name or option value stay inside
// their field; records without exactly 12 fields are skipped, not an error.
func (c *Client) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	out, err := c.run(ctx, "list-sessions", "-F", listFormat)
	if err != nil {
		if errors.Is(err, errNoServer) {
			return nil, nil
		}
		return nil, err
	}
	return parseListOutput(out)
}

// parseListOutput parses the output of "list-sessions -F listFormat" into
// session records. Output is split on the recSep record delimiter, so
// newlines inside a session name or option value stay inside their field.
// Records without exactly 12 fields are skipped, not an error; a record with
// a non-numeric created or attached field is an error.
func parseListOutput(out string) ([]SessionInfo, error) {
	sessions := []SessionInfo{}
	for _, record := range strings.Split(out, recSep) {
		record = strings.TrimPrefix(record, "\n")
		if record == "" {
			continue
		}
		fields := strings.Split(record, fieldSep)
		if len(fields) != 12 {
			continue
		}
		created, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("tmux: bad session_created %q: %w", fields[1], err)
		}
		attached, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("tmux: bad session_attached %q: %w", fields[2], err)
		}
		sessions = append(sessions, SessionInfo{
			Name:       fields[0],
			Created:    time.Unix(created, 0),
			Attached:   attached,
			Dead:       fields[3] == "1",
			Path:       fields[4],
			Bib:        fields[5],
			Agent:      fields[6],
			Title:      fields[7],
			CreatedTag: fields[8],
			Muted:      fields[9] == "1",
			Argv:       fields[10],
			Exit:       fields[11],
		})
	}
	return sessions, nil
}

// NewSession creates a detached session and applies its options. The
// options are set by set-option commands chained into the same tmux
// invocation, so the server applies them before it can notice the pane
// exiting and before any other client sees the session without them. The
// command argv is passed straight through tmux; it is never joined or
// shell-quoted.
func (c *Client) NewSession(ctx context.Context, o NewSessionOpts) error {
	for k, v := range o.Options {
		if err := checkOption(k, v); err != nil {
			return err
		}
	}
	args := []string{"new-session", "-d", "-s", escapeArg(o.Name)}
	if o.Dir != "" {
		// tmux expands -c as a format, unlike the other arguments here, so
		// "#" must be doubled to reach the filesystem as written.
		args = append(args, "-c", escapeArg(strings.ReplaceAll(o.Dir, "#", "##")))
	}
	if o.Cols > 0 {
		args = append(args, "-x", strconv.Itoa(o.Cols))
	}
	if o.Rows > 0 {
		args = append(args, "-y", strconv.Itoa(o.Rows))
	}
	for _, k := range sortedKeys(o.Env) {
		args = append(args, "-e", escapeArg(k+"="+o.Env[k]))
	}
	if len(o.Command) > 0 {
		args = append(args, "--")
		for _, a := range o.Command {
			args = append(args, escapeArg(a))
		}
	}
	for _, k := range sortedKeys(o.Options) {
		args = append(args, ";", "set-option", "-t", "="+o.Name+":", escapeArg(k), escapeArg(o.Options[k]))
	}
	if _, err := c.run(ctx, args...); err != nil {
		return fmt.Errorf("tmux: new session %s: %w", o.Name, err)
	}
	if o.Dir != "" {
		c.waitPanePath(ctx, o.Name, o.Dir)
	}
	return nil
}

// waitPanePath waits, briefly, for the new pane's pane_current_path to
// settle. tmux reads it from the pane process, and for a moment after
// new-session returns that process has not yet entered its directory, so
// the path reads as "" or as the server's own "/". A caller that lists the
// session at once, such as a client opening a file in it, would see a wrong
// cwd. Until the shell has entered its directory the path can also read as
// the tmux server's own working directory, so the wait is for the requested
// directory itself (symlinks resolved, as tmux reports the real path). It
// gives up quietly after about a second: the path is best effort.
func (c *Client) waitPanePath(ctx context.Context, name, dir string) {
	want := dir
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		want = r
	}
	for range 200 {
		out, err := c.run(ctx, "list-panes", "-t", "="+name+":", "-F", "#{pane_current_path}")
		if err != nil {
			return
		}
		if p := strings.TrimSpace(out); p != "" && samePath(p, want) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// escapeArg protects an argument from tmux's own argv parsing, which treats
// a trailing ";" as a command separator and turns a trailing "\;" into ";".
// Putting a backslash before the final ";" makes tmux keep the value as
// given, whatever precedes it.
func escapeArg(s string) string {
	if strings.HasSuffix(s, ";") {
		return s[:len(s)-1] + `\;`
	}
	return s
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// KillSession kills the session with the exact given name.
func (c *Client) KillSession(ctx context.Context, name string) error {
	_, err := c.run(ctx, "kill-session", "-t", "="+escapeArg(name))
	if errors.Is(err, errNoServer) {
		return ErrNoSession
	}
	return err
}

// SetOption sets a (user) option on the session with the exact given name.
// The target is the exact-match session form "=NAME:"; the trailing colon is
// required because set-option parses its target as a pane, where a bare
// "=NAME" would be read as a pane identifier. Keys and values containing a
// field or record separator token are rejected without
// calling tmux, because they would corrupt list-sessions output.
// When no server is running the error is ErrNoSession, matching
// KillSession.
func (c *Client) SetOption(ctx context.Context, session, key, value string) error {
	if err := checkOption(key, value); err != nil {
		return err
	}
	_, err := c.run(ctx, "set-option", "-t", "="+session+":", escapeArg(key), escapeArg(value))
	if errors.Is(err, errNoServer) {
		return ErrNoSession
	}
	return err
}

// UnsetOption removes a user option from the session with the exact given
// name. When no server is running the error is ErrNoSession.
func (c *Client) UnsetOption(ctx context.Context, session, key string) error {
	if err := checkOption(key, ""); err != nil {
		return err
	}
	_, err := c.run(ctx, "set-option", "-u", "-t", "="+session+":", escapeArg(key))
	if errors.Is(err, errNoServer) {
		return ErrNoSession
	}
	return err
}

// SetEnvironment sets a variable in the environment of the session with the
// exact given name. Panes created afterwards inherit it; running panes do
// not. When no server is running the error is ErrNoSession.
func (c *Client) SetEnvironment(ctx context.Context, session, key, value string) error {
	if err := checkOption(key, value); err != nil {
		return err
	}
	_, err := c.run(ctx, "set-environment", "-t", "="+session, escapeArg(key), escapeArg(value))
	if errors.Is(err, errNoServer) {
		return ErrNoSession
	}
	return err
}

func checkOption(key, value string) error {
	if strings.Contains(key, fieldSep) || strings.Contains(key, recSep) ||
		strings.Contains(value, fieldSep) || strings.Contains(value, recSep) {
		return fmt.Errorf("tmux: option %q: keys and values must not contain field or record separators", key)
	}
	return nil
}

// CaptureHistory returns the plain text of the session's active pane: lines
// rows of history above the visible screen, then the visible rows. join
// rejoins lines the terminal wrapped, so a long command or URL comes back
// as one line. Escape sequences are not included.
func (c *Client) CaptureHistory(ctx context.Context, name string, lines int, join bool) (string, error) {
	args := []string{"capture-pane", "-p"}
	if join {
		args = append(args, "-J")
	}
	args = append(args, "-S", "-"+strconv.Itoa(lines), "-t", "="+name+":")
	out, err := c.run(ctx, args...)
	if errors.Is(err, errNoServer) {
		return "", ErrNoSession
	}
	return out, err
}

// AttachArgv returns the argv needed to attach to the session with the exact
// given name. It performs no exec itself. -u makes tmux draw UTF-8 even
// when the attaching environment has no UTF-8 locale.
func (c *Client) AttachArgv(name string) []string {
	return append(append([]string{c.Bin, "-u"}, c.Socket...), "attach-session", "-t", "="+escapeArg(name))
}

// run execs the tmux binary with the socket arguments and the given args,
// capturing stdout and translating the error.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	// -u keeps tmux from replacing non-ASCII bytes with "_" when the
	// environment has no UTF-8 locale, as under launchd or a bare ssh command.
	full := append([]string{"-u"}, c.Socket...)
	if c.Config != "" {
		full = append(full, "-f", c.Config)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, c.Bin, full...)
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		return out.String(), c.mapError(err, errBuf.String())
	}
	return out.String(), nil
}

// mapError translates an exec error into a driver error, keeping the stderr
// text in the message.
func (c *Client) mapError(err error, stderr string) error {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return ErrNotInstalled
	}
	return fmt.Errorf("%w: %w", classifyStderr(stderr), err)
}

// classifyStderr maps the stderr text of a failed tmux invocation to a
// driver error. "no server running", a missing socket file ("error
// connecting to ... (No such file or directory)") and a server that went away
// mid-command ("server exited unexpectedly", "lost server") classify as
// errNoServer; session-missing
// messages classify as ErrNoSession; every other cause, including empty
// stderr and "Permission denied", is a generic error carrying the text
// verbatim.
func classifyStderr(stderr string) error {
	text := strings.TrimSpace(stderr)
	switch {
	case text == "":
		return errors.New("tmux: unknown error (empty stderr)")
	case strings.Contains(strings.ToLower(text), "no server running") ||
		// A bare "No such file or directory" can come from any path tmux
		// opens, so it only means no server next to the connect failure.
		(strings.Contains(strings.ToLower(text), "error connecting") &&
			strings.Contains(strings.ToLower(text), "no such file or directory")) ||
		strings.Contains(strings.ToLower(text), "server exited unexpectedly") ||
		strings.Contains(strings.ToLower(text), "lost server"):
		return fmt.Errorf("%w: %s", errNoServer, text)
	case strings.Contains(text, "can't find session") || strings.Contains(text, "no such session"):
		return fmt.Errorf("%w: %s", ErrNoSession, text)
	default:
		return fmt.Errorf("tmux: %s", text)
	}
}

// samePath reports whether tmux's reported pane path names dir, comparing
// cleaned paths and, where they exist, their resolved forms.
func samePath(reported, dir string) bool {
	a, b := filepath.Clean(reported), filepath.Clean(dir)
	if a == b {
		return true
	}
	if r, err := filepath.EvalSymlinks(a); err == nil && r == b {
		return true
	}
	return false
}
