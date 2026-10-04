package handler_test

// Integration tests: the raw file route. All tests require TEST_DATABASE_DSN
// and skip otherwise.

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func commitOnMain(t *testing.T, reposRoot string, r raceRepo, path string, content []byte) {
	t.Helper()
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	if err := code.CommitFile(r.owner.name, r.name, "main", path, content, raceAuthor, "Add "+path); err != nil {
		t.Fatalf("commit %s: %v", path, err)
	}
}

func TestRawFile_ServesTheFileBytes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	// Past the binary sniff window, so the whole body must survive the sniff.
	long := []byte(strings.Repeat("line of text\n", 2000))
	binary := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	commitOnMain(t, reposRoot, r, "docs/long.txt", long)
	commitOnMain(t, reposRoot, r, "logo.png", binary)

	for _, tt := range []struct {
		path        string
		want        []byte
		contentType string
	}{
		{"a.txt", []byte("one\ntwo\n"), "text/plain; charset=utf-8"},
		{"docs/long.txt", long, "text/plain; charset=utf-8"},
		{"logo.png", binary, "application/octet-stream"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			rr := requestAPI(api, http.MethodGet, r.path+"/raw/main/"+tt.path, "")

			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", rr.Code)
			}
			if !bytes.Equal(rr.Body.Bytes(), tt.want) {
				t.Errorf("body: want %d bytes, got %d", len(tt.want), rr.Body.Len())
			}
			if got := rr.Header().Get("Content-Type"); got != tt.contentType {
				t.Errorf("Content-Type: want %q, got %q", tt.contentType, got)
			}
		})
	}
}

func TestBlobPage_RawButtonServesTheFile(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	rawURL := r.path + "/raw/main/a.txt"

	rr := requestAPI(api, http.MethodGet, r.path+"/blob/main/a.txt", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("blob page: want 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `href="`+rawURL+`"`) {
		t.Errorf("blob page: no Raw button linking %s", rawURL)
	}
	if rr := requestAPI(api, http.MethodGet, rawURL, ""); rr.Code != http.StatusOK || rr.Body.String() != "one\ntwo\n" {
		t.Errorf("GET %s: want 200 %q, got %d", rawURL, "one\ntwo\n", rr.Code)
	}
}

// An uploaded .html or .svg is served from the forge's own origin, so it must
// never render as a document that can run script.
func TestRawFile_HTMLFile_ServedInertAsText(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	commitOnMain(t, reposRoot, r, "evil.html", []byte("<script>alert(document.cookie)</script>\n"))

	rr := requestAPI(api, http.MethodGet, r.path+"/raw/main/evil.html", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	for header, want := range map[string]string{
		"Content-Type":            "text/plain; charset=utf-8",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; sandbox",
	} {
		if got := rr.Header().Get(header); got != want {
			t.Errorf("%s: want %q, got %q", header, want, got)
		}
	}
}

func TestRawFile_FileOverTheCap_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	commitOnMain(t, reposRoot, r, "huge.bin", make([]byte, handler.MaxRawBlobBytes+1))

	rr := requestAPI(api, http.MethodGet, r.path+"/raw/main/huge.bin", "")

	if rr.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "clone the repository") {
		t.Errorf("want the clone hint, got a %d-byte body", rr.Body.Len())
	}
}

func TestRawFile_MissingFileOrRef_NotFound(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)

	for _, page := range []string{"/raw/main/nope.txt", "/raw/main/", "/raw/no-such-branch/a.txt"} {
		if rr := requestAPI(api, http.MethodGet, r.path+page, ""); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s: want 404, got %d", page, rr.Code)
		}
	}
}

// A 403 for a private repo against a 404 for a missing one would confirm that
// the private repo exists, so the two responses must be identical.
func TestRawFile_PrivateRepoNonReader_LooksLikeMissingRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	testutil.Exec(t, db, `UPDATE repositories SET private = true WHERE id = $1`, r.id)
	missingPath := "/nobody_" + testutil.UniqueSuffix(t) + "/norepo"

	for name, token := range map[string]string{"anonymous": "", "stranger": seedSignedInUser(t, db).token} {
		t.Run(name, func(t *testing.T) {
			private := requestAPI(api, http.MethodGet, r.path+"/raw/main/a.txt", token)
			missing := requestAPI(api, http.MethodGet, missingPath+"/raw/main/a.txt", token)

			if private.Code != http.StatusNotFound {
				t.Errorf("want 404, got %d", private.Code)
			}
			if private.Code != missing.Code || private.Body.String() != missing.Body.String() {
				t.Errorf("private repo response (%d) must match a missing repo's (%d)", private.Code, missing.Code)
			}
		})
	}
}

func TestRawFile_PrivateRepoOwner_ServesTheFile(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	testutil.Exec(t, db, `UPDATE repositories SET private = true WHERE id = $1`, r.id)

	rr := requestAPI(api, http.MethodGet, r.path+"/raw/main/a.txt", r.owner.token)

	if rr.Code != http.StatusOK || rr.Body.String() != "one\ntwo\n" {
		t.Errorf("want 200 %q, got %d", "one\ntwo\n", rr.Code)
	}
}
