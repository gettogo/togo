package session

import "fmt"

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
