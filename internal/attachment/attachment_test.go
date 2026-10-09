package attachment_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/attachment"
)

func pixels(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	return img
}

func TestValidate_AcceptsEachFormatUnchanged(t *testing.T) {
	var pngBuf, jpgBuf, gifBuf bytes.Buffer
	if err := png.Encode(&pngBuf, pixels(8, 8)); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpgBuf, pixels(8, 8), nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&gifBuf, pixels(8, 8), nil); err != nil {
		t.Fatal(err)
	}
	webp, err := os.ReadFile("testdata/opaque.webp")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, ext, contentType string
		data                   []byte
	}{
		{"png", "png", "image/png", pngBuf.Bytes()},
		{"jpeg", "jpg", "image/jpeg", jpgBuf.Bytes()},
		{"gif", "gif", "image/gif", gifBuf.Bytes()},
		{"webp", "webp", "image/webp", webp},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := attachment.Validate(bytes.NewReader(c.data))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if got.Ext != c.ext || got.ContentType != c.contentType {
				t.Errorf("ext/type = %q/%q, want %q/%q", got.Ext, got.ContentType, c.ext, c.contentType)
			}
			if !bytes.Equal(got.Data, c.data) {
				t.Error("bytes were changed; attachments are stored as uploaded")
			}
		})
	}
}

func TestValidate_RejectsNonImages(t *testing.T) {
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, pixels(4, 4))
	cases := map[string][]byte{
		"svg":           []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"html":          []byte("<!doctype html><script>alert(1)</script>"),
		"pdf":           []byte("%PDF-1.7\n"),
		"empty":         {},
		"truncated png": pngBuf.Bytes()[:20],
		"text":          []byte("hello"),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := attachment.Validate(bytes.NewReader(data))
			if !errors.Is(err, attachment.ErrUnsupportedType) && !errors.Is(err, attachment.ErrInvalidImage) {
				t.Fatalf("err = %v, want ErrUnsupportedType or ErrInvalidImage", err)
			}
		})
	}
}

func TestValidate_SizeLimit(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, pixels(4, 4))
	atLimit := append(buf.Bytes(), bytes.Repeat([]byte{0}, attachment.MaxBytes-buf.Len())...)
	if _, err := attachment.Validate(bytes.NewReader(atLimit)); err != nil {
		t.Fatalf("a file of exactly MaxBytes: %v", err)
	}
	over := append(atLimit, 0)
	if _, err := attachment.Validate(bytes.NewReader(over)); !errors.Is(err, attachment.ErrTooLarge) {
		t.Fatalf("one byte over: err = %v, want ErrTooLarge", err)
	}
}

func TestValidate_RejectsHugeDeclaredDimensions(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, pixels(4, 4))
	data := buf.Bytes()
	// IHDR data is bytes 16-28 and its CRC the next four; the decoder checks it.
	copy(data[16:24], []byte{0, 0xFF, 0xFF, 0xFF, 0, 0xFF, 0xFF, 0xFF})
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, err := attachment.Validate(bytes.NewReader(data)); !errors.Is(err, attachment.ErrDimensions) {
		t.Fatalf("err = %v, want ErrDimensions", err)
	}
}

func TestToken_IsUnguessableAndWellFormed(t *testing.T) {
	a, b := attachment.NewToken(), attachment.NewToken()
	if a == b || len(a) != 32 || strings.Trim(a, "0123456789abcdef") != "" {
		t.Fatalf("tokens %q, %q", a, b)
	}
}
