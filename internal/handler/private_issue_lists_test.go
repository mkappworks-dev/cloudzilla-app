package handler_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// A writer's private issue, pinned and in milestone #1, on a public repo.
func TestPrivateIssue_PinnedStripAndMilestone(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, false)
	writer := seedSignedInUser(t, db)
	testutil.Exec(t, db, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'writer')`, writer.id, repo.id)
	title := "privatepinned_" + testutil.UniqueSuffix(t)
	testutil.Exec(t, db, `INSERT INTO milestones (repo_id, number, title) VALUES ($1, 1, 'v1')`, repo.id)
	testutil.Exec(t, db,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility, is_pinned, milestone_id)
		 SELECT $1, 1, $2, $3, '', 'open', 'private', TRUE, id FROM milestones WHERE repo_id = $1`,
		repo.id, writer.id, title)

	cases := []struct {
		viewer, token string
		want          bool
	}{
		{"owner", repo.owner.token, true},
		{"stranger", seedSignedInUser(t, db).token, false},
		{"anonymous", "", false},
	}
	for _, c := range cases {
		t.Run(c.viewer, func(t *testing.T) {
			// The closed tab lists no open issue, so only the pinned strip can
			// show this one.
			for _, page := range []string{"/issues?state=closed", "/milestones/1"} {
				rr := requestAPI(api, http.MethodGet, repo.path+page, c.token)
				if rr.Code != http.StatusOK {
					t.Fatalf("GET %s: status %d", page, rr.Code)
				}
				if got := strings.Contains(rr.Body.String(), title); got != c.want {
					t.Errorf("GET %s shows the private issue: %v, want %v", page, got, c.want)
				}
			}

			wantOpen := 0
			if c.want {
				wantOpen = 1
			}
			var one model.Milestone
			decodeAPI(t, api, "/api/repos"+repo.path+"/milestones/1", c.token, &one)
			var all []model.Milestone
			decodeAPI(t, api, "/api/repos"+repo.path+"/milestones", c.token, &all)
			if one.OpenCount != wantOpen || len(all) != 1 || all[0].OpenCount != wantOpen {
				t.Errorf("milestone API open_count: get %d, list %+v; want %d", one.OpenCount, all, wantOpen)
			}
		})
	}
}

func decodeAPI(t *testing.T, h http.Handler, path, token string, v any) {
	t.Helper()
	rr := requestAPI(h, http.MethodGet, path, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d %s", path, rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("GET %s: decode: %v", path, err)
	}
}
