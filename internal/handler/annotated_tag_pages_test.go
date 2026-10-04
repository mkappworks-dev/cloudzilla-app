package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCodeBrowserPages_AnnotatedTag(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	// mainPushed is a commit no branch points at, so its hash on the refs page can only come from a tag.
	tagged := r.mainPushed.String()[:7]
	var tagObjects []string
	for _, name := range []string{"v1", "v2"} {
		ref, err := r.git.CreateTag(name, r.mainPushed, &gogit.CreateTagOptions{
			Message: "Release " + name,
			Tagger:  &object.Signature{Name: "Tester", Email: "tester@example.com", When: time.Unix(1700000000, 0)},
		})
		if err != nil {
			t.Fatalf("create annotated tag %s: %v", name, err)
		}
		tagObjects = append(tagObjects, ref.Hash().String()[:7])
	}

	for _, page := range []string{"/tree/v1", "/tree/v1/pushed.txt", "/blob/v1/pushed.txt", "/blame/v1/pushed.txt", "/commits/v1", "/raw/v1/pushed.txt", "/archive/v1.zip"} {
		if rr := requestAPI(api, http.MethodGet, r.path+page, r.owner.token); rr.Code != http.StatusOK {
			t.Errorf("GET %s: status %d", page, rr.Code)
		}
	}

	rr := requestAPI(api, http.MethodGet, r.path+"/refs", r.owner.token)
	assertTagHashes(t, "refs page", rr, tagged, tagObjects)

	req := httptest.NewRequest(http.MethodDelete, "/api/repos/"+r.owner.name+"/"+r.name+"/tags?name=v2", nil)
	req.Header.Set("Authorization", "Bearer "+r.owner.token)
	req.Header.Set("HX-Request", "true")
	rr = httptest.NewRecorder()
	api.ServeHTTP(rr, req)
	assertTagHashes(t, "tags fragment after delete", rr, tagged, tagObjects)
}

func assertTagHashes(t *testing.T, what string, rr *httptest.ResponseRecorder, tagged string, tagObjects []string) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("%s: status %d", what, rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, ">"+tagged+"<") {
		t.Errorf("%s: tagged commit %s not shown", what, tagged)
	}
	for _, h := range tagObjects {
		if strings.Contains(body, h) {
			t.Errorf("%s: shows tag object %s instead of the tagged commit", what, h)
		}
	}
}
