package store_test

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func seedNumberedIssue(t *testing.T, db *sql.DB, repoID, authorID int64, number int) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(
		`INSERT INTO issues (repo_id, number, author_id, title, body, state) VALUES ($1, $2, $3, 'i', '', 'open') RETURNING id`,
		repoID, number, authorID,
	).Scan(&id); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return id
}

func issueState(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var state string
	if err := db.QueryRow(`SELECT state FROM issues WHERE id = $1`, id).Scan(&state); err != nil {
		t.Fatalf("issue state: %v", err)
	}
	return state
}

func linkSources(t *testing.T, db *sql.DB, pullID int64) map[int64]string {
	t.Helper()
	rows, err := db.Query(`SELECT issue_id, source FROM pull_issue_links WHERE pull_id = $1`, pullID)
	if err != nil {
		t.Fatalf("link sources: %v", err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var src string
		if err := rows.Scan(&id, &src); err != nil {
			t.Fatal(err)
		}
		out[id] = src
	}
	return out
}

func TestIssueEventStore_ClaimCloseOncePerPullAndCommit(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	pullID, repoID := seedPull(t, db)
	var authorID int64
	if err := db.QueryRow(`SELECT author_id FROM pull_requests WHERE id = $1`, pullID).Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	issueID := seedNumberedIssue(t, db, repoID, authorID, 1)
	events := store.NewIssueEventStore(db)
	issues := store.NewIssueStore(db)

	byPull := func() *model.IssueEvent {
		return &model.IssueEvent{IssueID: issueID, ActorID: authorID, ActorName: "a", PullID: &pullID, SourceRepoID: &repoID}
	}
	byCommit := func(sha string) *model.IssueEvent {
		return &model.IssueEvent{IssueID: issueID, ActorID: authorID, ActorName: "a", CommitSHA: sha, SourceRepoID: &repoID}
	}
	claim := func(e *model.IssueEvent) bool {
		t.Helper()
		ok, err := events.ClaimClose(ctx, e)
		if err != nil {
			t.Fatalf("ClaimClose: %v", err)
		}
		return ok
	}
	reopen := func() {
		t.Helper()
		if err := issues.UpdateState(ctx, issueID, model.IssueStateOpen); err != nil {
			t.Fatal(err)
		}
	}

	if !claim(byPull()) {
		t.Fatal("first close by the pull: want closed")
	}
	if issueState(t, db, issueID) != "closed" {
		t.Fatal("issue not closed")
	}
	if claim(byCommit("aaaa")) {
		t.Error("closing an already-closed issue claimed it")
	}
	reopen()
	if claim(byPull()) {
		t.Error("the same pull closed the issue again after a reopen")
	}
	if issueState(t, db, issueID) != "open" {
		t.Error("a refused claim changed the issue")
	}
	if !claim(byCommit("aaaa")) {
		t.Fatal("first close by the commit: want closed")
	}
	reopen()
	if claim(byCommit("aaaa")) {
		t.Error("the same commit closed the issue again after a reopen")
	}
	if !claim(byCommit("bbbb")) {
		t.Error("a new commit should close the reopened issue")
	}

	list, err := events.ListByIssue(ctx, issueID, &authorID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("want 3 closed events, got %d", len(list))
	}
	if list[0].PullID == nil || *list[0].PullID != pullID || list[0].PullNumber != 1 || list[0].SourceRepo == "" {
		t.Errorf("first event = %+v, want the pull with its number and repo", list[0])
	}
	if list[1].CommitSHA != "aaaa" {
		t.Errorf("second event commit = %q", list[1].CommitSHA)
	}
}

func TestIssueEventStore_ListHidesSourcesTheViewerCantRead(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, suffix)
	stranger := testutil.SeedUser(t, db, suffix+"x")
	issueRepo := testutil.SeedRepo(t, db, owner, "testuser_"+suffix, suffix)
	privRepo := testutil.SeedRepo(t, db, owner, "testuser_"+suffix, suffix+"p")
	testutil.Exec(t, db, `UPDATE repositories SET private = true WHERE id = $1`, privRepo)
	issueID := seedNumberedIssue(t, db, issueRepo, owner, 1)
	events := store.NewIssueEventStore(db)
	if ok, err := events.ClaimClose(ctx, &model.IssueEvent{IssueID: issueID, ActorID: owner, ActorName: "o", CommitSHA: "cafe", SourceRepoID: &privRepo}); err != nil || !ok {
		t.Fatalf("ClaimClose = %v, %v", ok, err)
	}

	got, err := events.ListByIssue(ctx, issueID, &stranger)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CommitSHA != "" || got[0].SourceRepo != "" || got[0].SourceRepoID != nil {
		t.Errorf("stranger sees %+v, want a bare close", got)
	}
	got, err = events.ListByIssue(ctx, issueID, &owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CommitSHA != "cafe" || got[0].SourceRepo == "" {
		t.Errorf("owner sees %+v, want the commit and its repo", got)
	}
}

func TestIssueStore_KeywordLinks(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	pullID, repoID := seedPull(t, db)
	var authorID int64
	if err := db.QueryRow(`SELECT author_id FROM pull_requests WHERE id = $1`, pullID).Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	i1 := seedNumberedIssue(t, db, repoID, authorID, 1)
	i2 := seedNumberedIssue(t, db, repoID, authorID, 2)
	i3 := seedNumberedIssue(t, db, repoID, authorID, 3)
	s := store.NewIssueStore(db)

	if err := s.LinkToPull(ctx, pullID, i1); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceKeywordLinks(ctx, pullID, []int64{i1, i2}); err != nil {
		t.Fatal(err)
	}
	want := map[int64]string{i1: "manual", i2: "keyword"}
	if got := linkSources(t, db, pullID); !mapsEqual(got, want) {
		t.Fatalf("after first replace = %v, want %v", got, want)
	}
	if err := s.ReplaceKeywordLinks(ctx, pullID, []int64{i3}); err != nil {
		t.Fatal(err)
	}
	want = map[int64]string{i1: "manual", i3: "keyword"}
	if got := linkSources(t, db, pullID); !mapsEqual(got, want) {
		t.Fatalf("after second replace = %v, want %v", got, want)
	}
	if err := s.ReplaceKeywordLinks(ctx, pullID, []int64{i2, i3}); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkToPull(ctx, pullID, i3); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceKeywordLinks(ctx, pullID, nil); err != nil {
		t.Fatal(err)
	}
	want = map[int64]string{i1: "manual", i3: "manual"}
	if got := linkSources(t, db, pullID); !mapsEqual(got, want) {
		t.Fatalf("a hand link of a keyword link should make it manual: %v, want %v", got, want)
	}
	ids, err := s.LinkedIssueIDs(ctx, pullID)
	if err != nil || !slices.Equal(ids, []int64{i1, i3}) {
		t.Errorf("LinkedIssueIDs = %v, %v", ids, err)
	}
}

func TestLinkedListsFilterCrossRepoRowsByReadability(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, suffix)
	stranger := testutil.SeedUser(t, db, suffix+"x")
	pubRepo := testutil.SeedRepo(t, db, owner, "testuser_"+suffix, suffix)
	privRepo := testutil.SeedRepo(t, db, owner, "testuser_"+suffix, suffix+"p")
	testutil.Exec(t, db, `UPDATE repositories SET private = true WHERE id = $1`, privRepo)

	var pubPull, privPull int64
	for _, p := range []struct {
		repo int64
		id   *int64
	}{{pubRepo, &pubPull}, {privRepo, &privPull}} {
		if err := db.QueryRow(
			`INSERT INTO pull_requests (repo_id, number, author_id, title, head_branch) VALUES ($1, 7, $2, 'p', 'p') RETURNING id`,
			p.repo, owner,
		).Scan(p.id); err != nil {
			t.Fatal(err)
		}
	}
	pubIssue := seedNumberedIssue(t, db, pubRepo, owner, 1)
	privIssue := seedNumberedIssue(t, db, privRepo, owner, 1)
	issues := store.NewIssueStore(db)
	pulls := store.NewPullStore(db)
	for _, l := range [][2]int64{{pubPull, pubIssue}, {pubPull, privIssue}, {privPull, pubIssue}} {
		if err := issues.LinkToPull(ctx, l[0], l[1]); err != nil {
			t.Fatal(err)
		}
	}

	linked, err := issues.ListLinkedToPull(ctx, pubPull, &stranger)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 1 || linked[0].ID != pubIssue {
		t.Errorf("stranger's linked issues = %+v, want only the public repo's", linked)
	}
	linked, err = issues.ListLinkedToPull(ctx, pubPull, &owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 2 || linked[0].ID != pubIssue || linked[1].RepoName != "testrepo_"+suffix+"p" {
		t.Errorf("owner's linked issues = %+v, want same-repo first, then the private repo's with its name", linked)
	}

	prs, err := pulls.ListLinkedToIssue(ctx, pubIssue, &stranger)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].ID != pubPull {
		t.Errorf("stranger's linked pulls = %+v, want only the public repo's", prs)
	}
	prs, err = pulls.ListLinkedToIssue(ctx, pubIssue, &owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 || prs[0].ID != pubPull || prs[1].RepoOwner != "testuser_"+suffix {
		t.Errorf("owner's linked pulls = %+v, want same-repo first, then the private repo's with its owner", prs)
	}
}

func mapsEqual(a, b map[int64]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
