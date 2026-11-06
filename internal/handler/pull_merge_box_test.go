package handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestPullDetail_MergeBoxActionsNeedWriteAccess(t *testing.T) {
	const closeDialog = "document.getElementById('close-pull-dialog').showModal()"
	const readOnlyLine = "Only collaborators with write access can merge or close this pull request."
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	app := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	seedOpenPull(t, db, r.seededRepo)

	tests := []struct {
		name    string
		token   string
		wantBox bool
	}{
		{"writer", r.owner.token, true},
		{"reader", seedSignedInUser(t, db).token, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := requestAPI(app, http.MethodGet, r.path+"/pulls/1", tc.token)
			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", rr.Code)
			}
			body := rr.Body.String()
			assertContains(t, body, "Ready to merge")
			for _, action := range []string{"merge_strategy", closeDialog} {
				if got := strings.Contains(body, action); got != tc.wantBox {
					t.Errorf("%q on the page = %v, want %v", action, got, tc.wantBox)
				}
			}
			if got := strings.Contains(body, readOnlyLine); got == tc.wantBox {
				t.Errorf("read-only line on the page = %v, want %v", got, !tc.wantBox)
			}
		})
	}
}
