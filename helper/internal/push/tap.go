package push

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/gettogo/togo/helper/internal/logfile"
	"strings"
	"sync"
	"unicode"
)

// TapEnv names a file that, when set, receives one JSON line per relay
// request the Sender makes. It exists for end-to-end tests, which must show
// that nothing readable leaves the host, so the line holds hashes only:
// the body's, and one per word in it. A test hashes a word it expects to
// stay sealed and checks it is absent.
const TapEnv = "TOGO_TEST_RELAY_TAP"

// TapRecord is one line of the tap file. Status is the relay's HTTP status,
// 0 when the request never got an answer; Reason is the relay's short
// reason code for a failure, such as an APNs rejection.
type TapRecord struct {
	Path   string   `json:"path"`
	Kind   string   `json:"kind"`
	Sha    string   `json:"sha"`
	Len    int      `json:"len"`
	Words  []string `json:"words"`
	Status int      `json:"status"`
	Reason string   `json:"reason,omitempty"`
}

// FileTap returns a tap that appends TapRecords to path, or nil when path
// is empty.
func FileTap(path string) func(TapRecord) {
	if path == "" {
		return nil
	}
	var mu sync.Mutex
	return func(r TapRecord) {
		line, err := json.Marshal(r)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		f, err := logfile.Open(path, logfile.MaxSize)
		if err != nil {
			return
		}
		defer f.Close()
		_, _ = f.Write(append(line, '\n'))
	}
}

// TapWords hashes each run of letters and digits in body, case folded, to
// the 6-character prefix used for ids in logs. Duplicates are kept once.
func TapWords(body []byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range strings.FieldsFunc(string(body), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		h := WordHash(w)
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// WordHash is the tap's hash of one word.
func WordHash(w string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(w)))
	return hex.EncodeToString(sum[:])[:6]
}

func (s *Sender) tap(path string, kind string, data []byte, err error) {
	if s.Tap == nil {
		return
	}
	sum := sha256.Sum256(data)
	r := TapRecord{Path: path, Kind: kind, Sha: hex.EncodeToString(sum[:])[:6], Len: len(data), Words: TapWords(data)}
	var st *errStatus
	switch {
	case err == nil:
		r.Status = 202
	case asStatus(err, &st):
		r.Status, r.Reason = st.code, st.reason
	}
	s.Tap(r)
}
