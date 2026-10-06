// Package avatar turns an uploaded file into the square PNG or JPEG that is
// stored, after checking it is a real image of sane size.
package avatar

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"

	_ "image/gif"

	_ "golang.org/x/image/webp"

	xdraw "golang.org/x/image/draw"
)

const (
	MaxUploadBytes = 2 << 20
	MaxSide        = 4096
	MaxPixels      = 16 << 20
	OutputSide     = 460
	jpegQuality    = 90
)

var (
	ErrTooLarge        = errors.New("avatar: file is larger than 2 MB")
	ErrUnsupportedType = errors.New("avatar: not a PNG, JPEG, GIF or WebP image")
	ErrDimensions      = errors.New("avatar: image dimensions out of range")
	ErrInvalidImage    = errors.New("avatar: image could not be decoded")
)

// allowedTypes maps a sniffed Content-Type to the image package's format
// name. Decoders register process-wide, so the decoded format is checked too.
var allowedTypes = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpeg",
	"image/gif":  "gif",
	"image/webp": "webp",
}

// Image is a processed avatar, ready to store.
type Image struct {
	Data []byte
	Ext  string // "png" or "jpg"
	// SHA256 is the hex digest of Data.
	SHA256 string
}

func (i Image) ContentType() string {
	if i.Ext == "png" {
		return "image/png"
	}
	return "image/jpeg"
}

// Process reads at most MaxUploadBytes from r. The type comes from the bytes
// alone; a filename or declared Content-Type is never consulted.
func Process(r io.Reader) (Image, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxUploadBytes+1))
	if err != nil {
		return Image{}, fmt.Errorf("avatar: read upload: %w", err)
	}
	if len(data) > MaxUploadBytes {
		return Image{}, ErrTooLarge
	}
	format, ok := allowedTypes[http.DetectContentType(data)]
	if !ok {
		return Image{}, ErrUnsupportedType
	}
	// The header check runs before any pixel is decoded, which is what stops
	// a small file that declares enormous dimensions.
	cfg, cfgFormat, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfgFormat != format {
		return Image{}, ErrInvalidImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxSide || cfg.Height > MaxSide || cfg.Width*cfg.Height > MaxPixels {
		return Image{}, ErrDimensions
	}
	src, srcFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil || srcFormat != format {
		return Image{}, ErrInvalidImage
	}
	out := squareThumb(src)

	var buf bytes.Buffer
	ext := "jpg"
	if out.Opaque() {
		err = jpeg.Encode(&buf, out, &jpeg.Options{Quality: jpegQuality})
	} else {
		ext = "png"
		err = png.Encode(&buf, out)
	}
	if err != nil {
		return Image{}, fmt.Errorf("avatar: encode: %w", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return Image{Data: buf.Bytes(), Ext: ext, SHA256: hex.EncodeToString(sum[:])}, nil
}

// squareThumb centre-crops src to a square and scales it down to OutputSide,
// never up.
func squareThumb(src image.Image) *image.NRGBA {
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	x0 := b.Min.X + (b.Dx()-side)/2
	y0 := b.Min.Y + (b.Dy()-side)/2
	crop := image.Rect(x0, y0, x0+side, y0+side)

	outSide := min(side, OutputSide)
	dst := image.NewNRGBA(image.Rect(0, 0, outSide, outSide))
	if outSide == side {
		draw.Draw(dst, dst.Bounds(), src, crop.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, xdraw.Src, nil)
	}
	return dst
}
