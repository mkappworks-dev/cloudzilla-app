package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func seedLabelRow(t *testing.T, db *sql.DB, repoID int64, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(),
		`INSERT INTO labels (repo_id, name) VALUES ($1, $2) RETURNING id`, repoID, name,
	).Scan(&id); err != nil {
		t.Fatalf("seed label: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM labels WHERE id = $1`, id) })
	return id
}

func TestProjectStore_CreateCard_TitleRoundTrips(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	_, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})
	s := store.NewProjectStore(db)
	ctx := context.Background()

	if err := s.CreateCard(ctx, &model.ProjectCard{ColumnID: cols[0], Title: "t", Note: "d"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	got, err := s.ListCardsByColumn(ctx, cols[0], nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListCardsByColumn = %v, %v", got, err)
	}
	if got[0].Title != "t" || got[0].Note != "d" || got[0].DueDate != nil {
		t.Errorf("card = %+v, want title t, note d, no due date", got[0])
	}
}

func TestProjectStore_CreateCardWithPeople(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	_, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})
	s := store.NewProjectStore(db)
	ctx := context.Background()
	var labelID int64
	if err := db.QueryRow(`INSERT INTO labels (repo_id, name) VALUES ($1, 'bug') RETURNING id`, repoID).Scan(&labelID); err != nil {
		t.Fatal(err)
	}

	card := &model.ProjectCard{ColumnID: cols[0], Title: "ok"}
	if err := s.CreateCardWithPeople(ctx, card, []int64{ownerID}, []int64{labelID}); err != nil {
		t.Fatalf("CreateCardWithPeople: %v", err)
	}
	var people int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM card_assignees WHERE card_id = $1) + (SELECT COUNT(*) FROM card_labels WHERE card_id = $1)`, card.ID).Scan(&people); err != nil || people != 2 {
		t.Fatalf("join rows = %d, %v; want 2", people, err)
	}

	bad := &model.ProjectCard{ColumnID: cols[0], Title: "rolled back"}
	if err := s.CreateCardWithPeople(ctx, bad, []int64{-1}, nil); err == nil {
		t.Fatal("want FK error for unknown assignee")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_cards WHERE column_id = $1 AND title = 'rolled back'`, cols[0]).Scan(&n); err != nil || n != 0 {
		t.Errorf("card rows after failure = %d, %v; want 0", n, err)
	}
}

func TestProjectStore_CreateCard_LinkedWithTitleAccepted(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	_, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})
	issueID := seedIssueRow(t, db, repoID, ownerID, 1, "open")
	s := store.NewProjectStore(db)

	if err := s.CreateCard(context.Background(), &model.ProjectCard{ColumnID: cols[0], IssueID: &issueID, Title: "custom"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
}

func TestProjectStore_Card_WithoutLinkOrTitleRejected(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	_, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})

	_, err := db.ExecContext(context.Background(),
		`INSERT INTO project_cards (column_id, title, note) VALUES ($1, '', 'x')`, cols[0])
	if err == nil {
		t.Fatal("insert of unlinked card with empty title succeeded, want CHECK violation")
	}
}

