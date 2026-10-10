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
// A dev Set leaves files nil and resolves the tree on every call instead.
type Set struct {
	fsys  fs.FS
	files map[string]*file
	dev   bool
}

type file struct {
	name string
	hash string

	gzipOnce sync.Once
	gzipped  []byte
}

var compressible = map[string]bool{".css": true, ".js": true, ".svg": true}

// New hashes every file in fsys, whose root is served at the URL root. A dev
// build gets a dev Set instead, so an asset edit needs no rebuild.
func New(fsys fs.FS) (*Set, error) {
	if devMode {
		return NewDev(fsys), nil
	}
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

// NewDev returns a Set that resolves every file when it is asked for, so an
// edit under the served tree needs no restart. Exported for the tests, which
// are an external package; the dev build tag is what reaches it in a binary.
func NewDev(fsys fs.FS) *Set {
	return &Set{fsys: fsys, dev: true}
}

// devName resolves a URL path against fsys, where an fs name carries no
// leading slash and no "..", so a traversal attempt is simply not valid.
func devName(urlPath string) (string, bool) {
	name := strings.TrimPrefix(urlPath, "/")
	return name, fs.ValidPath(name)
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
	if s.dev {
		// Hashed on every render rather than stamped from mtime, whose
		// resolution varies by filesystem: two same-size edits in one second
		// would otherwise keep serving the first.
		name, ok := devName(urlPath)
		if !ok {
			return urlPath
		}
		hash, err := hashFile(s.fsys, name)
		if err != nil {
			return urlPath
		}
		return urlPath + "?v=" + hash
	}
	if f, ok := s.files[urlPath]; ok {
		return urlPath + "?v=" + f.hash
	}
	return urlPath
}

// ServeHTTP serves the file at r.URL.Path. Only a request carrying the file's
// current hash may cache it for good; any other is told to revalidate, so an
// instance that is mid-upgrade can't pin the wrong bytes under a new URL.
func (s *Set) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.dev {
		s.serveDev(w, r)
		return
	}
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

// serveDev serves the file as it is on disk right now. It sends no validators
// and no gzip: the release path's immutable hashed URL and its per-file gzip
// sync.Once would both pin one edit for the rest of the session.
func (s *Set) serveDev(w http.ResponseWriter, r *http.Request) {
	name, ok := devName(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	info, err := fs.Stat(s.fsys, name)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFileFS(w, r, s.fsys, name)
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
