package handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// A writer's private issue and a public one, both open and linked to PR #1, on
// a public personal repo and a public org repo. Neither repo's owners hold a
// permissions row.
func TestPrivateIssue_LinkedToPullAndIssuesTabCount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	author := seedSignedInUser(t, db)
	reader := seedSignedInUser(t, db)
	stranger := seedSignedInUser(t, db)
	personal := seedOwnedRepo(t, db, false)

	orgOwner := seedSignedInUser(t, db)
	orgMember := seedSignedInUser(t, db)
	orgName := "testorg_" + testutil.UniqueSuffix(t)
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, orgName).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, db, orgID)
	testutil.Exec(t, db,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'member')`,
		orgID, orgOwner.id, orgMember.id)
	var orgRepoID int64
	if err := db.QueryRow(
		`INSERT INTO repositories (owner_name, org_id, created_by, name) VALUES ($1, $2, $3, 'vault') RETURNING id`,
		orgName, orgID, orgMember.id,
	).Scan(&orgRepoID); err != nil {
		t.Fatalf("insert org repo: %v", err)
	}
	orgPath := "/" + orgName + "/vault"

	publicTitle := "publiclinked_" + testutil.UniqueSuffix(t)
	privateTitle := "privatelinked_" + testutil.UniqueSuffix(t)
	for _, repoID := range []int64{personal.id, orgRepoID} {
		testutil.Exec(t, db,
			`INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $3, 'writer'), ($2, $3, 'reader')`,
			author.id, reader.id, repoID)
		testutil.Exec(t, db,
			`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility)
			 VALUES ($1, 1, $2, $3, '', 'open', 'public'), ($1, 2, $2, $4, '', 'open', 'private')`,
			repoID, author.id, publicTitle, privateTitle)
		testutil.Exec(t, db,
			`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
			 VALUES ($1, 1, $2, 'change', 'open', 'feature', 'main')`,
			repoID, author.id)
		testutil.Exec(t, db,
			`INSERT INTO pull_issue_links (pull_id, issue_id)
			 SELECT p.id, i.id FROM pull_requests p JOIN issues i ON i.repo_id = p.repo_id WHERE p.repo_id = $1`,
			repoID)
	}

	cases := []struct {
		repo, path, viewer, token string
		want                      bool
	}{
		{"personal", personal.path, "author", author.token, true},
		{"personal", personal.path, "owner", personal.owner.token, true},
		{"personal", personal.path, "reader", reader.token, false},
		{"personal", personal.path, "stranger", stranger.token, false},
		{"personal", personal.path, "anonymous", "", false},
		{"org", orgPath, "org owner", orgOwner.token, true},
		{"org", orgPath, "org member", orgMember.token, false},
		{"org", orgPath, "reader", reader.token, false},
	}
	for _, c := range cases {
		t.Run(c.repo+"/"+c.viewer, func(t *testing.T) {
			rr := requestAPI(api, http.MethodGet, c.path+"/pulls/1", c.token)
			if rr.Code != http.StatusOK {
				t.Fatalf("GET pull: status %d", rr.Code)
			}
			body := rr.Body.String()
			if !strings.Contains(body, publicTitle) {
				t.Fatal("the public linked issue is missing, so the linked-issues sidebar did not render")
			}
			if got := strings.Contains(body, privateTitle); got != c.want {
				t.Errorf("linked issues show the private issue: %v, want %v", got, c.want)
			}
			wantCount := `aria-label="1 Issues"`
			if c.want {
				wantCount = `aria-label="2 Issues"`
			}
			if !strings.Contains(body, wantCount) {
				t.Errorf("Issues tab: want %s", wantCount)
			}
		})
	}
}
