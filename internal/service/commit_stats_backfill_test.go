package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type backfillWorld struct {
	db     *sql.DB
	stats  *CommitStatsService
	store  *store.CommitStatsStore
	userID int64
	email  string
	now    time.Time
}

func newBackfillWorld(t *testing.T) *backfillWorld {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	cs := store.NewCommitStatsStore(db)
	return &backfillWorld{
		db:     db,
		stats:  NewCommitStatsService(cs, store.NewUserStore(db)),
		store:  cs,
		userID: testutil.SeedUser(t, db, suffix),
		email:  "testuser_" + suffix + "@test.invalid",
		now:    time.Now().UTC(),
	}
}

// repo seeds a repository row for owner/name and a bare git repo on disk with a commit per entry in daysAgo, all authored by the world's user.
func (w *backfillWorld) repo(t *testing.T, owner, name string, daysAgo ...int) (model.Repository, *CodeService) {
	t.Helper()
	times := make([]time.Time, len(daysAgo))
	for i, d := range daysAgo {
		times[i] = w.now.AddDate(0, 0, -d)
	}
	code := newTestRepoWithCommitsBy(t, owner, name, w.email, times)
	suffix := testutil.UniqueSuffix(t)
	id := testutil.SeedRepo(t, w.db, w.userID, owner, suffix)
	return model.Repository{ID: id, OwnerName: owner, Name: name, DefaultBranch: "master"}, code
}

func (w *backfillWorld) total(t *testing.T) int {
	t.Helper()
	rows, err := w.store.ListForUserSince(context.Background(), w.userID, w.now.AddDate(-1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range rows {
		n += r.CommitCount
	}
	return n
}

func TestBackfillRecentCommits_CountsOnlyCommitsInsideTheWindowByKnownAuthors(t *testing.T) {
	w := newBackfillWorld(t)
	repo, code := w.repo(t, "bf", "recent", 20, 5, 1)

	if err := w.stats.BackfillRecentCommits(context.Background(), []model.Repository{repo}, code, 10); err != nil {
		t.Fatalf("BackfillRecentCommits: %v", err)
	}
	if got := w.total(t); got != 2 {
		t.Errorf("counted %d commits; want the 2 inside the 10-day window", got)
	}
}

func TestBackfillRecentCommits_IgnoresAuthorsWithoutAnAccount(t *testing.T) {
	w := newBackfillWorld(t)
	repo, _ := w.repo(t, "bf", "strangers", 1)
	code := newTestRepoWithCommitsBy(t, "bf", "strangers", "nobody@elsewhere.invalid", []time.Time{w.now.AddDate(0, 0, -1)})

	if err := w.stats.BackfillRecentCommits(context.Background(), []model.Repository{repo}, code, 10); err != nil {
		t.Fatalf("BackfillRecentCommits: %v", err)
	}
	if got := w.total(t); got != 0 {
		t.Errorf("counted %d commits; want 0", got)
	}
}

func TestBackfillRecentCommits_SkipsReposThatAlreadyHaveCountsSoLiveIngestIsNotDoubled(t *testing.T) {
	w := newBackfillWorld(t)
	repo, code := w.repo(t, "bf", "twice", 2, 1)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := w.stats.BackfillRecentCommits(ctx, []model.Repository{repo}, code, 10); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	if got := w.total(t); got != 2 {
		t.Errorf("counted %d commits after two runs; want 2", got)
	}
}

func TestBackfillRecentCommits_FallsBackToHeadWithoutADefaultBranch(t *testing.T) {
	w := newBackfillWorld(t)
	repo, code := w.repo(t, "bf", "nodefault", 1)
	repo.DefaultBranch = ""

	if err := w.stats.BackfillRecentCommits(context.Background(), []model.Repository{repo}, code, 10); err != nil {
		t.Fatalf("BackfillRecentCommits: %v", err)
	}
	if got := w.total(t); got != 1 {
		t.Errorf("counted %d commits; want 1", got)
	}
}

func TestBackfillRecentCommits_OneBrokenRepoDoesNotStopTheRest(t *testing.T) {
	w := newBackfillWorld(t)
	good, code := w.repo(t, "bf", "good", 1)
	noDirectory := model.Repository{ID: good.ID, OwnerName: "bf", Name: "missing", DefaultBranch: "master"}
	// No repositories row has this ID: the log succeeds, the insert violates the foreign key.
	noRow := good
	noRow.ID = -1

	repos := []model.Repository{noDirectory, noRow, good}
	if err := w.stats.BackfillRecentCommits(context.Background(), repos, code, 10); err != nil {
		t.Fatalf("BackfillRecentCommits: %v", err)
	}
	if got := w.total(t); got != 1 {
		t.Errorf("counted %d commits; want the good repo's 1", got)
	}
}

func TestBackfillRecentCommits_StopsWhenCancelled(t *testing.T) {
	w := newBackfillWorld(t)
	repo, code := w.repo(t, "bf", "cancelled", 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := w.stats.BackfillRecentCommits(ctx, []model.Repository{repo}, code, 10); err != context.Canceled {
		t.Errorf("err = %v; want context.Canceled", err)
	}
	if got := w.total(t); got != 0 {
		t.Errorf("counted %d commits; want 0", got)
	}
}

func TestBackfillRecentCommits_CarriesOnWhenTheExistenceCheckFails(t *testing.T) {
	w := newBackfillWorld(t)
	repo, code := w.repo(t, "bf", "dbdown", 1)
	closed := testutil.OpenTestDB(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	broken := NewCommitStatsService(store.NewCommitStatsStore(closed), store.NewUserStore(w.db))

	if err := broken.BackfillRecentCommits(context.Background(), []model.Repository{repo}, code, 10); err != nil {
		t.Errorf("err = %v; want the failure counted, not returned", err)
	}
	if got := w.total(t); got != 0 {
		t.Errorf("counted %d commits; want 0", got)
	}
}
