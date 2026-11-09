// Package authkeys takes a paired phone's SSH key off this machine. It
// only ever removes lines, and only lines Togo installed: those whose key
// equals the phone's and whose comment is a Togo comment, "togo@host" from
// earlier builds or "togo-<model>-<fingerprint>" from current ones. A user
// who imported their own key into the app keeps every other use of it.
package authkeys

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Conflict reasons, as the protocol names them.
const (
	ReasonSymlink    = "symlink"
	ReasonMissing    = "missing"
	ReasonNotRegular = "not_regular"
)

// CommentPrefix starts the comment of key lines from earlier builds of
// the app, such as "togo@Sams-iPhone".
const CommentPrefix = "togo@"

// DeviceCommentPrefix starts the comment current builds give a device
// key, such as "togo-iPhone-1a2b3c4d".
const DeviceCommentPrefix = "togo-"

// BackupSuffix names the copy of authorized_keys Remove keeps, beside it,
// from just before its last edit. sshd does not read it.
const BackupSuffix = ".togo-backup"

// IsTogoComment reports whether comment is one Togo gives the keys it
// makes.
func IsTogoComment(comment string) bool {
	return strings.HasPrefix(comment, CommentPrefix) || strings.HasPrefix(comment, DeviceCommentPrefix)
}

// maxFile bounds how much of authorized_keys is read. sshd itself reads
// far less in practice; a larger file is refused rather than truncated.
const maxFile = 4 << 20

// ErrBadKey reports a public key that is not a single OpenSSH key of a
// supported type with a matching blob.
var ErrBadKey = errors.New("authkeys: malformed public key")

// Conflict reports an authorized_keys file Remove will not touch: a
// symlink, a missing file or something other than a regular file. Nothing
// is written.
type Conflict struct{ Reason string }

func (c *Conflict) Error() string {
	return "authkeys: authorized_keys is " + map[string]string{
		ReasonSymlink:    "a symlink",
		ReasonMissing:    "missing",
		ReasonNotRegular: "not a regular file",
	}[c.Reason]
}

var keyRe = regexp.MustCompile(wire.SSHPublicKeyPattern)

// ParseKey checks that publicKey is "type base64" as the schema allows and
// that the blob decodes and names the same type. It returns the two
// fields.
func ParseKey(publicKey string) (keyType, blob string, err error) {
	if !keyRe.MatchString(publicKey) {
		return "", "", ErrBadKey
	}
	keyType, blob, _ = strings.Cut(publicKey, " ")
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil || len(raw) < 4 {
		return "", "", ErrBadKey
	}
	n := binary.BigEndian.Uint32(raw)
	if uint64(n) > uint64(len(raw)-4) || string(raw[4:4+n]) != keyType {
		return "", "", ErrBadKey
	}
	return keyType, blob, nil
}

// DefaultPath returns ~/.ssh/authorized_keys for the user running the
// helper: $HOME, or the passwd entry when HOME is unset, as under some
// service managers. A custom AuthorizedKeysFile in sshd_config is not
// consulted.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		u, uerr := user.Current()
		if uerr != nil || u.HomeDir == "" {
			return "", errors.New("authkeys: no home directory")
		}
		home = u.HomeDir
	}
	return filepath.Join(home, ".ssh", "authorized_keys"), nil
}

// Remove deletes from the authorized_keys file at path every line whose
// key is publicKey and whose comment is a Togo comment (IsTogoComment), and
// returns how many it removed. Every other byte stays as it was, including
// CRLF line ends and a missing final newline. Before the edit the file is
// copied to path+BackupSuffix; then it is replaced through a temp file in
// the same directory, synced and renamed. Both keep the file's mode. When
// nothing matches nothing is written at all.
func Remove(path, publicKey string) (int, error) {
	keyType, blob, err := ParseKey(publicKey)
	if err != nil {
		return 0, err
	}
	fi, data, err := load(path)
	if err != nil {
		return 0, err
	}
	kept, removed := filter(data, keyType, blob)
	if removed == 0 {
		return 0, nil
	}
	if err := replace(path+BackupSuffix, data, fi.Mode().Perm()); err != nil {
		return 0, err
	}
	if err := replace(path, kept, fi.Mode().Perm()); err != nil {
		return 0, err
	}
	return removed, nil
}

// Line is one key line of authorized_keys, without its options.
type Line struct {
	Type, Blob, Comment string
}

// Key returns the line's key as "type blob".
func (l Line) Key() string { return l.Type + " " + l.Blob }

// Lines returns the key lines of the authorized_keys file at path, for a
// caller that explains why Remove found nothing. It writes nothing and
// refuses the same files Remove does.
func Lines(path string) ([]Line, error) {
	_, data, err := load(path)
	if err != nil {
		return nil, err
	}
	var out []Line
	for _, raw := range strings.Split(string(data), "\n") {
		if l, ok := parseLine(strings.TrimSuffix(raw, "\r")); ok {
			out = append(out, l)
		}
	}
	return out, nil
}

