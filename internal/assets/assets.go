// Package assets serves the embedded frontend under content-hashed URLs, with
// validators and gzip.
package assets

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Set is a frontend tree with each file's content hash, keyed by URL path.
type Set struct {
	fsys  fs.FS
	files map[string]*file
}

type file struct {
	name string
	hash string

	gzipOnce sync.Once
	gzipped  []byte
}

var compressible = map[string]bool{".css": true, ".js": true, ".svg": true}

// New hashes every file in fsys, whose root is served at the URL root.
func New(fsys fs.FS) (*Set, error) {
	s := &Set{fsys: fsys, files: map[string]*file{}}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		switch {
		case name == "." && errors.Is(err, fs.ErrNotExist):
			return fs.SkipAll
		case err != nil:
			return err
		case d.IsDir():
			return nil
		}
		hash, err := hashFile(fsys, name)
		if err != nil {
			return err
		}
		s.files["/"+name] = &file{name: name, hash: hash}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

func hashFile(fsys fs.FS, name string) (string, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)[:8]), nil
}

// URL returns urlPath with its content hash as the query, or urlPath alone
// when s has no such file.
func (s *Set) URL(urlPath string) string {
	if f, ok := s.files[urlPath]; ok {
		return urlPath + "?v=" + f.hash
	}
	return urlPath
}

// ServeHTTP serves the file at r.URL.Path. Only a request carrying the file's
// current hash may cache it for good; any other is told to revalidate, so an
// instance that is mid-upgrade can't pin the wrong bytes under a new URL.
func (s *Set) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f, ok := s.files[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	if r.URL.Query().Get("v") == f.hash {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	if compressible[path.Ext(f.name)] {
		h.Set("Vary", "Accept-Encoding")
		if acceptsGzip(r.Header) {
			if gz := s.gzipped(f); gz != nil {
				h.Set("Content-Encoding", "gzip")
				h.Set("ETag", `"`+f.hash+`-gzip"`)
				// ServeContent omits Content-Length once Content-Encoding is set (go.dev/issue/66343).
				h.Set("Content-Length", strconv.Itoa(len(gz)))
				http.ServeContent(w, r, f.name, time.Time{}, bytes.NewReader(gz))
				return
			}
		}
	}
	h.Set("ETag", `"`+f.hash+`"`)
	http.ServeFileFS(w, r, s.fsys, f.name)
}

// gzipped returns f compressed, or nil when gzip doesn't shrink it. It runs on
// first request rather than in New, so startup doesn't wait to compress mermaid.
func (s *Set) gzipped(f *file) []byte {
	f.gzipOnce.Do(func() {
		body, err := fs.ReadFile(s.fsys, f.name)
		if err != nil {
			return
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, _ = zw.Write(body)
		_ = zw.Close()
		if buf.Len() < len(body) {
			f.gzipped = buf.Bytes()
		}
	})
	return f.gzipped
}

func acceptsGzip(h http.Header) bool {
	for _, field := range h.Values("Accept-Encoding") {
		for _, coding := range strings.Split(field, ",") {
			name, params, _ := strings.Cut(coding, ";")
			if strings.EqualFold(strings.TrimSpace(name), "gzip") {
				return !zeroQuality(params)
			}
		}
	}
	return false
}

func zeroQuality(params string) bool {
	for _, param := range strings.Split(params, ";") {
		key, value, _ := strings.Cut(param, "=")
		if strings.EqualFold(strings.TrimSpace(key), "q") {
			q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			return err == nil && q == 0
		}
	}
	return false
}

var defaultSet atomic.Pointer[Set]

// SetDefault makes s the set the package-level URL resolves against.
func SetDefault(s *Set) { defaultSet.Store(s) }

// URL resolves urlPath against the default set, returning it unchanged when
// there is none.
func URL(urlPath string) string {
	if s := defaultSet.Load(); s != nil {
		return s.URL(urlPath)
	}
	return urlPath
}
