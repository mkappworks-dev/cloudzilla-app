package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newPinService(db *sql.DB) (*service.UserService, *store.UserStore) {
	users := store.NewUserStore(db)
	repos := service.NewRepoService(store.NewRepoStore(db), users, store.NewOrgStore(db), nil, nil, config.GitConfig{})
	return service.NewUserService(users, config.AuthConfig{}).WithRepoService(repos), users
}

func seedPinRepos(t *testing.T, db *sql.DB, ownerID int64, ownerName, suffix string, n int) []int64 {
	t.Helper()
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = testutil.SeedRepo(t, db, ownerID, ownerName, fmt.Sprintf("%s_%d", suffix, i))
	}
	return ids
}

func pinnedIDs(t *testing.T, svc *service.UserService, userID int64, viewerID *int64) []int64 {
	t.Helper()
	repos, err := svc.PinnedRepos(context.Background(), userID, viewerID)
	if err != nil {
		t.Fatalf("PinnedRepos: %v", err)
	}
	ids := make([]int64, len(repos))
	for i, r := range repos {
		ids[i] = r.ID
	}
	return ids
}

func TestUserService_PinUnpin(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	repoIDs := seedPinRepos(t, db, userID, "testuser_"+suffix, suffix, 7)
	svc, _ := newPinService(db)

	for _, id := range repoIDs[:2] {
		if err := svc.PinRepo(ctx, userID, id); err != nil {
			t.Fatalf("pin %d: %v", id, err)
		}
	}
	if got, want := pinnedIDs(t, svc, userID, &userID), repoIDs[:2]; !reflect.DeepEqual(got, want) {
		t.Fatalf("after two pins: got %v, want %v", got, want)
	}

	if err := svc.PinRepo(ctx, userID, repoIDs[0]); err != nil {
		t.Fatalf("re-pin: %v", err)
	}
	if got, want := pinnedIDs(t, svc, userID, &userID), repoIDs[:2]; !reflect.DeepEqual(got, want) {
		t.Fatalf("re-pin must be a no-op: got %v, want %v", got, want)
	}

	if err := svc.UnpinRepo(ctx, userID, repoIDs[0]); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if got, want := pinnedIDs(t, svc, userID, &userID), repoIDs[1:2]; !reflect.DeepEqual(got, want) {
		t.Fatalf("after unpin: got %v, want %v", got, want)
	}

	for _, id := range []int64{repoIDs[0], repoIDs[2], repoIDs[3], repoIDs[4], repoIDs[5]} {
		if err := svc.PinRepo(ctx, userID, id); err != nil {
			t.Fatalf("fill pin %d: %v", id, err)
		}
	}
	if got := pinnedIDs(t, svc, userID, &userID); len(got) != service.MaxPinnedRepos {
		t.Fatalf("want %d pinned, got %v", service.MaxPinnedRepos, got)
	}
	if err := svc.PinRepo(ctx, userID, repoIDs[6]); !errors.Is(err, service.ErrPinLimit) {
		t.Fatalf("7th pin: want ErrPinLimit, got %v", err)
	}
}

// runTogether starts every call at once and returns their errors in order.
func runTogether(calls ...func() error) []error {
	errs := make([]error, len(calls))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Go(func() {
			<-start
			errs[i] = call()
		})
	}
	close(start)
	wg.Wait()
	return errs
}

func sortedPins(t *testing.T, users *store.UserStore, userID int64) []int64 {
	t.Helper()
	ids, err := users.GetPinnedRepoIDs(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetPinnedRepoIDs: %v", err)
	}
	slices.Sort(ids)
	return ids
}

// The pin modal fires one request per checkbox without waiting for the last.
func TestUserService_PinRepo_ConcurrentPinsAreNotLost(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	repoIDs := seedPinRepos(t, db, userID, "testuser_"+suffix, suffix, service.MaxPinnedRepos+2)
	svc, users := newPinService(db)

	calls := make([]func() error, len(repoIDs))
	for i, id := range repoIDs {
		calls[i] = func() error { return svc.PinRepo(ctx, userID, id) }
	}
	var won []int64
	for i, err := range runTogether(calls...) {
		switch {
		case err == nil:
			won = append(won, repoIDs[i])
		case !errors.Is(err, service.ErrPinLimit):
			t.Fatalf("pin %d: %v", repoIDs[i], err)
		}
	}
	if len(won) != service.MaxPinnedRepos {
		t.Errorf("%d pins succeeded, want exactly %d", len(won), service.MaxPinnedRepos)
	}
	if got := sortedPins(t, users, userID); !slices.Equal(got, won) {
		t.Errorf("stored pins %v, want every successful pin %v", got, won)
	}
}

