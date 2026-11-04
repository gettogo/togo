package session

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var (
	managedIDPattern   = regexp.MustCompile(`^s_[a-z2-7]{10}$`)
	unmanagedIDPattern = regexp.MustCompile(`^u_([0-9a-f]{2})+$`)
	idEncoding         = base32.StdEncoding.WithPadding(base32.NoPadding)
)

// ValidateID reports whether id names a managed session ("s_" + 10 base32
// characters) or an unmanaged one ("u_" + hex of the tmux name). Any other
// string is an *InvalidParamsError.
func ValidateID(id string) (managed bool, err error) {
	switch {
	case managedIDPattern.MatchString(id):
		return true, nil
	case unmanagedIDPattern.MatchString(id):
		return false, nil
	default:
		return false, &InvalidParamsError{Msg: fmt.Sprintf("invalid session id %q", id)}
	}
}

// newID returns "s_" and 10 characters of lowercase base32 drawn from 7
// random bytes (56 bits, of which the id keeps 50).
func newID() string {
	b := make([]byte, 7)
	_, _ = rand.Read(b) // crypto/rand.Read never fails since Go 1.24.
	return "s_" + strings.ToLower(idEncoding.EncodeToString(b))[:10]
}

func unmanagedID(name string) string {
	return "u_" + hex.EncodeToString([]byte(name))
}

func unmanagedName(id string) (string, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(id, "u_"))
	if err != nil {
		return "", &InvalidParamsError{Msg: fmt.Sprintf("invalid session id %q", id)}
	}
	return string(b), nil
}
