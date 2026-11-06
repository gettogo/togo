package session

import (
	"fmt"
	"strconv"
)

const maxBib = 99

// lowestFreeBib returns the smallest two-digit number from 01 to 99 not in
// used, so numbers freed by killed sessions are handed out again first.
func lowestFreeBib(used map[string]bool) (string, error) {
	for n := 1; n <= maxBib; n++ {
		if bib := fmt.Sprintf("%02d", n); !used[bib] {
			return bib, nil
		}
	}
	return "", &InvalidParamsError{Msg: "all 99 session numbers in use"}
}

// isBibNumber reports whether s is one or two ASCII digits. strconv.Atoi
// alone would also accept signs such as "+1".
func isBibNumber(s string) bool {
	if len(s) < 1 || len(s) > 2 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validBib(bib string) bool {
	n, err := strconv.Atoi(bib)
	return len(bib) == 2 && isBibNumber(bib) && err == nil && n >= 1
}
