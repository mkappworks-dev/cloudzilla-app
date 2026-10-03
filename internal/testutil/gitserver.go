package testutil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
)

// GitHTTPHandler serves the bare repo at gitDir read-only over smart HTTP at
// any path. A non-empty user requires those basic-auth credentials.
func GitHTTPHandler(t *testing.T, gitDir, user, pass string) http.Handler {
	t.Helper()
	repo, err := gogit.PlainOpen(gitDir)
	if err != nil {
		t.Fatalf("open source repo: %v", err)
	}
	ep, err := transport.NewEndpoint("/")
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	// MapLoader is keyed on ep.String() (e.g. "file:///"), not the input to NewEndpoint.
	srv := server.NewServer(server.MapLoader{ep.String(): repo.Storer})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != pass {
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		sess, err := srv.NewUploadPackSession(ep, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ar, err := sess.AdvertisedReferencesContext(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/info/refs"):
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			pe := pktline.NewEncoder(w)
			_ = pe.Encodef("# service=git-upload-pack\n")
			_ = pe.Flush()
			_ = ar.Encode(w)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-upload-pack"):
			req := packp.NewUploadPackRequest()
			if err := req.Decode(r.Body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			resp, err := sess.UploadPack(r.Context(), req)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_ = resp.Encode(w)
		default:
			http.NotFound(w, r)
		}
	})
}

// ServeGitHTTP serves GitHTTPHandler on a test server and returns a clone URL.
func ServeGitHTTP(t *testing.T, gitDir, user, pass string) string {
	t.Helper()
	srv := httptest.NewServer(GitHTTPHandler(t, gitDir, user, pass))
	t.Cleanup(srv.Close)
	return srv.URL + "/source.git"
}

// SourceRepo is a bare repo for import tests: main → First, develop → Second
// (a child of First), tag v1 → First, HEAD → develop, and refs/pull/1/head →
// Second, which an import must leave behind.
type SourceRepo struct {
	Dir           string
	First, Second plumbing.Hash
}

func SeedSourceRepo(t *testing.T) SourceRepo {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, true)
	if err != nil {
		t.Fatalf("init source repo: %v", err)
	}
	first := WriteCommit(t, repo.Storer, "first")
	second := WriteCommit(t, repo.Storer, "second", first)
	refs := map[plumbing.ReferenceName]plumbing.Hash{
		plumbing.NewBranchReferenceName("main"):    first,
		plumbing.NewBranchReferenceName("develop"): second,
		plumbing.NewTagReferenceName("v1"):         first,
		"refs/pull/1/head":                         second,
	}
	for name, hash := range refs {
		if err := repo.Storer.SetReference(plumbing.NewHashReference(name, hash)); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	head := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("develop"))
	if err := repo.Storer.SetReference(head); err != nil {
		t.Fatalf("set HEAD: %v", err)
	}
	return SourceRepo{Dir: dir, First: first, Second: second}
}
