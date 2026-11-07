package upload

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	// Decoders for the formats fs.image can shrink.
	_ "image/gif"
	_ "image/png"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/session"
)

const (
	// MaxImageSource is the largest picture fs.image opens.
	MaxImageSource = 40 << 20
	// maxImagePixels bounds the pictures fs.image decodes to shrink.
	maxImagePixels = 50_000_000
	// imageSide is the long side a shrunk picture starts from.
	imageSide = 2048
)

// ImageResult is the fs.image result.
type ImageResult struct {
	MediaType string `json:"mediaType"`
	// Data is the picture as base64url without padding, at most MaxBytes.
	Data string `json:"data"`
	// Bytes is the file's size on disk.
	Bytes int64 `json:"bytes"`
	// Scaled is true when the picture was shrunk to fit.
	Scaled bool `json:"scaled"`
}

// image reads a picture for the chat: only from the session's own upload
// directory or its working folder, after symlinks, and only image files.
// A picture over MaxBytes is shrunk to a JPEG that fits when it is PNG,
// JPEG or GIF; any other large picture is refused.
func (s *Service) image(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	managed, err := session.ValidateID(p.SessionID)
	if err != nil {
		return nil, err
	}
	if !managed {
		return nil, session.ErrNotManaged
	}
	sess, err := s.Sessions.Resolve(ctx, p.SessionID)
	if err != nil {
		return nil, err
	}
	full, err := s.ImagePath(sess, p.Path)
	if err != nil {
		return nil, err
	}
	return readImage(full)
}

func invalid(format string, args ...any) error {
	return &session.InvalidParamsError{Msg: fmt.Sprintf(format, args...)}
}

// ImagePath resolves path for fs.image: absolute and clean, an image
// extension, and after symlinks inside the session's upload directory or
// working folder.
func (s *Service) ImagePath(sess session.Session, path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", invalid("path must be absolute and clean")
	}
	if imageExt[strings.ToLower(filepath.Ext(path))] == "" {
		return "", invalid("path is not an image")
	}
	full, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", invalid("image not found")
	}
	var roots []string
	roots = append(roots, filepath.Join(s.Dir(), sess.ID))
	if sess.Cwd != "" {
		roots = append(roots, sess.Cwd)
	}
	for _, root := range roots {
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(r, full); err == nil && filepath.IsLocal(rel) {
			return full, nil
		}
	}
	return "", invalid("image is outside the session's folders")
}

var imageExt = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".heic": "image/heic",
}

func readImage(full string) (ImageResult, error) {
	f, err := os.Open(full)
	if err != nil {
		return ImageResult{}, invalid("image not readable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return ImageResult{}, invalid("image is not a plain file")
	}
	if info.Size() > MaxImageSource {
		return ImageResult{}, tooLarge()
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxImageSource+1))
	if err != nil {
		return ImageResult{}, fmt.Errorf("fs.image: %w", err)
	}
	mt := sniff(data)
	if mt == "" {
		return ImageResult{}, invalid("file is not an image")
	}
	res := ImageResult{MediaType: mt, Bytes: info.Size()}
	if len(data) <= MaxBytes {
		res.Data = base64.RawURLEncoding.EncodeToString(data)
		return res, nil
	}
	small, err := shrink(data, MaxBytes)
	if err != nil {
		return ImageResult{}, err
	}
	res.MediaType, res.Data, res.Scaled = "image/jpeg", base64.RawURLEncoding.EncodeToString(small), true
	return res, nil
}

// sniff is the media type of an image's bytes, or "" for anything else.
func sniff(data []byte) string {
	switch mt := http.DetectContentType(data); mt {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return mt
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "heic", "heix", "mif1", "msf1", "hevc":
			return "image/heic"
		}
	}
	return ""
}

var errCannotShrink = errors.New("fs.image: the picture is too large and cannot be shrunk")

// shrink re-encodes a PNG, JPEG or GIF as a JPEG of at most limit bytes,
// halving its long side from imageSide until it fits.
func shrink(data []byte, limit int) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxImagePixels {
		return nil, &session.InvalidParamsError{Msg: errCannotShrink.Error()}
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, &session.InvalidParamsError{Msg: errCannotShrink.Error()}
	}
	for side := imageSide; side >= 64; side /= 2 {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, scale(src, side), &jpeg.Options{Quality: 80}); err != nil {
			return nil, fmt.Errorf("fs.image: %w", err)
		}
		if buf.Len() <= limit {
			return buf.Bytes(), nil
		}
	}
	return nil, &session.InvalidParamsError{Msg: errCannotShrink.Error()}
}

// scale fits img in side x side by sampling the nearest pixel; a picture
// already that small is only flattened onto white.
func scale(img image.Image, side int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if long := max(w, h); long > side {
		w, h = max(1, w*side/long), max(1, h*side/long)
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	for y := range h {
		sy := b.Min.Y + y*b.Dy()/h
		for x := range w {
			sx := b.Min.X + x*b.Dx()/w
			c := img.At(sx, sy)
			if _, _, _, a := c.RGBA(); a == 0xffff {
				out.Set(x, y, c)
			} else {
				draw.Draw(out, image.Rect(x, y, x+1, y+1), image.NewUniform(c), image.Point{}, draw.Over)
			}
		}
	}
	return out
}
