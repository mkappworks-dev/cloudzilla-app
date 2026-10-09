package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type commitChecksEnv struct {
	svc      *service.CommitStatusService
	statuses *store.CommitStatusStore
	pulls    *store.PullStore
	owner    string
	repoName string
	repoID   int64
	userID   int64
	branch   string
	headSHA  string
}

func newCommitChecksEnv(t *testing.T) *commitChecksEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	repoID := testutil.SeedRepo(t, db, userID, owner, suffix)
	branch := "feature-" + suffix
	code, headSHA := seedBareRepo(t, t.TempDir(), owner, repoName, branch)

	statuses := store.NewCommitStatusStore(db)
	pulls := store.NewPullStore(db)
	return &commitChecksEnv{
		svc:      service.NewCommitStatusService(statuses, store.NewRepoStore(db), pulls, store.NewBranchProtectionStore(db), code),
		statuses: statuses,
		pulls:    pulls,
		owner:    owner,
		repoName: repoName,
		repoID:   repoID,
		userID:   userID,
		branch:   branch,
		headSHA:  headSHA,
	}
}

func (e *commitChecksEnv) protect(t *testing.T, pattern string, contexts ...string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	bp := &model.BranchProtection{RepoID: e.repoID, Pattern: pattern, RequireStatusChecks: contexts}
	if err := store.NewBranchProtectionStore(db).Create(context.Background(), bp); err != nil {
		t.Fatalf("protect: %v", err)
	}
}

func (e *commitChecksEnv) report(t *testing.T, sha, checkCtx string, state model.CommitStatusState) {
	t.Helper()
	cs := &model.CommitStatus{Context: checkCtx, State: state, CreatorID: e.userID}
	if err := e.svc.Upsert(context.Background(), e.owner, e.repoName, sha, cs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

func (e *commitChecksEnv) pull(t *testing.T, head, headSHA, base string) *model.PullRequest {
	t.Helper()
	pr := &model.PullRequest{RepoID: e.repoID, AuthorID: e.userID, Title: "pr " + head, State: model.PRStateOpen, HeadBranch: head, BaseBranch: base, HeadSHA: headSHA}
	if err := e.pulls.Create(context.Background(), pr); err != nil {
		t.Fatalf("pull create: %v", err)
	}
	return pr
}

func TestCommitStatusService_Upsert_DefaultsContextAndReplacesByContext(t *testing.T) {
	e := newCommitChecksEnv(t)
	ctx := context.Background()

	e.report(t, e.headSHA, "", model.CommitStatusPending)
	e.report(t, e.headSHA, "", model.CommitStatusSuccess)
	e.report(t, e.headSHA, "ci/lint", model.CommitStatusFailure)

	list, err := e.svc.List(ctx, e.owner, e.repoName, e.headSHA)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("statuses = %+v, want 2 (second default report replaces the first)", list)
	}
	byCtx := map[string]model.CommitStatusState{}
	for _, cs := range list {
		byCtx[cs.Context] = cs.State
		if cs.RepoID != e.repoID || cs.SHA != e.headSHA {
			t.Errorf("status %+v not bound to repo/sha", cs)
		}
	}
	if byCtx["default"] != model.CommitStatusSuccess || byCtx["ci/lint"] != model.CommitStatusFailure {
		t.Errorf("byCtx = %v", byCtx)
	}
}

func TestCommitStatusService_UnknownRepo(t *testing.T) {
	e := newCommitChecksEnv(t)
	ctx := context.Background()

	checks := map[string]func() error{
		"Upsert": func() error {
			return e.svc.Upsert(ctx, e.owner, "nope", e.headSHA, &model.CommitStatus{State: model.CommitStatusSuccess})
		},
		"List":        func() error { _, err := e.svc.List(ctx, e.owner, "nope", e.headSHA); return err },
		"GetCombined": func() error { _, _, err := e.svc.GetCombined(ctx, e.owner, "nope", e.headSHA); return err },
	}
	for name, call := range checks {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil || !strings.Contains(err.Error(), "repo not found") {
				t.Fatalf("err = %v, want repo not found", err)
			}
		})
	}
}

func TestCommitStatusService_GetCombined(t *testing.T) {
	e := newCommitChecksEnv(t)
	ctx := context.Background()

	state, statuses, err := e.svc.GetCombined(ctx, e.owner, e.repoName, e.headSHA)
	if err != nil || state != "" || len(statuses) != 0 {
		t.Fatalf("no statuses: state %q, %d statuses, err %v; want empty", state, len(statuses), err)
	}

	steps := []struct {
		ctx   string
		state model.CommitStatusState
		want  model.CommitStatusState
	}{
		{"a", model.CommitStatusSuccess, model.CommitStatusSuccess},
		{"b", model.CommitStatusPending, model.CommitStatusPending},
		{"c", model.CommitStatusFailure, model.CommitStatusFailure},
		{"d", model.CommitStatusPending, model.CommitStatusFailure},
		{"e", model.CommitStatusError, model.CommitStatusError},
	}
	for _, s := range steps {
		e.report(t, e.headSHA, s.ctx, s.state)
		got, all, err := e.svc.GetCombined(ctx, e.owner, e.repoName, e.headSHA)
		if err != nil {
			t.Fatal(err)
		}
		if got != s.want {
			t.Errorf("after %s=%s combined = %q, want %q", s.ctx, s.state, got, s.want)
		}
		if len(all) == 0 {
			t.Error("GetCombined must also return the individual statuses")
		}
	}
}

