package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

// sealFitting seals p, shrinking it until the envelope fits the relay's
// ciphertext limit: options go first, then the body, then the subject is
// shortened, then the title goes. The question stays longest because it is
// what the user answers. If nothing else fits, it sends the generic push
// (kind, request id and time only), so the user still hears about it. The
// phone can fetch the full approval over SSH.
func sealFitting(pub [32]byte, p wire.PushPlaintext, aad []byte) (string, error) {
	generic := false
	for {
		raw, err := json.Marshal(p)
		if err != nil {
			return "", err
		}
		ct, err := seal.Seal(pub, raw, seal.LabelPush, aad)
		if err != nil {
			return "", fmt.Errorf("seal: %w", err)
		}
		switch {
		case len(ct) <= maxCiphertext:
			return ct, nil
		case len(p.Options) > 0:
			p.Options = p.Options[:len(p.Options)-1]
		case p.Body != "":
			p.Body = ""
		case p.Subject != nil && utf8.RuneCountInString(p.Subject.Text) > minSubject:
			p.Subject = shorten(*p.Subject)
		case p.SessionName != nil:
			p.SessionName = nil
		case p.Title != "":
			p.Title = ""
		case !generic:
			generic = true
			p = wire.PushPlaintext{V: p.V, Kind: p.Kind, RequestID: p.RequestID, TS: p.TS}
		default:
			return "", errors.New("push too large")
		}
	}
}

// minSubject is the shortest a subject is cut to before the title goes.
const minSubject = 16

// shorten halves the subject text, cutting it the way its kind reads
// best (see shortSubject). The result still passes Subject.Validate.
func shorten(s wire.Subject) *wire.Subject {
	v := shortSubject(s, utf8.RuneCountInString(s.Text)/2)
	return &v
}
