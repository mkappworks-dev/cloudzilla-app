// Package attachment checks the image a user pastes into markdown. Unlike
// avatars, attachments are stored as uploaded: re-encoding would flatten
// animated GIFs.
package attachment

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

const (
	MaxBytes = 10 << 20
	// MaxSide only guards against absurd headers; the server never decodes pixels.
	MaxSide = 16384
)

var (
	ErrTooLarge        = errors.New("attachment: file is larger than 10 MB")
	ErrUnsupportedType = errors.New("attachment: not a PNG, JPEG, GIF or WebP image")
	ErrDimensions      = errors.New("attachment: image dimensions out of range")
	ErrInvalidImage    = errors.New("attachment: image could not be read")
)

// types maps a sniffed Content-Type to its stored extension and the image
// package's format name. Decoders register process-wide, so the format the
// header decodes as is checked against it.
var types = map[string]struct{ ext, format string }{
	"image/png":  {"png", "png"},
	"image/jpeg": {"jpg", "jpeg"},
	"image/gif":  {"gif", "gif"},
	"image/webp": {"webp", "webp"},
}

// File is a validated upload, ready to store.
type File struct {
	Data        []byte
	Ext         string
	ContentType string
}

// Validate reads at most MaxBytes from r. The type comes from the bytes alone;
// a filename or declared Content-Type is never consulted.
func Validate(r io.Reader) (File, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return File{}, fmt.Errorf("attachment: read upload: %w", err)
	}
	if len(data) > MaxBytes {
		return File{}, ErrTooLarge
	}
	contentType := http.DetectContentType(data)
	t, ok := types[contentType]
	if !ok {
		return File{}, ErrUnsupportedType
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != t.format {
		return File{}, ErrInvalidImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxSide || cfg.Height > MaxSide {
		return File{}, ErrDimensions
	}
	return File{Data: data, Ext: t.ext, ContentType: contentType}, nil
}

// NewToken returns 128 random bits as hex. It is the attachment's name in
// URLs and object keys; access is still checked against the repo.
func NewToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("attachment: crypto/rand: %v", err))
	}
	return hex.EncodeToString(b)
}
