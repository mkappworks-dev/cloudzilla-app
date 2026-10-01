package store_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func ghostUser(t *testing.T, db *sql.DB) (id int64, username, email string) {
	t.Helper()
	if err := db.QueryRow(`SELECT id, username, email FROM users WHERE id = ghost_user_id()`).Scan(&id, &username, &email); err != nil {
		t.Fatalf("load ghost: %v", err)
	}
	return id, username, email
}

func seedPull(t *testing.T, db *sql.DB) (pullID, repoID int64) {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID = testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	if err := db.QueryRow(
		`INSERT INTO pull_requests (repo_id, number, author_id, title, head_branch) VALUES ($1, 1, $2, 'p', 'p') RETURNING id`,
		repoID, ownerID,
	).Scan(&pullID); err != nil {
		t.Fatalf("seed pull: %v", err)
	}
	return pullID, repoID
}

func TestUserStore_NameAndEmailLookupsSkipTheGhost(t *testing.T) {
	db := openStoreDB(t)
	s := store.NewUserStore(db)
	ctx := context.Background()
	id, username, email := ghostUser(t, db)

	lookups := map[string]func() error{
		"GetByUsername":      func() error { _, err := s.GetByUsername(ctx, username); return err },
		"GetByEmail":         func() error { _, err := s.GetByEmail(ctx, email); return err },
		"GetByEmailWithRole": func() error { _, err := s.GetByEmailWithRole(ctx, email); return err },
		"GetByEmailWithTOTP": func() error { _, err := s.GetByEmailWithTOTP(ctx, email); return err },
	}
	for name, lookup := range lookups {
		if err := lookup(); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("%s found the ghost: err = %v", name, err)
		}
	}
	if users, err := s.GetManyByUsernames(ctx, []string{username}); err != nil || len(users) != 0 {
		t.Errorf("GetManyByUsernames = %v, %v; want no users", users, err)
	}
	if hits, err := store.NewSearchStore(db).SearchUsers(ctx, username, 10); err != nil {
		t.Fatalf("SearchUsers: %v", err)
	} else if slices.ContainsFunc(hits, func(u model.User) bool { return u.ID == id }) {
		t.Error("SearchUsers listed the ghost")
	}
	if u, err := s.GetByID(ctx, id); err != nil || u.Username != username {
		t.Errorf("GetByID(ghost) = %v, %v; content it authored must still resolve", u, err)
	}
}

func TestUserStore_CountAccounts_SkipsTheGhost(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)

	n, err := store.NewUserStore(db).CountAccounts(context.Background())

	if err != nil || n != 0 {
		t.Errorf("CountAccounts on a fresh install = %d, %v; want 0 so setup still runs", n, err)
	}
}

// A column that references users(id) with no ON DELETE action blocks every
// account delete that leaves a row in it, unless the delete reassigns it.
func TestGhostReassignments_CoverEveryUserFKWithoutDeleteAction(t *testing.T) {
	db := openStoreDB(t)
	rows, err := db.Query(`
		SELECT c.conrelid::regclass::text || '.' || a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
		WHERE c.contype = 'f' AND c.confrelid = 'users'::regclass AND c.confdeltype IN ('a', 'r')
		ORDER BY 1`)
	if err != nil {
		t.Fatalf("list FKs: %v", err)
	}
	defer rows.Close()
	var blocking []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			t.Fatalf("scan FK: %v", err)
		}
		blocking = append(blocking, col)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list FKs: %v", err)
	}
	if len(blocking) == 0 {
		t.Fatal("found no blocking FKs; the catalog query is wrong")
	}

	reassigned := store.GhostReassignedColumns()
	for _, col := range blocking {
		if !slices.Contains(reassigned, col) {
			t.Errorf("%s references users(id) with no ON DELETE action: add it to ghostReassignments or give it one", col)
		}
	}
	for _, col := range reassigned {
		if !slices.Contains(blocking, col) {
			t.Errorf("ghostReassignments lists %s, which no longer blocks a user delete", col)
		}
	}
}

func TestPullReviewStore_GhostReviewsDoNotGateMerging(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	pullID, repoID := seedPull(t, db)
	for _, state := range []string{"changes_requested", "approved", "approved"} {
		testutil.Exec(t, db,
			`INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state) VALUES ($1, $2, ghost_user_id(), 'ghost', $3)`,
			pullID, repoID, state)
	}
	reviews := store.NewPullReviewStore(db)

	if blocked, err := reviews.HasChangesRequested(ctx, pullID); err != nil || blocked {
		t.Errorf("HasChangesRequested = %v, %v; the ghost can never withdraw a request for changes", blocked, err)
	}
	if n, err := reviews.CountApprovals(ctx, pullID); err != nil || n != 0 {
		t.Errorf("CountApprovals = %d, %v; want 0, a deleted account's approval does not count", n, err)
	}
}

func TestPullReviewStore_KeepsOneReviewPerLiveReviewer(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	pullID, repoID := seedPull(t, db)
	reviewerID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	reviews := store.NewPullReviewStore(db)

	if err := reviews.RequestReview(ctx, pullID, repoID, reviewerID, "r"); err != nil {
		t.Fatalf("RequestReview: %v", err)
	}
	if err := reviews.RequestReview(ctx, pullID, repoID, reviewerID, "r"); err != nil {
		t.Fatalf("RequestReview again: %v", err)
	}
	for _, state := range []model.PRReviewState{model.PRReviewChangesRequested, model.PRReviewApproved} {
		r := &model.PullReview{PullID: pullID, RepoID: repoID, AuthorID: reviewerID, AuthorName: "r", State: state}
		if err := reviews.Upsert(ctx, r); err != nil {
			t.Fatalf("Upsert %s: %v", state, err)
		}
	}

	got, err := reviews.ListByPull(ctx, pullID)
	if err != nil {
		t.Fatalf("ListByPull: %v", err)
	}
	if len(got) != 1 || got[0].State != model.PRReviewApproved {
		t.Errorf("reviews = %+v; want the one review, now approved", got)
	}
}