func TestCommitStatusService_Counts(t *testing.T) {
	e := newCommitChecksEnv(t)
	ctx := context.Background()

	unprotected := e.pull(t, e.branch, e.headSHA, "main")
	if req, pass, err := e.svc.Counts(ctx, unprotected.ID); err != nil || req != 0 || pass != 0 {
		t.Fatalf("no rule: %d/%d, %v; want 0/0 nil", pass, req, err)
	}

	e.protect(t, "main", "ci/build", "ci/test")
	e.report(t, e.headSHA, "ci/build", model.CommitStatusSuccess)
	e.report(t, e.headSHA, "ci/test", model.CommitStatusFailure)
	e.report(t, e.headSHA, "ci/extra", model.CommitStatusSuccess)

	tests := []struct {
		name        string
		headSHA     string
		head        string
		wantReq     int
		wantPass    int
		wantErrPart string
	}{
		{name: "cached head sha", headSHA: e.headSHA, head: e.branch, wantReq: 2, wantPass: 1},
		{name: "falls back to resolving the head branch", headSHA: "", head: e.branch, wantReq: 2, wantPass: 1},
		{name: "unresolvable head branch still reports required", headSHA: "", head: "gone", wantReq: 2, wantErrPart: "resolve head ref"},
		{name: "stale sha with no statuses", headSHA: strings.Repeat("0", 40), head: e.branch, wantReq: 2, wantPass: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := e.pull(t, tt.head, tt.headSHA, "main")
			req, pass, err := e.svc.Counts(ctx, pr.ID)
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErrPart)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if req != tt.wantReq || pass != tt.wantPass {
				t.Errorf("counts = %d/%d, want %d/%d", pass, req, tt.wantPass, tt.wantReq)
			}
		})
	}

	if req, pass, _ := e.svc.Counts(ctx, -1); req != 0 || pass != 0 {
		t.Errorf("unknown pull: %d/%d, want 0/0", pass, req)
	}
}

func TestCommitStatusService_Counts_RuleWithoutRequiredChecks(t *testing.T) {
	e := newCommitChecksEnv(t)
	e.protect(t, "main")
	pr := e.pull(t, e.branch, e.headSHA, "main")
	if req, pass, err := e.svc.Counts(context.Background(), pr.ID); err != nil || req != 0 || pass != 0 {
		t.Errorf("counts = %d/%d, %v; want 0/0 nil", pass, req, err)
	}
}

func TestCommitStatusService_Counts_NilDependencies(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := service.NewCommitStatusService(store.NewCommitStatusStore(db), store.NewRepoStore(db), nil, nil, nil)
	if req, pass, err := svc.Counts(context.Background(), 1); err != nil || req != 0 || pass != 0 {
		t.Errorf("Counts = %d/%d, %v; want 0/0 nil", pass, req, err)
	}
	got, err := svc.CountsByPullIDs(context.Background(), []int64{1})
	if err != nil || len(got) != 0 {
		t.Errorf("CountsByPullIDs = %v, %v; want empty", got, err)
	}
}

func TestCommitStatusService_CountsByPullIDs(t *testing.T) {
	e := newCommitChecksEnv(t)
	ctx := context.Background()
	e.protect(t, "main", "ci/build", "ci/test")
	e.report(t, e.headSHA, "ci/build", model.CommitStatusSuccess)
	e.report(t, e.headSHA, "ci/test", model.CommitStatusSuccess)

	green := e.pull(t, e.branch, e.headSHA, "main")
	noStatuses := e.pull(t, "other", strings.Repeat("1", 40), "main")
	noSHA := e.pull(t, "nosha", "", "main")
	unprotectedBase := e.pull(t, e.branch, e.headSHA, "release")

	got, err := e.svc.CountsByPullIDs(ctx, []int64{green.ID, noStatuses.ID, noSHA.ID, unprotectedBase.ID, -1})
	if err != nil {
		t.Fatal(err)
	}
	if c := got[green.ID]; c.Required != 2 || c.Passing != 2 {
		t.Errorf("green = %+v, want 2/2", c)
	}
	if c := got[noStatuses.ID]; c.Required != 2 || c.Passing != 0 {
		t.Errorf("noStatuses = %+v, want 0 passing of 2", c)
	}
	if _, ok := got[noSHA.ID]; ok {
		t.Error("a PR with no head sha must be omitted")
	}
	if _, ok := got[unprotectedBase.ID]; ok {
		t.Error("a PR whose base has no required checks must be omitted")
	}
	if len(got) != 2 {
		t.Errorf("result = %v, want 2 entries", got)
	}

	empty, err := e.svc.CountsByPullIDs(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("no ids: %v, %v", empty, err)
	}
}

func TestCommitStatusService_ListRecentCommits_PagingEdges(t *testing.T) {
	e := newCommitChecksEnv(t)
	ctx := context.Background()
	e.report(t, e.headSHA, "ci/build", model.CommitStatusSuccess)

	commits, hasMore, err := e.svc.ListRecentCommits(ctx, e.repoID, e.owner, e.repoName, 0, 10)
	if err != nil || hasMore || len(commits) != 1 {
		t.Fatalf("page 0 clamps to 1: %d commits, hasMore %v, err %v", len(commits), hasMore, err)
	}
	if commits[0].Subject != "feat: add feature" || commits[0].Passed != 1 || commits[0].State != model.CommitStatusSuccess {
		t.Errorf("commit = %+v", commits[0])
	}

	commits, hasMore, err = e.svc.ListRecentCommits(ctx, e.repoID, e.owner, e.repoName, 1<<40, 10)
	if err != nil || hasMore || len(commits) != 0 {
		t.Errorf("page that overflows int32 offset: %d commits, hasMore %v, err %v; want none", len(commits), hasMore, err)
	}
	commits, _, err = e.svc.ListRecentCommits(ctx, e.repoID, e.owner, e.repoName, 2, 10)
	if err != nil || len(commits) != 0 {
		t.Errorf("page 2: %d commits, err %v; want none", len(commits), err)
	}
}
