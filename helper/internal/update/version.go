package update

import (
	"regexp"
	"strconv"
	"strings"
)

var versionRE = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// ValidVersion reports whether v is a release version: vMAJOR.MINOR.PATCH
// with an optional pre-release, and no build metadata.
func ValidVersion(v string) bool {
	return len(v) <= 64 && versionRE.MatchString(v)
}

// Compare orders two versions by semantic version precedence and returns
// -1, 0 or 1. A string that is not a release version sorts before every
// release version, and equal to another such string.
func Compare(a, b string) int {
	va, vb := ValidVersion(a), ValidVersion(b)
	switch {
	case !va && !vb:
		return 0
	case !va:
		return -1
	case !vb:
		return 1
	}
	ma, pa := split(a)
	mb, pb := split(b)
	for i := range 3 {
		if c := compareNumeric(ma[i], mb[i]); c != 0 {
			return c
		}
	}
	switch {
	case pa == "" && pb == "":
		return 0
	case pa == "":
		return 1
	case pb == "":
		return -1
	}
	ia, ib := strings.Split(pa, "."), strings.Split(pb, ".")
	for i := 0; i < len(ia) && i < len(ib); i++ {
		if c := compareIdent(ia[i], ib[i]); c != 0 {
			return c
		}
	}
	return compareInt(len(ia), len(ib))
}

func split(v string) (main [3]string, pre string) {
	core, pre, _ := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	copy(main[:], strings.SplitN(core, ".", 3))
	return main, pre
}

// compareNumeric compares digit strings without leading zeros of any
// length.
func compareNumeric(a, b string) int {
	if c := compareInt(len(a), len(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func compareIdent(a, b string) int {
	_, errA := strconv.ParseUint(a, 10, 64)
	_, errB := strconv.ParseUint(b, 10, 64)
	switch {
	case errA == nil && errB == nil:
		return compareNumeric(strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0"))
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