func TestUserService_ConcurrentPinAndUnpin(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	repoIDs := seedPinRepos(t, db, userID, "testuser_"+suffix, suffix, service.MaxPinnedRepos)
	svc, users := newPinService(db)
	half := service.MaxPinnedRepos / 2
	for _, id := range repoIDs[:half] {
		if err := svc.PinRepo(ctx, userID, id); err != nil {
			t.Fatalf("pin %d: %v", id, err)
		}
	}

	var calls []func() error
	for i := range half {
		unpin, pin := repoIDs[i], repoIDs[half+i]
		calls = append(calls,
			func() error { return svc.UnpinRepo(ctx, userID, unpin) },
			func() error { return svc.PinRepo(ctx, userID, pin) })
	}
	for i, err := range runTogether(calls...) {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got, want := sortedPins(t, users, userID), repoIDs[half:]; !slices.Equal(got, want) {
		t.Errorf("stored pins %v, want %v", got, want)
	}
}

// The profile only counts pins the owner can still see, so the limit must too.
func TestUserService_PinRepo_StalePinsFreeTheirSlots(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	otherID := testutil.SeedUser(t, db, suffix+"_other")
	own := seedPinRepos(t, db, userID, "testuser_"+suffix, suffix, 7)
	foreign := testutil.SeedRepo(t, db, otherID, "testuser_"+suffix+"_other", suffix+"_foreign")
	svc, users := newPinService(db)

	full := append(append([]int64{}, own[:5]...), foreign)
	for _, id := range full {
		if err := svc.PinRepo(ctx, userID, id); err != nil {
			t.Fatalf("pin %d: %v", id, err)
		}
	}
	if err := svc.PinRepo(ctx, userID, own[5]); !errors.Is(err, service.ErrPinLimit) {
		t.Fatalf("pin past limit: want ErrPinLimit, got %v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, own[0]); err != nil {
		t.Fatalf("soft-delete: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE repositories SET private = TRUE WHERE id = $1`, foreign); err != nil {
		t.Fatalf("make foreign private: %v", err)
	}

	visible := pinnedIDs(t, svc, userID, &userID)
	if want := own[1:5]; !reflect.DeepEqual(visible, want) {
		t.Fatalf("profile pins after delete/privatize: got %v, want %v", visible, want)
	}

	for _, id := range own[5:7] {
		if err := svc.PinRepo(ctx, userID, id); err != nil {
			t.Fatalf("pin %d into a freed slot: %v", id, err)
		}
	}
	stored, err := users.GetPinnedRepoIDs(ctx, userID)
	if err != nil {
		t.Fatalf("GetPinnedRepoIDs: %v", err)
	}
	if want := own[1:7]; !reflect.DeepEqual(stored, want) {
		t.Errorf("stored pins: got %v, want %v (stale IDs pruned)", stored, want)
	}
	if err := svc.PinRepo(ctx, userID, own[0]); !errors.Is(err, service.ErrRepoNotFound) {
		t.Errorf("pin deleted repo: want ErrRepoNotFound, got %v", err)
	}
}

func TestUserService_PrivatePins(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	visitorID := testutil.SeedUser(t, db, suffix+"_visitor")
	collabID := testutil.SeedUser(t, db, suffix+"_collab")
	secret := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix+"_secret")
	if _, err := db.ExecContext(ctx, `UPDATE repositories SET private = TRUE WHERE id = $1`, secret); err != nil {
		t.Fatalf("make private: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'reader')`, collabID, secret); err != nil {
		t.Fatalf("grant collaborator: %v", err)
	}
	svc, _ := newPinService(db)

	if err := svc.PinRepo(ctx, visitorID, secret); !errors.Is(err, service.ErrRepoNotFound) {
		t.Fatalf("visitor pinning a private repo: want ErrRepoNotFound, got %v", err)
	}
	if got := pinnedIDs(t, svc, visitorID, &visitorID); len(got) != 0 {
		t.Fatalf("visitor pins: got %v, want none", got)
	}

	if err := svc.PinRepo(ctx, ownerID, secret); err != nil {
		t.Fatalf("owner pin: %v", err)
	}
	for _, tc := range []struct {
		name   string
		viewer *int64
		want   []int64
	}{
		{"anonymous", nil, []int64{}},
		{"visitor", &visitorID, []int64{}},
		{"collaborator", &collabID, []int64{secret}},
		{"owner", &ownerID, []int64{secret}},
	} {
		if got := pinnedIDs(t, svc, ownerID, tc.viewer); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s sees pins %v, want %v", tc.name, got, tc.want)
		}
	}
}
