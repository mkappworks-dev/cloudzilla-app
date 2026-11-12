package handler_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var forkOwnerOptionRE = regexp.MustCompile(`<li id="fork-owner-btn-opt-\d+"[^>]*data-value="([^"]+)"[^>]*>`)

func getPage(t *testing.T, router http.Handler, token, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d: %s", path, rr.Code, rr.Body)
	}
	return rr.Body.String()
}

func TestPageForkRepo_UserFirstAndEnabledWhenNotSourceOwner(t *testing.T) {
	db := testutil.OpenTestDB(t)
	router, _ := newOrgTestRouter(t, db)
	repo := seedOwnedRepo(t, db, false)
	forker := seedSignedInUser(t, db)
	seedOrgOwnedBy(t, db, "forkorg-"+testutil.UniqueSuffix(t), forker.id)

	body := getPage(t, router, forker.token, repo.path+"/fork")

	opts := forkOwnerOptionRE.FindAllStringSubmatch(body, -1)
	if len(opts) != 2 || opts[0][1] != forker.name {
		t.Fatalf("want the User first of 2 owners, got %v", opts)
	}
	if strings.Contains(body, "Owns this repository") {
		t.Error("User does not own the source, so no option should be disabled")
	}
	assertContains(t, body, `name="owner" value="`+forker.name+`"`)
}

func TestPageForkRepo_SourceOwnerStaysListedDisabledAndOrgIsDefault(t *testing.T) {
	db := testutil.OpenTestDB(t)
	router, _ := newOrgTestRouter(t, db)
	repo := seedOwnedRepo(t, db, false)
	orgName := "forkorg-" + testutil.UniqueSuffix(t)
	seedOrgOwnedBy(t, db, orgName, repo.owner.id)

	body := getPage(t, router, repo.owner.token, repo.path+"/fork")

	opts := forkOwnerOptionRE.FindAllStringSubmatch(body, -1)
	if len(opts) != 2 || opts[0][1] != repo.owner.name || opts[1][1] != orgName {
		t.Fatalf("want [User, org], got %v", opts)
	}
	if !strings.Contains(opts[0][0], `aria-disabled="true"`) || strings.Contains(opts[1][0], "aria-disabled") {
		t.Errorf("want only the User disabled, got %q and %q", opts[0][0], opts[1][0])
	}
	assertContains(t, body, "Owns this repository")
	assertContains(t, body, `name="owner" value="`+orgName+`"`)
}

func TestPageForkRepo_SourceOwnerWithoutOrgShowsEmptyState(t *testing.T) {
	db := testutil.OpenTestDB(t)
	router, _ := newOrgTestRouter(t, db)
	repo := seedOwnedRepo(t, db, false)

	body := getPage(t, router, repo.owner.token, repo.path+"/fork")

	assertContains(t, body, "fork your own repository into your own account.")
	if strings.Contains(body, `id="fork-form"`) {
		t.Error("no enabled owner, so the fork form should not render")
	}
}