// load reads the regular file at path without following a link.
func load(path string) (os.FileInfo, []byte, error) {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, nil, &Conflict{Reason: ReasonMissing}
	case err != nil:
		return nil, nil, fmt.Errorf("authkeys: %w", err)
	case fi.Mode()&os.ModeSymlink != 0:
		return nil, nil, &Conflict{Reason: ReasonSymlink}
	case !fi.Mode().IsRegular():
		return nil, nil, &Conflict{Reason: ReasonNotRegular}
	}
	data, err := readNoFollow(path, fi)
	if err != nil {
		return nil, nil, err
	}
	return fi, data, nil
}

// readNoFollow opens path without following a link and checks it is still
// the file Lstat saw, so a swap between the two cannot redirect the read.
func readNoFollow(path string, want os.FileInfo) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, &Conflict{Reason: ReasonSymlink}
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, &Conflict{Reason: ReasonMissing}
		}
		return nil, fmt.Errorf("authkeys: %w", err)
	}
	defer f.Close()
	got, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("authkeys: %w", err)
	}
	if !got.Mode().IsRegular() {
		return nil, &Conflict{Reason: ReasonNotRegular}
	}
	if !os.SameFile(got, want) {
		return nil, errors.New("authkeys: authorized_keys changed while it was read")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, fmt.Errorf("authkeys: %w", err)
	}
	if len(data) > maxFile {
		return nil, errors.New("authkeys: authorized_keys is too large")
	}
	return data, nil
}

// filter returns data without the matching lines. Each kept line is
// copied with its own terminator, so the result differs from data only by
// whole removed lines.
func filter(data []byte, keyType, blob string) ([]byte, int) {
	out := make([]byte, 0, len(data))
	removed := 0
	for rest := data; len(rest) > 0; {
		end := bytes.IndexByte(rest, '\n') + 1
		if end == 0 {
			end = len(rest)
		}
		line := rest[:end]
		rest = rest[end:]
		text := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		if matches(text, keyType, blob) {
			removed++
			continue
		}
		out = append(out, line...)
	}
	return out, removed
}

// matches reports whether one authorized_keys line, without its line end,
// holds exactly keyType and blob with a Togo comment.
func matches(line, keyType, blob string) bool {
	l, ok := parseLine(line)
	return ok && l.Type == keyType && l.Blob == blob && IsTogoComment(l.Comment)
}

// keyTypes are the key types a line can hold, so the first field of a line
// is known to be options when it is none of them.
var keyTypes = map[string]bool{
	"ssh-ed25519": true, "ssh-rsa": true, "ssh-dss": true,
	"ecdsa-sha2-nistp256": true, "ecdsa-sha2-nistp384": true, "ecdsa-sha2-nistp521": true,
	"sk-ssh-ed25519@openssh.com": true, "sk-ecdsa-sha2-nistp256@openssh.com": true,
}

// parseLine splits one authorized_keys line, without its line end, into
// its key and comment. The line is "[options] type blob [comment]", where
// options run to the first space or tab outside double quotes. Blank
// lines, comments and lines of no known key type are not key lines.
func parseLine(line string) (Line, bool) {
	s := strings.TrimLeft(line, " \t")
	if s == "" || s[0] == '#' {
		return Line{}, false
	}
	t, rest := field(s)
	if !keyTypes[t] {
		// Not a key type, so the first field is options.
		_, rest = optionsField(s)
		t, rest = field(rest)
		if !keyTypes[t] {
			return Line{}, false
		}
	}
	b, comment := field(rest)
	return Line{Type: t, Blob: b, Comment: strings.TrimRight(comment, " \t")}, true
}

// field returns the text up to the next space or tab and what follows the
// run of blanks after it.
func field(s string) (string, string) {
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimLeft(s[i:], " \t")
}

// optionsField splits off an options field, honoring double quotes and
// backslash-escaped quotes inside them, as sshd does.
func optionsField(s string) (string, string) {
	quoted := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && quoted && i+1 < len(s) && s[i+1] == '"':
			i++
		case c == '"':
			quoted = !quoted
		case (c == ' ' || c == '\t') && !quoted:
			return s[:i], strings.TrimLeft(s[i:], " \t")
		}
	}
	return s, ""
}

// replace writes data to a temp file beside path with mode, syncs it and
// renames it over path, then syncs the directory.
func replace(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("authkeys: %w", err)
	}
	defer os.Remove(tmp.Name())
	err = tmp.Chmod(mode)
	if err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("authkeys: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
