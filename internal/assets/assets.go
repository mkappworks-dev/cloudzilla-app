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

// Set serves a frontend tree and tells templates which URL to reference it by.
// Two implementations: hashed, which walks the tree once and caches a hash per
// file forever, and dev, which resolves the tree per request.
type Set interface {
	// URL returns urlPath carrying the file's content hash, or urlPath alone
	// when there is no such file.
	URL(urlPath string) string
	http.Handler
}

// hashedSet is the release implementation: an embedded tree cannot change under
// it, so one walk at startup is enough.
type hashedSet struct {
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

// New hashes every file in fsys, whose root is served at the URL root. A dev
// build gets a dev Set instead, so an asset edit needs no rebuild.
func New(fsys fs.FS) (Set, error) {
	if devMode {
		return NewDev(fsys), nil
	}
	s := &hashedSet{fsys: fsys, files: map[string]*file{}}
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

// devSet reads the working tree, so it has to resolve a file when it is asked
// for rather than trusting a walk done at startup.
type devSet struct {
	fsys fs.FS

	mu    sync.Mutex
	cache map[string]devEntry
}

// devEntry is a hash and what it was computed from. Keying the cache on mtime
// and size assumes sub-second timestamps, which APFS and ext4 both give; on a
// filesystem with one-second stamps a same-size edit inside one tick would be
// missed. That is the trade for not re-reading 5.3 MB of mermaid per render.
type devEntry struct {
	hash    string
	modTime time.Time
	size    int64
}

// NewDev returns a Set that resolves every file when it is asked for, so an
// edit under the served tree needs no restart. Exported for the tests, which
// are an external package; the dev build tag is what reaches it in a binary.
func NewDev(fsys fs.FS) Set {
	return &devSet{fsys: fsys, cache: map[string]devEntry{}}
}

// hash returns the file's content hash, re-reading it only when stat says it
// has changed since the last call.
func (s *devSet) hash(name string) (string, error) {
	info, err := fs.Stat(s.fsys, name)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.cache[name]; ok && e.modTime.Equal(info.ModTime()) && e.size == info.Size() {
		return e.hash, nil
	}
	hash, err := hashFile(s.fsys, name)
	if err != nil {
		return "", err
	}
	s.cache[name] = devEntry{hash: hash, modTime: info.ModTime(), size: info.Size()}
	return hash, nil
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

func (s *hashedSet) URL(urlPath string) string {
	if f, ok := s.files[urlPath]; ok {
		return urlPath + "?v=" + f.hash
	}
	return urlPath
}

func (s *devSet) URL(urlPath string) string {
	name, ok := devName(urlPath)
	if !ok {
		return urlPath
	}
	hash, err := s.hash(name)
	if err != nil {
		return urlPath
	}
	return urlPath + "?v=" + hash
}

// ServeHTTP serves the file at r.URL.Path. Only a request carrying the file's
// current hash may cache it for good; any other is told to revalidate, so an
// instance that is mid-upgrade can't pin the wrong bytes under a new URL.
func (s *hashedSet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

// ServeHTTP serves the file as it is on disk right now. It never gzips: the
// release path memoizes the compressed body per file in a sync.Once, which
// would pin one edit for the rest of the session.
//
// no-cache revalidates on every request but still lets the browser keep the
// bytes, so an unchanged file answers 304 instead of being re-sent. Every save
// refreshes the page, and no-store made each one re-download the whole tree —
// including 5.3 MB of mermaid to re-parse.
func (s *devSet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	h := w.Header()
	h.Set("Cache-Control", "no-cache")
	if hash, err := s.hash(name); err == nil {
		h.Set("ETag", `"`+hash+`"`)
	}
	http.ServeFileFS(w, r, s.fsys, name)
}

// gzipped returns f compressed, or nil when gzip doesn't shrink it. It runs on
// first request rather than in New, so startup doesn't wait to compress mermaid.
func (s *hashedSet) gzipped(f *file) []byte {
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

// A Set is an interface, so the atomic holds a struct wrapping it: storing an
// interface value directly would need atomic.Value, which panics on a type
// change between stores.
var defaultSet atomic.Pointer[struct{ Set }]

// SetDefault makes s the set the package-level URL resolves against. A nil s
// clears it, rather than storing a nil interface inside a non-nil wrapper that
// would pass URL's check and then panic.
func SetDefault(s Set) {
	if s == nil {
		defaultSet.Store(nil)
		return
	}
	defaultSet.Store(&struct{ Set }{s})
}

// URL resolves urlPath against the default set, returning it unchanged when
// there is none.
func URL(urlPath string) string {
	if s := defaultSet.Load(); s != nil {
		return s.URL(urlPath)
	}
	return urlPath
}
