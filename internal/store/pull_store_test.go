package store_test

// Integration tests for PullStore. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"slices"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedPullDeps seeds an owner user and repo, returning the pull store,
// repo ID, and owner ID for use in pull request tests.
func seedPullDeps(t *testing.T) (*store.PullStore, int64, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	return store.NewPullStore(db), repoID, ownerID
}

// TestPullStore_Create_AssignsIDAndNumber verifies that Create inserts a pull request,
// assigns a non-zero database ID, and assigns a repo-scoped sequential number.
func TestPullStore_Create_AssignsIDAndNumber(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)

	pr := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Test PR",
		Body:       "body",
		HeadBranch: "feature",
		BaseBranch: "main",
		State:      model.PRStateOpen,
	}
	if err := ps.Create(context.Background(), pr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if pr.ID == 0 {
		t.Error("Create must assign a non-zero ID")
	}
	if pr.Number == 0 {
		t.Error("Create must assign a non-zero sequential number")
	}
}

// TestPullStore_GetByNumber_ReturnsCorrectPR verifies that GetByNumber finds a pull
// request by its repo-scoped number after creation.
func TestPullStore_GetByNumber_ReturnsCorrectPR(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)

	pr := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Findable PR",
		HeadBranch: "feature",
		BaseBranch: "main",
		State:      model.PRStateOpen,
	}
	if err := ps.Create(context.Background(), pr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := ps.GetByNumber(context.Background(), repoID, pr.Number)
	if err != nil {
		t.Fatalf("GetByNumber: %v", err)
	}
	if found.ID != pr.ID {
		t.Errorf("want PR ID %d, got %d", pr.ID, found.ID)
	}
	if found.Title != "Findable PR" {
		t.Errorf("want title %q, got %q", "Findable PR", found.Title)
	}
}

// TestPullStore_GetByNumber_Unknown_Error verifies that GetByNumber returns an error
// when no pull request with the given number exists in the repository.
func TestPullStore_GetByNumber_Unknown_Error(t *testing.T) {
	ps, repoID, _ := seedPullDeps(t)

	_, err := ps.GetByNumber(context.Background(), repoID, 9999)
	if err == nil {
		t.Error("GetByNumber must return an error for an unknown PR number")
	}
}

// TestPullStore_UpdateState_Closed verifies that UpdateState transitions an open pull
// request to the closed state and persists the change in the database.
func TestPullStore_UpdateState_Closed(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)

	pr := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Closeable PR",
		HeadBranch: "feature",
		BaseBranch: "main",
		State:      model.PRStateOpen,
	}
	if err := ps.Create(context.Background(), pr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ps.UpdateState(context.Background(), pr.ID, model.PRStateClosed); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	found, err := ps.GetByNumber(context.Background(), repoID, pr.Number)
	if err != nil {
		t.Fatalf("GetByNumber: %v", err)
	}
	if found.State != model.PRStateClosed {
		t.Errorf("want state closed, got %q", found.State)
	}
}

// TestPullStore_SetDraft_True verifies that SetDraft can mark a non-draft pull
// request as a draft and the flag is persisted.
func TestPullStore_SetDraft_True(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)

	pr := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Draft PR",
		HeadBranch: "feature",
		BaseBranch: "main",
		State:      model.PRStateOpen,
		IsDraft:    false,
	}
	if err := ps.Create(context.Background(), pr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ps.SetDraft(context.Background(), pr.ID, true); err != nil {
		t.Fatalf("SetDraft: %v", err)
	}

	found, err := ps.GetByNumber(context.Background(), repoID, pr.Number)
	if err != nil {
		t.Fatalf("GetByNumber: %v", err)
	}
	if !found.IsDraft {
		t.Error("SetDraft(true) must set IsDraft=true")
	}
}

// TestPullStore_ListOpen_ReturnsOpenPRs verifies that ListOpen returns only open pull
// requests for a repository, excluding merged or closed ones.
func TestPullStore_ListOpen_ReturnsOpenPRs(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)

	open := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Open PR",
		HeadBranch: "open-feature",
		BaseBranch: "main",
		State:      model.PRStateOpen,
	}
	closed := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Closed PR",
		HeadBranch: "closed-feature",
		BaseBranch: "main",
		State:      model.PRStateOpen, // start open, then close
	}
	if err := ps.Create(context.Background(), open); err != nil {
		t.Fatalf("Create open: %v", err)
	}
	if err := ps.Create(context.Background(), closed); err != nil {
		t.Fatalf("Create closed: %v", err)
	}
	if err := ps.UpdateState(context.Background(), closed.ID, model.PRStateClosed); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	prs, err := ps.ListOpen(context.Background(), repoID)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	for _, pr := range prs {
		if pr.State != model.PRStateOpen {
			t.Errorf("ListOpen must return only open PRs, got state %q", pr.State)
		}
	}
	// The open PR must be in the results.
	found := false
	for _, pr := range prs {
		if pr.ID == open.ID {
			found = true
		}
	}
	if !found {
		t.Error("ListOpen must include the open PR we created")
	}
}

