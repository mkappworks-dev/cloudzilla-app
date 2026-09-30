package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// Org repos have no owner_id; every query that lists repos must still scan them.
func TestRepoLists_IncludeOrgReposWithoutOwner(t *testing.T) {
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, "orgscan_"+suffix)
	orgName := "orgscan_" + suffix
	var orgID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO organizations (name) VALUES ($1) RETURNING id`, orgName).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, db, orgID)
	var repoID, deletedID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_name, org_id, created_by, name, description, is_template) VALUES ($1, $2, $3, 'live', 'zebracorn', TRUE) RETURNING id`,
		orgName, orgID, userID).Scan(&repoID); err != nil {
		t.Fatalf("insert org repo: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_name, org_id, name, description, deleted_at, deleted_by) VALUES ($1, $2, 'binned', 'zebracorn', NOW(), $3) RETURNING id`,
		orgName, orgID, userID).Scan(&deletedID); err != nil {
		t.Fatalf("insert deleted org repo: %v", err)
	}
	var forkID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_name, org_id, name, is_fork, fork_of_id) VALUES ($1, $2, 'forked', TRUE, $3) RETURNING id`,
		orgName, orgID, repoID).Scan(&forkID); err != nil {
		t.Fatalf("insert org fork: %v", err)
	}
	testutil.Exec(t, db, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'reader')`, userID, repoID)
	testutil.Exec(t, db, `INSERT INTO stars (user_id, repo_id) VALUES ($1, $2)`, userID, repoID)
	topic := "orgscan-" + time.Now().Format("150405.000000")
	if err := store.NewTopicStore(db).SetTopics(ctx, repoID, []string{topic}); err != nil {
		t.Fatalf("set topic: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM topics WHERE name = $1`, topic) })

	repos := store.NewRepoStore(db)
	want := func(what string, got []model.Repository, err error, id int64) {
		t.Helper()
		if err != nil {
			t.Errorf("%s: %v", what, err)
			return
		}
		for _, r := range got {
			if r.ID == id {
				if r.OwnerID != 0 || r.OrgID != orgID {
					t.Errorf("%s: owner_id %d, org_id %d; want 0 and %d", what, r.OwnerID, r.OrgID, orgID)
				}
				return
			}
		}
		t.Errorf("%s: org repo %d missing", what, id)
	}
	withStats := func(rs []model.RepositoryWithStats, err error) ([]model.Repository, error) {
		out := make([]model.Repository, len(rs))
		for i, r := range rs {
			out[i] = r.Repository
		}
		return out, err
	}
	one := func(r *model.Repository, err error) ([]model.Repository, error) {
		if err != nil {
			return nil, err
		}
		return []model.Repository{*r}, nil
	}

	got, err := one(repos.GetByID(ctx, repoID))
	want("GetByID", got, err, repoID)
	got, err = one(repos.GetByOwnerName(ctx, orgName, "live"))
	want("GetByOwnerName", got, err, repoID)
	got, err = repos.GetByOrgID(ctx, orgID)
	want("GetByOrgID", got, err, repoID)
	got, err = repos.GetByOwnerNameList(ctx, orgName)
	want("GetByOwnerNameList", got, err, repoID)
	got, err = one(repos.GetDeletedByID(ctx, deletedID))
	want("GetDeletedByID", got, err, deletedID)
	got, err = one(repos.GetDeletedByOwnerAndName(ctx, orgName, "binned"))
	want("GetDeletedByOwnerAndName", got, err, deletedID)
	got, err = repos.ListForks(ctx, repoID)
	want("ListForks", got, err, forkID)
	got, err = repos.ListTemplates(ctx)
	want("ListTemplates", got, err, repoID)
	got, err = repos.List(ctx)
	want("List", got, err, repoID)
	got, err = repos.ListAll(ctx)
	want("ListAll", got, err, repoID)
	got, err = repos.ListForUser(ctx, userID, "collaborator")
	want("ListForUser collaborator", got, err, repoID)
	got, err = repos.ListForUser(ctx, userID, "all")
	want("ListForUser all", got, err, repoID)
	got, err = store.NewSearchStore(db).SearchRepos(ctx, "zebracorn", &userID, 1000)
	want("SearchRepos", got, err, repoID)
	for _, r := range got {
		if r.ID == deletedID {
			t.Error("SearchRepos returned a soft-deleted repo")
		}
	}
	got, err = store.NewStarStore(db).ListByUser(ctx, userID)
	want("StarStore.ListByUser", got, err, repoID)
	got, err = withStats(store.NewExploreStore(db).NewestRepos(ctx, 1000))
	want("NewestRepos", got, err, repoID)
	got, err = withStats(store.NewTopicStore(db).ListReposByTopicWithStats(ctx, topic, 1, 10, ""))
	want("ListReposByTopicWithStats", got, err, repoID)

	err = repos.CreateWithOwnerName(ctx, &model.Repository{OwnerName: orgName, OrgID: orgID, Name: "live", DefaultBranch: "main"})
	if !errors.Is(err, store.ErrRepoNameInUse) {
		t.Errorf("second live org repo named live: want ErrRepoNameInUse, got %v", err)
	}
	// A soft-deleted org repo does not hold its name.
	if err := repos.CreateWithOwnerName(ctx, &model.Repository{OwnerName: orgName, OrgID: orgID, Name: "binned", DefaultBranch: "main"}); err != nil {
		t.Errorf("reusing a soft-deleted org repo's name: %v", err)
	}

	owned, err := repos.ListForUser(ctx, userID, "owned")
	if err != nil {
		t.Fatalf("ListForUser owned: %v", err)
	}
	for _, r := range owned {
		if r.ID == repoID {
			t.Error(`an org repo is listed as "owned" by its creator`)
		}
	}
}