func TestProjectStore_SetCardDetails_ReplacesSets(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	otherID := testutil.SeedUser(t, db, suffix+"b")
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	projectID, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})
	l1 := seedLabelRow(t, db, repoID, "bug")
	l2 := seedLabelRow(t, db, repoID, "ui")
	s := store.NewProjectStore(db)
	ctx := context.Background()
	card := seedCard(t, s, cols[0], "first")

	due := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	err := s.SetCardDetails(ctx, card.ID, projectID, model.CardDetails{
		Title: "new title", Description: "new desc", DueDate: &due,
		AssigneeIDs: []int64{ownerID, otherID}, LabelIDs: []int64{l1, l2},
	})
	if err != nil {
		t.Fatalf("SetCardDetails: %v", err)
	}
	got, _ := s.ListCardsByColumn(ctx, cols[0], nil)
	if got[0].Title != "new title" || got[0].Note != "new desc" || got[0].DueDate == nil || got[0].DueDate.Format("2006-01-02") != "2026-11-02" {
		t.Errorf("card = %+v", got[0])
	}
	assignees, err := s.CardAssignees(ctx, []int64{card.ID})
	if err != nil || len(assignees[card.ID]) != 2 {
		t.Errorf("assignees = %v, %v, want 2", assignees, err)
	}
	labels, err := s.CardLabels(ctx, []int64{card.ID})
	if err != nil || len(labels[card.ID]) != 2 {
		t.Errorf("labels = %v, %v, want 2", labels, err)
	}

	err = s.SetCardDetails(ctx, card.ID, projectID, model.CardDetails{
		Title: "new title", AssigneeIDs: []int64{otherID}, LabelIDs: []int64{l2},
	})
	if err != nil {
		t.Fatalf("SetCardDetails replace: %v", err)
	}
	assignees, _ = s.CardAssignees(ctx, []int64{card.ID})
	labels, _ = s.CardLabels(ctx, []int64{card.ID})
	if len(assignees[card.ID]) != 1 || assignees[card.ID][0].ID != otherID {
		t.Errorf("assignees after replace = %v", assignees[card.ID])
	}
	if len(labels[card.ID]) != 1 || labels[card.ID][0].ID != l2 {
		t.Errorf("labels after replace = %v", labels[card.ID])
	}
	got, _ = s.ListCardsByColumn(ctx, cols[0], nil)
	if got[0].DueDate != nil {
		t.Errorf("due date = %v, want cleared", got[0].DueDate)
	}

	if err := s.SetCardDetails(ctx, card.ID, projectID, model.CardDetails{Title: "new title"}); err != nil {
		t.Fatalf("SetCardDetails clear: %v", err)
	}
	assignees, _ = s.CardAssignees(ctx, []int64{card.ID})
	labels, _ = s.CardLabels(ctx, []int64{card.ID})
	if len(assignees) != 0 || len(labels) != 0 {
		t.Errorf("sets not cleared: %v %v", assignees, labels)
	}
}

func TestProjectStore_SetCardDetails_OtherProjectRejected(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	_, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})
	otherProject, _ := seedProjectWithColumns(t, db, repoID, []string{"todo"})
	s := store.NewProjectStore(db)
	card := seedCard(t, s, cols[0], "first")

	err := s.SetCardDetails(context.Background(), card.ID, otherProject, model.CardDetails{Title: "x"})
	if !errors.Is(err, store.ErrCardNotInProject) {
		t.Errorf("err = %v, want ErrCardNotInProject", err)
	}
}

func TestProjectStore_GetCardInProject(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	projectID, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})
	otherProject, _ := seedProjectWithColumns(t, db, repoID, []string{"todo"})
	s := store.NewProjectStore(db)
	ctx := context.Background()
	card := seedCard(t, s, cols[0], "first\nsecond")

	got, err := s.GetCardInProject(ctx, card.ID, projectID)
	if err != nil || got.ID != card.ID || got.Title != "first" || got.Note != "second" {
		t.Errorf("GetCardInProject = %+v, %v", got, err)
	}
	if _, err := s.GetCardInProject(ctx, card.ID, otherProject); !errors.Is(err, store.ErrCardNotInProject) {
		t.Errorf("other project err = %v, want ErrCardNotInProject", err)
	}
}

