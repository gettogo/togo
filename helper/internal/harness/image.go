package harness

import (
	"encoding/base64"
	"path/filepath"
	"strings"
)

// imageTypes maps the image extensions the chat shows to their media type.
var imageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".heic": "image/heic",
}

// ImageMediaType is the media type of an image file name, or "" when the
// extension is not a picture the chat shows.
func ImageMediaType(name string) string {
	return imageTypes[strings.ToLower(filepath.Ext(name))]
}

// ImageAtPath refers to an image file by absolute path. ok is false for a
// relative path or a name that is not an image.
func ImageAtPath(path string) (ImageRef, bool) {
	mt := ImageMediaType(path)
	if mt == "" || !filepath.IsAbs(path) {
		return ImageRef{}, false
	}
	return ImageRef{Path: filepath.Clean(path), MediaType: mt}, true
}

// InlineImage turns a standard base64 image block, as transcripts store
// them, into a reference: the data travels only when it is at most
// MaxInlineImage bytes, otherwise only its type and size do.
func InlineImage(mediaType, b64 string) (ImageRef, bool) {
	if !strings.HasPrefix(mediaType, "image/") {
		return ImageRef{}, false
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ImageRef{}, false
	}
	ref := ImageRef{MediaType: mediaType, Bytes: len(data)}
	if len(data) <= MaxInlineImage {
		ref.Data = base64.RawURLEncoding.EncodeToString(data)
	}
	return ref, true
}

// ImagePolicy says how the harness takes pictures in a prompt.
type ImagePolicy struct {
	// Insert is per_path or together (see Insertions).
	Insert string `json:"insert"`
}
