package router_test

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newArchiveRepo(t *testing.T) (http.Handler, string) {
	t.Helper()
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	if _, err := svc.Repo.Create(context.Background(), ownerID, owner, "src", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	return h, "/" + owner + "/src/archive/"
}

func TestDownloadArchive_UnknownRefIsNotFound(t *testing.T) {
	h, archive := newArchiveRepo(t)

	rr := serve(h, httptest.NewRequest(http.MethodGet, archive+"no-such-ref", nil))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d %q (%d bytes), want 404", rr.Code, rr.Header().Get("Content-Type"), rr.Body.Len())
	}
	if cd := rr.Header().Get("Content-Disposition"); cd != "" {
		t.Errorf("Content-Disposition = %q on a 404, want none", cd)
	}
}

func TestDownloadArchive_BranchStreamsAZip(t *testing.T) {
	h, archive := newArchiveRepo(t)

	rr := serve(h, httptest.NewRequest(http.MethodGet, archive+"main.zip", nil))

	if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("got %d %q, want 200 application/zip", rr.Code, rr.Header().Get("Content-Type"))
	}
	zr, err := zip.NewReader(bytes.NewReader(rr.Body.Bytes()), int64(rr.Body.Len()))
	if err != nil {
		t.Fatalf("read zip (%d bytes): %v", rr.Body.Len(), err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if len(names) != 1 || names[0] != "src-main/README.md" {
		t.Errorf("zip entries = %v, want [src-main/README.md]", names)
	}
}