func TestProjectStore_DeletingLabelRemovesCardLabel(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	projectID, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})
	label := seedLabelRow(t, db, repoID, "bug")
	s := store.NewProjectStore(db)
	ctx := context.Background()
	card := seedCard(t, s, cols[0], "first")
	if err := s.SetCardDetails(ctx, card.ID, projectID, model.CardDetails{Title: "first", LabelIDs: []int64{label}}); err != nil {
		t.Fatal(err)
	}

	testutil.Exec(t, db, `DELETE FROM labels WHERE id = $1`, label)
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM card_labels WHERE card_id = $1`, card.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("card_labels rows = %d, %v, want 0", n, err)
	}
}

func TestProjectStore_LinkedLabelsAndAssignees(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	label := seedLabelRow(t, db, repoID, "bug")
	var issueID, bareIssueID, pullID int64
	for _, row := range []struct {
		dst  *int64
		q    string
		args []any
	}{
		{&issueID, `INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 1, $2, 'i') RETURNING id`, []any{repoID, ownerID}},
		{&bareIssueID, `INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 2, $2, 'bare') RETURNING id`, []any{repoID, ownerID}},
		{&pullID, `INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch) VALUES ($1, 3, $2, 'p', 'open', 'f', 'main') RETURNING id`, []any{repoID, ownerID}},
	} {
		if err := db.QueryRow(row.q, row.args...).Scan(row.dst); err != nil {
			t.Fatal(err)
		}
	}
	testutil.Exec(t, db, `INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)`, issueID, label)
	testutil.Exec(t, db, `INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`, issueID, ownerID)
	testutil.Exec(t, db, `INSERT INTO pull_labels (pull_id, label_id) VALUES ($1, $2)`, pullID, label)
	testutil.Exec(t, db, `INSERT INTO pull_assignees (pull_id, user_id) VALUES ($1, $2)`, pullID, ownerID)
	s := store.NewProjectStore(db)
	ctx := context.Background()

	labels, err := s.LinkedLabels(ctx, "issue", []int64{issueID, bareIssueID})
	if err != nil || len(labels) != 1 || len(labels[issueID]) != 1 || labels[issueID][0].ID != label {
		t.Errorf("LinkedLabels(issue) = %v, %v", labels, err)
	}
	assignees, err := s.LinkedAssignees(ctx, "issue", []int64{issueID, bareIssueID})
	if err != nil || len(assignees) != 1 || len(assignees[issueID]) != 1 || assignees[issueID][0].ID != ownerID {
		t.Errorf("LinkedAssignees(issue) = %v, %v", assignees, err)
	}
	labels, err = s.LinkedLabels(ctx, "pull", []int64{pullID})
	if err != nil || len(labels[pullID]) != 1 {
		t.Errorf("LinkedLabels(pull) = %v, %v", labels, err)
	}
	assignees, err = s.LinkedAssignees(ctx, "pull", []int64{pullID})
	if err != nil || len(assignees[pullID]) != 1 {
		t.Errorf("LinkedAssignees(pull) = %v, %v", assignees, err)
	}
	if got, err := s.LinkedLabels(ctx, "issue", nil); err != nil || len(got) != 0 {
		t.Errorf("LinkedLabels(nil) = %v, %v, want empty", got, err)
	}
	if _, err := s.LinkedLabels(ctx, "gist", []int64{1}); err == nil {
		t.Error("LinkedLabels with an unknown kind succeeded")
	}
	if _, err := s.LinkedAssignees(ctx, "gist", []int64{1}); err == nil {
		t.Error("LinkedAssignees with an unknown kind succeeded")
	}
}

func TestProjectStore_LabelsInRepo(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	otherSuffix := suffix + "o"
	otherRepo := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, otherSuffix)
	mine := seedLabelRow(t, db, repoID, "bug")
	foreign := seedLabelRow(t, db, otherRepo, "bug")
	s := store.NewProjectStore(db)
	ctx := context.Background()

	for _, tc := range []struct {
		ids  []int64
		want bool
	}{{nil, true}, {[]int64{mine}, true}, {[]int64{mine, mine}, true}, {[]int64{mine, foreign}, false}, {[]int64{999999999}, false}} {
		got, err := s.LabelsInRepo(ctx, repoID, tc.ids)
		if err != nil || got != tc.want {
			t.Errorf("LabelsInRepo(%v) = %v, %v, want %v", tc.ids, got, err, tc.want)
		}
	}
}
