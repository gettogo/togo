package harness

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode"
)

// MaxSendImages is how many pictures one prompt may carry.
const MaxSendImages = 10

// maxImagePath bounds one image path in harness.send.
const maxImagePath = 1024

// Image insertion styles (adapter images.insert).
const (
	// InsertPerPath pastes each image path on its own, one after another,
	// as if dropped one at a time, then the text. Claude Code takes only
	// the first path of a paste that holds several.
	InsertPerPath = "per_path"
	// InsertTogether pastes every path and the text at once.
	InsertTogether = "together"
)

// ValidateImagePaths checks harness.send images: at most MaxSendImages
// absolute, clean paths with an image extension and no control characters.
func ValidateImagePaths(paths []string) error {
	if len(paths) > MaxSendImages {
		return errors.New("too many images")
	}
	for _, p := range paths {
		switch {
		case p == "" || len(p) > maxImagePath:
			return errors.New("image path is empty or too long")
		case !filepath.IsAbs(p) || filepath.Clean(p) != p:
			return errors.New("image path must be absolute and clean")
		case strings.ContainsFunc(p, unicode.IsControl):
			return errors.New("image path contains a control character")
		case ImageMediaType(p) == "":
			return errors.New("image path is not an image")
		}
	}
	return nil
}

// Insertion is one paste into the agent's input; Submit presses Enter after it.
type Insertion struct {
	Text   string
	Submit bool
}

// Insertions is what the typist pastes for a prompt with pictures, in
// order, so every picture and the text land in one prompt that is sent
// once. per_path pastes each path alone (a space after it), then the text;
// together (and any unknown style) pastes the paths and the text at once.
func Insertions(text string, images []string, insert string) []Insertion {
	if len(images) == 0 {
		return []Insertion{{Text: text, Submit: true}}
	}
	if insert != InsertPerPath {
		return []Insertion{{Text: Together(text, images), Submit: true}}
	}
	out := make([]Insertion, 0, len(images)+1)
	for _, p := range images {
		out = append(out, Insertion{Text: QuotePath(p) + " "})
	}
	if strings.TrimSpace(text) != "" {
		out = append(out, Insertion{Text: text})
	}
	out[len(out)-1].Submit = true
	return out
}

// Together is the paths, then the text, as one line.
func Together(text string, images []string) string {
	parts := make([]string, 0, len(images)+1)
	for _, p := range images {
		parts = append(parts, QuotePath(p))
	}
	if strings.TrimSpace(text) != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, " ")
}

// QuotePath escapes a path the way a dropped file is typed: a backslash
// before each space or shell metacharacter, so it stays one word.
func QuotePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if strings.ContainsRune(" \t'\"\\()[]{}&;|<>*?$`!#~", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ImageRefs refers to sent pictures by path.
func ImageRefs(paths []string) []ImageRef {
	var out []ImageRef
	for _, p := range paths {
		if r, ok := ImageAtPath(p); ok {
			out = append(out, r)
		}
	}
	return out
}

// ImagePaths are the paths of refs that have one.
func ImagePaths(refs []ImageRef) []string {
	var out []string
	for _, r := range refs {
		if r.Path != "" {
			out = append(out, r.Path)
		}
	}
	return out
}
