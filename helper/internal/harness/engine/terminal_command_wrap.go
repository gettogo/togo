package engine

import "strings"

// commandBlock finds the input line holding exactly the command, and
// returns the rest of the screen without it. A command wider than the
// terminal runs on to the lines below: the first must begin as the input
// line did when the command's name was typed, and the lines after it must
// carry on the command exactly.
//
// Where a line breaks, the screen does not always say whether a space
// stood there: "shared docs" broken at its space and "shareddocs" broken
// at the terminal's edge can look the same. So a break is taken only where
// it is certain to be a space: the line before it is shorter than another
// line of the same input, so it did not reach the edge and nothing but a
// break between words ends it; and the command has exactly one space
// there. How far a line reaches is counted in terminal cells, and only for
// lines whose every character is known to take one cell: with a wide or
// combining character in the input (Chinese, an emoji) the widths are not
// known here, and the command is not read across lines at all. A break after the input's longest line, or inside a word, is not
// read: the command is then not found, and is not run.
func commandBlock(screen, typedLine, whole string) (found bool, rest string) {
	lines := strings.Split(screen, "\n")
	for i, l := range lines {
		first := trimEdge(l)
		if first == whole {
			return true, strings.Join(append(append([]string{}, lines[:i]...), lines[i+1:]...), "\n")
		}
		if typedLine == "" || !strings.HasPrefix(first, typedLine) || !strings.HasPrefix(whole, first) {
			continue
		}
		left, end := whole[len(first):], -1
		for j := i + 1; j < len(lines) && j <= i+8 && left != ""; j++ {
			part := strings.TrimSpace(trimEdge(lines[j]))
			// Exactly one space at the break: two would look the same.
			if part == "" || !strings.HasPrefix(left, " "+part) || strings.HasPrefix(part, " ") || strings.HasPrefix(left, "  ") {
				break
			}
			if left = left[len(part)+1:]; left == "" {
				end = j
			}
		}
		if end < 0 {
			continue
		}
		widest, certain := 0, true
		for _, b := range lines[i : end+1] {
			w, known := cells(b)
			widest, certain = max(widest, w), certain && known
		}
		for _, b := range lines[i:end] {
			w, _ := cells(b)
			certain = certain && w < widest
		}
		if certain {
			return true, strings.Join(append(append([]string{}, lines[:i]...), lines[end+1:]...), "\n")
		}
	}
	return false, screen
}

// wrapsBelow reports whether the input line that began with the typed
// command has text on the line under it.
func wrapsBelow(screen, typedLine string) bool {
	lines := strings.Split(screen, "\n")
	for i, l := range lines {
		if typedLine != "" && strings.HasPrefix(trimEdge(l), typedLine) && i+1 < len(lines) && strings.TrimSpace(trimEdge(lines[i+1])) != "" {
			return true
		}
	}
	return false
}

// cells is how many terminal cells a line takes up to its last character,
// and whether that is known: every character is printable ASCII or a
// box-drawing or block character, each one cell wide. Anything else may
// be two cells or none.
func cells(line string) (n int, known bool) {
	for _, r := range strings.TrimRight(line, " ") {
		if !(r >= 0x20 && r <= 0x7e) && !(r >= 0x2500 && r <= 0x259f) {
			return 0, false
		}
		n++
	}
	return n, true
}
