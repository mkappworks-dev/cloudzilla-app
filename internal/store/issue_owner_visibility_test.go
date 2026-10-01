package store_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// A repo's owners hold no permissions row: RepoService.IsOwner reads owner_id,
// or the org's owners for an org repo. Seeding one here would hide that.
func TestPrivateIssues_VisibleToRepoOwners(t *testing.T) {
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	word := "ownvis" + strings.ReplaceAll(suffix, "_", "x")

	ownerName := "ownvisowner_" + suffix
	ownerID := testutil.SeedUser(t, db, ownerName)
	orgOwnerID := testutil.SeedUser(t, db, "ownvisorgowner_"+suffix)
	creatorID := testutil.SeedUser(t, db, "ownviscreator_"+suffix)
	writerID := testutil.SeedUser(t, db, "ownviswriter_"+suffix)

	var personal int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, $2, $3) RETURNING id`,
		ownerID, ownerName, "personal_"+suffix,
	).Scan(&personal); err != nil {
		t.Fatalf("insert personal repo: %v", err)
	}

	orgName := "ownvisorg_" + suffix
	var orgID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO organizations (name) VALUES ($1) RETURNING id`, orgName).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, db, orgID)
	testutil.Exec(t, db,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'member')`,
		orgID, orgOwnerID, creatorID)
	var orgRepo int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_name, org_id, created_by, name) VALUES ($1, $2, $3, $4) RETURNING id`,
		orgName, orgID, creatorID, "orgrepo_"+suffix,
	).Scan(&orgRepo); err != nil {
		t.Fatalf("insert org repo: %v", err)
	}

	for _, repoID := range []int64{personal, orgRepo} {
		testutil.Exec(t, db, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'writer')`, writerID, repoID)
		testutil.Exec(t, db,
			`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility) VALUES ($1, 1, $2, 'private issue', $3, 'open', 'private')`,
			repoID, writerID, word)
	}

	cases := []struct {
		repo     string
		repoID   int64
		viewer   string
		viewerID int64
		want     bool
	}{
		{"personal", personal, "owner", ownerID, true},
		{"personal", personal, "author", writerID, true},
		{"personal", personal, "org owner", orgOwnerID, false},
		{"org", orgRepo, "org owner", orgOwnerID, true},
		{"org", orgRepo, "author", writerID, true},
		{"org", orgRepo, "creator", creatorID, false},
		{"org", orgRepo, "personal owner", ownerID, false},
	}
	issues := store.NewIssueStore(db)
	search := store.NewSearchStore(db)
	for _, c := range cases {
		t.Run(c.repo+"/"+c.viewer, func(t *testing.T) {
			_, err := issues.GetByNumber(ctx, c.repoID, 1, &c.viewerID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("GetByNumber: %v", err)
			}
			if got := err == nil; got != c.want {
				t.Errorf("GetByNumber found it: %v, want %v", got, c.want)
			}

			listed, err := issues.ListByRepo(ctx, c.repoID, nil, &c.viewerID, 1, 50)
			if err != nil {
				t.Fatalf("ListByRepo: %v", err)
			}
			if got := len(listed) == 1; got != c.want {
				t.Errorf("ListByRepo listed it: %v, want %v", got, c.want)
			}

			hits, err := search.SearchIssues(ctx, word, &c.viewerID, 100)
			if err != nil {
				t.Fatalf("SearchIssues: %v", err)
			}
			inRepo := func(i model.Issue) bool { return i.RepoID == c.repoID }
			if got := slices.ContainsFunc(hits, inRepo); got != c.want {
				t.Errorf("SearchIssues found it: %v, want %v", got, c.want)
			}
		})
	}
}
