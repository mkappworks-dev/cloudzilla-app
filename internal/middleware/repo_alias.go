package middleware

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// AliasResolver is the slice of RepoService the alias redirect needs.
type AliasResolver interface {
	ResolveAlias(ctx context.Context, owner, name string) (*model.Repository, error)
	CanRead(ctx context.Context, repo *model.Repository, userID *int64) bool
}

// RepoAlias redirects a repo's former path to its current one. It acts only on a 404 from a repo-shaped path, so a
// live path pays nothing, and the 404 is held back and replayed byte for byte unless the viewer can read the target:
// an alias must look like a missing repo to anyone else. auth is an optional-auth middleware, run only on that miss
// because the viewer is not known this early in the chain.
func RepoAlias(res AliasResolver, auth func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			owner, name, rest, ok := splitRepoPath(r.URL)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			cw := &missHold{ResponseWriter: w}
			next.ServeHTTP(cw, r)
			if !cw.held {
				return
			}
			redirected := false
			auth(http.HandlerFunc(func(_ http.ResponseWriter, ar *http.Request) {
				redirected = redirectToAlias(w, ar, res, owner, name, rest)
			})).ServeHTTP(discardResponse{}, r)
			if !redirected {
				cw.release()
			}
		})
	}
}

// redirectToAlias answers with the redirect and reports true when owner/name is an alias of a repo the viewer can read.
func redirectToAlias(w http.ResponseWriter, r *http.Request, res AliasResolver, owner, name, rest string) bool {
	repo, err := res.ResolveAlias(r.Context(), owner, name)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("resolve repo alias", "error", err, "owner", owner, "name", name)
		}
		return false
	}
	var viewer *int64
	if c, ok := ClaimsFromContext(r.Context()); ok {
		viewer = &c.UserID
	}
	if !res.CanRead(r.Context(), repo, viewer) {
		return false
	}

	to := aliasTarget(r.URL, repo, rest)
	h := w.Header()
	h.Del("Content-Type")
	h.Del("Content-Length")
	h.Set("Location", to)
	switch {
	case r.Method == http.MethodGet && r.Header.Get("HX-Request") == "true":
		h.Set("HX-Redirect", to)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		w.WriteHeader(http.StatusMovedPermanently)
	default:
		w.WriteHeader(http.StatusPermanentRedirect)
	}
	return true
}

func aliasTarget(u *url.URL, repo *model.Repository, rest string) string {
	prefix := ""
	if strings.HasPrefix(u.EscapedPath(), "/api/repos/") {
		prefix = "/api/repos"
	}
	to := prefix + "/" + url.PathEscape(repo.OwnerName) + "/" + url.PathEscape(repo.Name) + rest
	if u.RawQuery != "" {
		to += "?" + u.RawQuery
	}
	return to
}

// splitRepoPath reads owner and repo from /{owner}/{repo}/... or /api/repos/{owner}/{repo}/...; rest is the raw
// (still escaped) remainder. Git transport paths are left to their own handlers.
func splitRepoPath(u *url.URL) (owner, name, rest string, ok bool) {
	segs := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	base := 0
	switch {
	case segs[0] == "api" && len(segs) > 1 && segs[1] == "repos":
		base = 2
	case segs[0] == "api" || segs[0] == "static" || segs[0] == "avatars":
		return "", "", "", false
	}
	if len(segs) < base+2 {
		return "", "", "", false
	}
	if tail := segs[base+2:]; len(tail) > 0 {
		rest = "/" + strings.Join(tail, "/")
	}
	segs = segs[base:]
	if strings.HasPrefix(rest, "/info/refs") || strings.HasPrefix(rest, "/git-") {
		return "", "", "", false
	}
	var err error
	if owner, err = url.PathUnescape(segs[0]); err != nil {
		return "", "", "", false
	}
	if name, err = url.PathUnescape(segs[1]); err != nil {
		return "", "", "", false
	}
	return owner, name, rest, true
}

// missHold withholds a 404 and its body so the middleware can decide between replaying it and redirecting.
type missHold struct {
	http.ResponseWriter
	held bool
	body bytes.Buffer
}

func (m *missHold) WriteHeader(code int) {
	if code == http.StatusNotFound && !m.held {
		m.held = true
		return
	}
	m.ResponseWriter.WriteHeader(code)
}

func (m *missHold) Write(b []byte) (int, error) {
	if m.held {
		return m.body.Write(b)
	}
	return m.ResponseWriter.Write(b)
}

func (m *missHold) Flush() {
	if f, ok := m.ResponseWriter.(http.Flusher); ok && !m.held {
		f.Flush()
	}
}

func (m *missHold) Unwrap() http.ResponseWriter { return m.ResponseWriter }

func (m *missHold) release() {
	m.ResponseWriter.WriteHeader(http.StatusNotFound)
	_, _ = m.ResponseWriter.Write(m.body.Bytes())
}

type discardResponse struct{}

func (discardResponse) Header() http.Header         { return http.Header{} }
func (discardResponse) Write(b []byte) (int, error) { return len(b), nil }
func (discardResponse) WriteHeader(int)             {}
