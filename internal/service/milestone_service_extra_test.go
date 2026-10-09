package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type milestoneEnv struct {
	svc      *service.MilestoneService
	ownerID  int64
	owner    string
	repo     string
	repoID   int64
	db       *sql.DB
	otherRep int64
}

func newMilestoneEnv(t *testing.T) milestoneEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, owner, suffix)
	otherSuffix := "o" + suffix
	otherRepoID := testutil.SeedRepo(t, db, ownerID, owner, otherSuffix)
	return milestoneEnv{
		db:       db,
		svc:      service.NewMilestoneService(store.NewMilestoneStore(db), store.NewRepoStore(db)),
		ownerID:  ownerID,
		owner:    owner,
		repo:     "testrepo_" + suffix,
		repoID:   repoID,
		otherRep: otherRepoID,
	}
}

func (e milestoneEnv) insertIssue(t *testing.T, repoID int64, number int, state, visibility string) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility) VALUES ($1, $2, $3, 'i', '', $4, $5) RETURNING id`,
		repoID, number, e.ownerID, state, visibility).Scan(&id); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	return id
}

func (e milestoneEnv) insertPull(t *testing.T, repoID int64, number int, state string) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(
		`INSERT INTO pull_requests (repo_id, number, author_id, title, body, state, head_branch, base_branch) VALUES ($1, $2, $3, 'p', '', $4, 'f', 'main') RETURNING id`,
		repoID, number, e.ownerID, state).Scan(&id); err != nil {
		t.Fatalf("insert pull: %v", err)
	}
	return id
}

func TestMilestoneService_UnknownRepoFailsEverywhere(t *testing.T) {
	e := newMilestoneEnv(t)
	ctx := context.Background()
	if _, err := e.svc.Create(ctx, e.owner, "nope", "t", "", nil); err == nil {
		t.Error("Create on unknown repo must fail")
	}
	if _, err := e.svc.ListByRepo(ctx, e.owner, "nope", nil); err == nil {
		t.Error("ListByRepo on unknown repo must fail")
	}
	if _, err := e.svc.GetByNumber(ctx, e.owner, "nope", 1, nil); err == nil {
		t.Error("GetByNumber on unknown repo must fail")
	}
	if _, err := e.svc.Update(ctx, e.owner, "nope", 1, "t", "", nil, nil); err == nil {
		t.Error("Update on unknown repo must fail")
	}
	if _, err := e.svc.Close(ctx, e.owner, "nope", 1, nil); err == nil {
		t.Error("Close on unknown repo must fail")
	}
	if _, err := e.svc.Reopen(ctx, e.owner, "nope", 1, nil); err == nil {
		t.Error("Reopen on unknown repo must fail")
	}
	if err := e.svc.Delete(ctx, e.owner, "nope", 1); err == nil {
		t.Error("Delete on unknown repo must fail")
	}
}

func TestMilestoneService_UnknownMilestoneNumberFails(t *testing.T) {
	e := newMilestoneEnv(t)
	ctx := context.Background()
	if _, err := e.svc.GetByNumber(ctx, e.owner, e.repo, 99, nil); err == nil {
		t.Error("GetByNumber of missing milestone must fail")
	}
	if _, err := e.svc.Update(ctx, e.owner, e.repo, 99, "t", "", nil, nil); err == nil {
		t.Error("Update of missing milestone must fail")
	}
	if _, err := e.svc.Close(ctx, e.owner, e.repo, 99, nil); err == nil {
		t.Error("Close of missing milestone must fail")
	}
	if _, err := e.svc.Reopen(ctx, e.owner, e.repo, 99, nil); err == nil {
		t.Error("Reopen of missing milestone must fail")
	}
}

func TestMilestoneService_UpdatePersistsFieldsAndKeepsState(t *testing.T) {
	e := newMilestoneEnv(t)
	ctx := context.Background()
	m, err := e.svc.Create(ctx, e.owner, e.repo, "v1", "old", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	due := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	if _, err := e.svc.Update(ctx, e.owner, e.repo, m.Number, "v1.1", "new", &due, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := e.svc.GetByNumber(ctx, e.owner, e.repo, m.Number, nil)
	if err != nil {
		t.Fatalf("GetByNumber: %v", err)
	}
	if got.Title != "v1.1" || got.Description != "new" || got.State != "open" {
		t.Errorf("milestone = %+v", got)
	}
	if got.DueDate == nil || !got.DueDate.Equal(due) {
		t.Errorf("due date = %v, want %v", got.DueDate, due)
	}
	byID, err := e.svc.GetByID(ctx, m.ID, nil)
	if err != nil || byID.Number != m.Number {
		t.Errorf("GetByID = %+v, %v", byID, err)
	}
}

func TestMilestoneService_CloseThenReopenClearsClosedAt(t *testing.T) {
	e := newMilestoneEnv(t)
	ctx := context.Background()
	m, _ := e.svc.Create(ctx, e.owner, e.repo, "v1", "", nil)
	closed, err := e.svc.Close(ctx, e.owner, e.repo, m.Number, nil)
	if err != nil || closed.ClosedAt == nil {
		t.Fatalf("Close = %+v, %v", closed, err)
	}
	reopened, err := e.svc.Reopen(ctx, e.owner, e.repo, m.Number, nil)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if reopened.State != "open" || reopened.ClosedAt != nil {
		t.Errorf("reopened = %+v", reopened)
	}
}

func TestMilestoneService_AssignIssueAndPull(t *testing.T) {
	e := newMilestoneEnv(t)
	ctx := context.Background()
	m, _ := e.svc.Create(ctx, e.owner, e.repo, "v1", "", nil)
	issueID := e.insertIssue(t, e.repoID, 1, "open", "public")
	closedIssueID := e.insertIssue(t, e.repoID, 2, "closed", "public")
	pullOpen := e.insertPull(t, e.repoID, 3, "open")
	pullMerged := e.insertPull(t, e.repoID, 4, "merged")

	for _, id := range []int64{issueID, closedIssueID} {
		if err := e.svc.SetIssue(ctx, id, &m.ID); err != nil {
			t.Fatalf("SetIssue: %v", err)
		}
	}
	for _, id := range []int64{pullOpen, pullMerged} {
		if err := e.svc.SetPull(ctx, id, &m.ID); err != nil {
			t.Fatalf("SetPull: %v", err)
		}
	}

	if got, err := e.svc.GetForIssue(ctx, issueID, nil); err != nil || got == nil || got.ID != m.ID {
		t.Errorf("GetForIssue = %+v, %v", got, err)
	}
	if got, err := e.svc.GetForPull(ctx, pullOpen, nil); err != nil || got == nil || got.ID != m.ID {
		t.Errorf("GetForPull = %+v, %v", got, err)
	}

	open, err := e.svc.ListIssues(ctx, m.ID, "open", nil, 1, 10)
	if err != nil || len(open) != 1 || open[0].ID != issueID {
		t.Errorf("open issues = %+v, %v", open, err)
	}
	closedIssues, _ := e.svc.ListIssues(ctx, m.ID, "closed", nil, 1, 10)
	if len(closedIssues) != 1 || closedIssues[0].ID != closedIssueID {
		t.Errorf("closed issues = %+v", closedIssues)
	}

	openPulls, err := e.svc.ListPulls(ctx, m.ID, "open", 1, 10)
	if err != nil || len(openPulls) != 1 || openPulls[0].ID != pullOpen {
		t.Errorf("open pulls = %+v, %v", openPulls, err)
	}
	closedPulls, _ := e.svc.ListPulls(ctx, m.ID, "closed", 1, 10)
	if len(closedPulls) != 1 || closedPulls[0].ID != pullMerged {
		t.Errorf("closed pulls must include merged: %+v", closedPulls)
	}
	if o, c, err := e.svc.PullCounts(ctx, m.ID); err != nil || o != 1 || c != 1 {
		t.Errorf("PullCounts = %d/%d, %v; want 1/1", o, c, err)
	}

	if err := e.svc.SetIssue(ctx, issueID, nil); err != nil {
		t.Fatalf("clear SetIssue: %v", err)
	}
	if got, err := e.svc.GetForIssue(ctx, issueID, nil); err != nil || got != nil {
		t.Errorf("GetForIssue after clear = %+v, %v; want nil, nil", got, err)
	}
	if err := e.svc.SetPull(ctx, pullOpen, nil); err != nil {
		t.Fatalf("clear SetPull: %v", err)
	}
	if got, err := e.svc.GetForPull(ctx, pullOpen, nil); err != nil || got != nil {
		t.Errorf("GetForPull after clear = %+v, %v; want nil, nil", got, err)
	}
}

func TestMilestoneService_SetRefusesCrossRepoAndMissing(t *testing.T) {
	e := newMilestoneEnv(t)
	ctx := context.Background()
	m, _ := e.svc.Create(ctx, e.owner, e.repo, "v1", "", nil)
	foreignIssue := e.insertIssue(t, e.otherRep, 1, "open", "public")
	foreignPull := e.insertPull(t, e.otherRep, 2, "open")

	if err := e.svc.SetIssue(ctx, foreignIssue, &m.ID); !errors.Is(err, service.ErrMilestoneRepoMismatch) {
		t.Errorf("cross-repo SetIssue err = %v, want ErrMilestoneRepoMismatch", err)
	}
	if err := e.svc.SetPull(ctx, foreignPull, &m.ID); !errors.Is(err, service.ErrMilestoneRepoMismatch) {
		t.Errorf("cross-repo SetPull err = %v, want ErrMilestoneRepoMismatch", err)
	}
	missing := int64(1 << 40)
	if err := e.svc.SetIssue(ctx, foreignIssue, &missing); !errors.Is(err, service.ErrMilestoneNotFound) {
		t.Errorf("missing-milestone SetIssue err = %v, want ErrMilestoneNotFound", err)
	}
	if err := e.svc.SetPull(ctx, foreignPull, &missing); !errors.Is(err, service.ErrMilestoneNotFound) {
		t.Errorf("missing-milestone SetPull err = %v, want ErrMilestoneNotFound", err)
	}
}
