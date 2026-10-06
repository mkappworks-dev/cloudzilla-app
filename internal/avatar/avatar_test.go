package avatar_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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

	"github.com/mkappworks-dev/cloudzilla-app/internal/avatar"
)

func solid(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeGIF(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func process(t *testing.T, data []byte) avatar.Image {
	t.Helper()
	out, err := avatar.Process(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	return out
}

func decodeOut(t *testing.T, out avatar.Image) image.Image {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(out.Data))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if (out.Ext == "png") != (format == "png") {
		t.Errorf("Ext %q but data is %s", out.Ext, format)
	}
	return img
}

func TestProcess_AcceptsEachFormat(t *testing.T) {
	webp, err := os.ReadFile("testdata/opaque.webp")
	if err != nil {
		t.Fatal(err)
	}
	opaque := solid(64, 48, color.NRGBA{200, 30, 30, 255})
	for name, data := range map[string][]byte{
		"png":  encodePNG(t, opaque),
		"jpeg": encodeJPEG(t, opaque),
		"gif":  encodeGIF(t, opaque),
		"webp": webp,
	} {
		t.Run(name, func(t *testing.T) {
			out := process(t, data)
			sum := sha256.Sum256(out.Data)
			if out.SHA256 != hex.EncodeToString(sum[:]) {
				t.Errorf("SHA256 doesn't match the data")
			}
			b := decodeOut(t, out).Bounds()
			if b.Dx() != b.Dy() {
				t.Errorf("output %dx%d is not square", b.Dx(), b.Dy())
			}
		})
	}
}

func TestProcess_RejectsGIFHeaderPolyglot(t *testing.T) {
	if _, err := avatar.Process(strings.NewReader("GIF89a<html><script>alert(1)</script></html>")); err == nil {
		t.Error("a GIF header followed by HTML was accepted")
	}
}

func TestProcess_RejectsNonImages(t *testing.T) {
	cases := map[string]string{
		"svg":          `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"svg with xml": `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`,
		"html":         `<!DOCTYPE html><html><body><script>alert(1)</script></body></html>`,
		"pdf":          "%PDF-1.7\n1 0 obj\n<<>>\nendobj\n",
		"plain text":   "just some text",
		"empty":        "",
		"bmp":          "BM" + strings.Repeat("\x00", 60),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := avatar.Process(strings.NewReader(body))
			if !errors.Is(err, avatar.ErrUnsupportedType) && !errors.Is(err, avatar.ErrInvalidImage) {
				t.Errorf("err = %v, want ErrUnsupportedType or ErrInvalidImage", err)
			}
		})
	}
}

// pngHeader is a PNG signature and IHDR chunk declaring w x h, with no pixel data.
func pngHeader(w, h uint32) []byte {
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	buf.Write(chunk)
	_ = binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return buf.Bytes()
}

func TestProcess_RejectsHugeDimensionsBeforeDecode(t *testing.T) {
	for name, data := range map[string][]byte{
		"65535 square":  pngHeader(65535, 65535),
		"one wide side": pngHeader(4097, 10),
		"zero side":     pngHeader(0, 10),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := avatar.Process(bytes.NewReader(data))
			if !errors.Is(err, avatar.ErrDimensions) && !errors.Is(err, avatar.ErrInvalidImage) {
				t.Errorf("err = %v, want ErrDimensions", err)
			}
		})
	}
	if _, err := avatar.Process(bytes.NewReader(pngHeader(65535, 65535))); !errors.Is(err, avatar.ErrDimensions) {
		t.Errorf("65535 square: err = %v, want ErrDimensions", err)
	}
}

func TestProcess_RejectsOverSizeLimit(t *testing.T) {
	data := append(encodePNG(t, solid(8, 8, color.White)), make([]byte, avatar.MaxUploadBytes)...)
	data = data[:avatar.MaxUploadBytes+1]
	if _, err := avatar.Process(bytes.NewReader(data)); !errors.Is(err, avatar.ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

// withEXIF inserts an APP1 Exif segment carrying a GPS tag after the JPEG SOI.
func withEXIF(jpg []byte) []byte {
	payload := []byte("Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08GPSLatitude=51.5074N")
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, payload...)
	return append(out, jpg[2:]...)
}

func TestProcess_StripsEXIF(t *testing.T) {
	in := withEXIF(encodeJPEG(t, solid(32, 32, color.NRGBA{10, 120, 200, 255})))
	if !bytes.Contains(in, []byte("GPSLatitude")) {
		t.Fatal("fixture lacks the GPS tag")
	}
	out := process(t, in)
	if bytes.Contains(out.Data, []byte("Exif")) || bytes.Contains(out.Data, []byte("GPSLatitude")) {
		t.Error("output still carries the EXIF segment")
	}
	if bytes.Contains(out.Data, []byte{0xFF, 0xE1}) {
		t.Error("output has an APP1 marker")
	}
}

func TestProcess_AlphaGivesPNGAndOpaqueGivesJPEG(t *testing.T) {
	translucent := solid(20, 20, color.NRGBA{0, 0, 0, 255})
	translucent.Set(3, 3, color.NRGBA{0, 0, 0, 100})
	if out := process(t, encodePNG(t, translucent)); out.Ext != "png" || out.ContentType() != "image/png" {
		t.Errorf("translucent: Ext %q, want png", out.Ext)
	}
	if out := process(t, encodePNG(t, solid(20, 20, color.White))); out.Ext != "jpg" || out.ContentType() != "image/jpeg" {
		t.Errorf("opaque PNG: Ext %q, want jpg", out.Ext)
	}
}

func TestProcess_CropsAndScales(t *testing.T) {
	cases := []struct {
		name       string
		w, h, side int
	}{
		{"large landscape scales to 460", 1200, 800, 460},
		{"large portrait scales to 460", 700, 2000, 460},
		{"small is cropped, not upscaled", 100, 60, 60},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := decodeOut(t, process(t, encodePNG(t, solid(tc.w, tc.h, color.White)))).Bounds()
			if b.Dx() != tc.side || b.Dy() != tc.side {
				t.Errorf("got %dx%d, want %dx%d", b.Dx(), b.Dy(), tc.side, tc.side)
			}
		})
	}
}

// The crop keeps the centre: a wide image with red edges and a blue centre
// comes out blue.
func TestProcess_CropKeepsCentre(t *testing.T) {
	img := solid(300, 100, color.NRGBA{255, 0, 0, 255})
	for y := range 100 {
		for x := 100; x < 200; x++ {
			img.Set(x, y, color.NRGBA{0, 0, 255, 255})
		}
	}
	out := decodeOut(t, process(t, encodePNG(t, img)))
	r, _, b, _ := out.At(0, 50).RGBA()
	if r > 0x3000 || b < 0xC000 {
		t.Errorf("left edge of output is not from the centre: r=%x b=%x", r, b)
	}
}

func TestProcess_SameInputSameHash(t *testing.T) {
	data := encodePNG(t, solid(40, 40, color.NRGBA{1, 2, 3, 255}))
	if a, b := process(t, data), process(t, data); a.SHA256 != b.SHA256 {
		t.Error("the same upload hashed differently")
	}
}
