package store_test

// Integration tests for CommitStatusStore's repo-wide listing. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type commitStatusFixture struct {
	db      *sql.DB
	cs      *store.CommitStatusStore
	ownerID int64
	repoID  int64
}

func seedCommitStatusDeps(t *testing.T) commitStatusFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	return commitStatusFixture{db: db, cs: store.NewCommitStatusStore(db), ownerID: ownerID, repoID: repoID}
}

// post upserts a status and pins its updated_at to minutesAgo, so ordering doesn't depend on NOW() resolution.
func (f commitStatusFixture) post(t *testing.T, repoID int64, sha, checkContext string, state model.CommitStatusState, minutesAgo int) {
	t.Helper()
	cs := &model.CommitStatus{RepoID: repoID, SHA: sha, Context: checkContext, State: state, CreatorID: f.ownerID}
	if err := f.cs.Upsert(context.Background(), cs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	testutil.Exec(t, f.db, `UPDATE commit_statuses SET updated_at = NOW() - make_interval(mins => $2) WHERE id = $1`, cs.ID, minutesAgo)
}

func TestCommitStatusStore_ListRecentSHAs_GroupsAndOrdersByLatestUpdate(t *testing.T) {
	f := seedCommitStatusDeps(t)
	f.post(t, f.repoID, "aaa", "build", model.CommitStatusSuccess, 30)
	f.post(t, f.repoID, "aaa", "lint", model.CommitStatusSuccess, 1)
	f.post(t, f.repoID, "bbb", "build", model.CommitStatusFailure, 10)
	f.post(t, f.repoID, "ccc", "build", model.CommitStatusPending, 20)

	got, err := f.cs.ListRecentSHAs(context.Background(), f.repoID, 10, 0)
	if err != nil {
		t.Fatalf("ListRecentSHAs: %v", err)
	}
	if want := []string{"aaa", "bbb", "ccc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListRecentSHAs = %v, want %v", got, want)
	}
}

func TestCommitStatusStore_ListRecentSHAs_Pages(t *testing.T) {
	f := seedCommitStatusDeps(t)
	for i, sha := range []string{"s1", "s2", "s3", "s4", "s5"} {
		f.post(t, f.repoID, sha, "build", model.CommitStatusSuccess, i)
	}

	first, err := f.cs.ListRecentSHAs(context.Background(), f.repoID, 2, 0)
	if err != nil {
		t.Fatalf("ListRecentSHAs: %v", err)
	}
	second, err := f.cs.ListRecentSHAs(context.Background(), f.repoID, 2, 2)
	if err != nil {
		t.Fatalf("ListRecentSHAs: %v", err)
	}
	last, err := f.cs.ListRecentSHAs(context.Background(), f.repoID, 2, 4)
	if err != nil {
		t.Fatalf("ListRecentSHAs: %v", err)
	}
	if !reflect.DeepEqual(first, []string{"s1", "s2"}) || !reflect.DeepEqual(second, []string{"s3", "s4"}) || !reflect.DeepEqual(last, []string{"s5"}) {
		t.Errorf("pages = %v %v %v, want [s1 s2] [s3 s4] [s5]", first, second, last)
	}
}

func TestCommitStatusStore_ListRecentSHAs_IsolatesRepos(t *testing.T) {
	f := seedCommitStatusDeps(t)
	suffix := testutil.UniqueSuffix(t) + "b"
	otherRepoID := testutil.SeedRepo(t, f.db, f.ownerID, "other_"+suffix, suffix)
	f.post(t, f.repoID, "mine", "build", model.CommitStatusSuccess, 5)
	f.post(t, otherRepoID, "theirs", "build", model.CommitStatusSuccess, 1)

	got, err := f.cs.ListRecentSHAs(context.Background(), f.repoID, 10, 0)
	if err != nil {
		t.Fatalf("ListRecentSHAs: %v", err)
	}
	if want := []string{"mine"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListRecentSHAs = %v, want %v", got, want)
	}

	statuses, err := f.cs.ListBySHAs(context.Background(), f.repoID, []string{"mine", "theirs"})
	if err != nil {
		t.Fatalf("ListBySHAs: %v", err)
	}
	if len(statuses) != 1 || statuses[0].SHA != "mine" {
		t.Errorf("ListBySHAs returned %+v, want only sha mine", statuses)
	}
}

func TestCommitStatusStore_ListBySHAs_OrdersBySHAThenContext(t *testing.T) {
	f := seedCommitStatusDeps(t)
	f.post(t, f.repoID, "bbb", "test", model.CommitStatusSuccess, 1)
	f.post(t, f.repoID, "aaa", "lint", model.CommitStatusSuccess, 1)
	f.post(t, f.repoID, "aaa", "build", model.CommitStatusFailure, 1)
	f.post(t, f.repoID, "ccc", "build", model.CommitStatusSuccess, 1)

	statuses, err := f.cs.ListBySHAs(context.Background(), f.repoID, []string{"aaa", "bbb"})
	if err != nil {
		t.Fatalf("ListBySHAs: %v", err)
	}
	var got []string
	for _, s := range statuses {
		got = append(got, s.SHA+"/"+s.Context)
	}
	if want := []string{"aaa/build", "aaa/lint", "bbb/test"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListBySHAs = %v, want %v", got, want)
	}
}

func TestCommitStatusStore_ListBySHAs_EmptyInput(t *testing.T) {
	f := seedCommitStatusDeps(t)
	statuses, err := f.cs.ListBySHAs(context.Background(), f.repoID, nil)
	if err != nil {
		t.Fatalf("ListBySHAs: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("ListBySHAs(nil) = %+v, want none", statuses)
	}
}

func TestCombineStates(t *testing.T) {
	st := func(states ...model.CommitStatusState) []model.CommitStatus {
		out := make([]model.CommitStatus, len(states))
		for i, s := range states {
			out[i] = model.CommitStatus{State: s}
		}
		return out
	}
	cases := []struct {
		name string
		in   []model.CommitStatus
		want model.CommitStatusState
	}{
		{"none", nil, ""},
		{"all success", st(model.CommitStatusSuccess, model.CommitStatusSuccess), model.CommitStatusSuccess},
		{"pending beats success", st(model.CommitStatusSuccess, model.CommitStatusPending), model.CommitStatusPending},
		{"failure beats pending", st(model.CommitStatusFailure, model.CommitStatusPending), model.CommitStatusFailure},
		{"error beats failure", st(model.CommitStatusFailure, model.CommitStatusError, model.CommitStatusPending), model.CommitStatusError},
	}
	for _, c := range cases {
		if got := store.CombineStates(c.in); got != c.want {
			t.Errorf("%s: CombineStates = %q, want %q", c.name, got, c.want)
		}
	}
}