// TestPullStore_ListLinkedToIssue_ReturnsMatchingPRs verifies that
// ListLinkedToIssue returns the PRs explicitly linked to an issue via the
// pull_issue_links table, and excludes PRs that are not linked. (Explicit
// links are distinct from #N references parsed out of a PR body.)
func TestPullStore_ListLinkedToIssue_ReturnsMatchingPRs(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)

	// ListLinkedToIssue joins issues by repo + number, so issue #3 must exist.
	var issueID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, 3, $2, 'crash on startup', '', 'open') RETURNING id`,
		repoID, ownerID,
	).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}

	linkedA := &model.PullRequest{
		RepoID: repoID, AuthorID: ownerID,
		Title: "Fix the startup crash", Body: "",
		HeadBranch: "fix-a", BaseBranch: "main", State: model.PRStateOpen,
	}
	linkedB := &model.PullRequest{
		RepoID: repoID, AuthorID: ownerID,
		Title: "Rewrite the init path", Body: "",
		HeadBranch: "fix-b", BaseBranch: "main", State: model.PRStateOpen,
	}
	unlinked := &model.PullRequest{
		RepoID: repoID, AuthorID: ownerID,
		Title: "Update README", Body: "",
		HeadBranch: "readme", BaseBranch: "main", State: model.PRStateOpen,
	}
	for _, pr := range []*model.PullRequest{linkedA, linkedB, unlinked} {
		if err := ps.Create(ctx, pr); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	for _, pr := range []*model.PullRequest{linkedA, linkedB} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO pull_issue_links (pull_id, issue_id) VALUES ($1, $2)`,
			pr.ID, issueID,
		); err != nil {
			t.Fatalf("link pull %d to issue: %v", pr.ID, err)
		}
	}

	linked, err := ps.ListLinkedToIssue(ctx, issueID, &ownerID)
	if err != nil {
		t.Fatalf("ListLinkedToIssue: %v", err)
	}
	if len(linked) != 2 {
		t.Errorf("want 2 linked PRs, got %d", len(linked))
	}
	for _, pr := range linked {
		if pr.ID == unlinked.ID {
			t.Error("unlinked PR must not appear in linked results")
		}
	}
}

func TestPullStore_SetMergedRange(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)
	ctx := context.Background()
	pr := &model.PullRequest{RepoID: repoID, AuthorID: ownerID, Title: "Ranged", HeadBranch: "feature", BaseBranch: "main", State: model.PRStateOpen}
	if err := ps.Create(ctx, pr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ps.SetMergedRange(ctx, pr.ID, "base1", "head1"); err != nil {
		t.Fatalf("SetMergedRange on an open PR: %v", err)
	}
	if got, _ := ps.GetByNumber(ctx, repoID, pr.Number); got.MergedHeadSHA != "" {
		t.Fatalf("an open PR recorded a merged range: %q", got.MergedHeadSHA)
	}

	if err := ps.UpdateState(ctx, pr.ID, model.PRStateMerged); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	for _, in := range [][2]string{{"", ""}, {"base1", "head1"}, {"base2", "head2"}} {
		if err := ps.SetMergedRange(ctx, pr.ID, in[0], in[1]); err != nil {
			t.Fatalf("SetMergedRange(%v): %v", in, err)
		}
	}
	got, err := ps.GetByNumber(ctx, repoID, pr.Number)
	if err != nil {
		t.Fatalf("GetByNumber: %v", err)
	}
	if got.MergedBaseSHA != "base1" || got.MergedHeadSHA != "head1" {
		t.Errorf("range = %q, %q; want base1, head1 (an empty backfill result yields to a real one; a real one is kept)", got.MergedBaseSHA, got.MergedHeadSHA)
	}
	byID, err := ps.GetByID(ctx, pr.ID)
	if err != nil || byID.MergedHeadSHA != "head1" {
		t.Errorf("GetByID range = %q, %v; want head1", byID.MergedHeadSHA, err)
	}
}

func TestPullStore_MergedWithoutRange(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)
	ctx := context.Background()
	mk := func(title string) *model.PullRequest {
		pr := &model.PullRequest{RepoID: repoID, AuthorID: ownerID, Title: title, HeadBranch: "feature", BaseBranch: "main", State: model.PRStateOpen}
		if err := ps.Create(ctx, pr); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return pr
	}
	open, old, recorded, tried := mk("open"), mk("old"), mk("recorded"), mk("tried")
	for _, pr := range []*model.PullRequest{old, recorded, tried} {
		if err := ps.UpdateState(ctx, pr.ID, model.PRStateMerged); err != nil {
			t.Fatalf("UpdateState: %v", err)
		}
	}
	_ = ps.SetMergedRange(ctx, recorded.ID, "b", "h")
	_ = ps.SetMergedRange(ctx, tried.ID, "", "")

	list, err := ps.MergedWithoutRange(ctx)
	if err != nil {
		t.Fatalf("MergedWithoutRange: %v", err)
	}
	var ids []int64
	for _, pr := range list {
		ids = append(ids, pr.ID)
	}
	if !slices.Contains(ids, old.ID) {
		t.Error("a merged PR with no range is missing")
	}
	for _, other := range []*model.PullRequest{open, recorded, tried} {
		if slices.Contains(ids, other.ID) {
			t.Errorf("PR %q was listed but needs no backfill", other.Title)
		}
	}
	for _, pr := range list {
		if pr.ID == old.ID && (pr.RepoOwner == "" || pr.RepoName == "") {
			t.Errorf("owner/name not loaded: %+v", pr)
		}
	}
}
