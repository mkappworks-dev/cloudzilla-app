package handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCodeBrowserPages_RefPickerListsOtherBranches(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)

	for _, tt := range []struct{ page, want string }{
		{"/tree/main", "/tree/feature"},
		{"/tree/main/a.txt", "/tree/feature/a.txt"},
		{"/blob/main/a.txt", "/blob/feature/a.txt"},
		{"/blame/main/a.txt", "/blame/feature/a.txt"},
	} {
		rr := requestAPI(api, http.MethodGet, r.path+tt.page, r.owner.token)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d", tt.page, rr.Code)
		}
		if want := `href="` + r.path + tt.want + `"`; !strings.Contains(rr.Body.String(), want) {
			t.Errorf("GET %s: no ref picker item %s", tt.page, want)
		}
	}
}
