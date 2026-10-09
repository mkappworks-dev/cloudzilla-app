package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type projBoardEnv struct {
	svc      *service.ProjectService
	owner    string
	repoName string
	repoID   int64
	ownerID  int64
	writerID int64
	otherID  int64
	adminID  int64
	issues   *store.IssueStore
	pulls    *store.PullStore
}

func newProjBoardEnv(t *testing.T) *projBoardEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	writerID := testutil.SeedUser(t, db, suffix+"w")
	otherID := testutil.SeedUser(t, db, suffix+"o")
	adminID := testutil.SeedUser(t, db, suffix+"a")
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'writer')`, repoID, writerID)
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'admin')`, repoID, adminID)

	repoSvc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, config.GitConfig{})
	return &projBoardEnv{
		svc:      service.NewProjectService(store.NewProjectStore(db), repoSvc),
		owner:    ownerName,
		repoName: "testrepo_" + suffix,
		repoID:   repoID,
		ownerID:  ownerID,
		writerID: writerID,
		otherID:  otherID,
		adminID:  adminID,
		issues:   store.NewIssueStore(db),
		pulls:    store.NewPullStore(db),
	}
}

func (e *projBoardEnv) project(t *testing.T, name string) *model.Project {
	t.Helper()
	p, err := e.svc.CreateProject(context.Background(), e.owner, e.repoName, e.ownerID, name, "desc")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return p
}

func (e *projBoardEnv) column(t *testing.T, projectID int64, name string) *model.ProjectColumn {
	t.Helper()
	c, err := e.svc.CreateColumn(context.Background(), projectID, e.ownerID, name)
	if err != nil {
		t.Fatalf("CreateColumn: %v", err)
	}
	return c
}

func (e *projBoardEnv) note(t *testing.T, projectID, columnID int64, note string) *model.ProjectCard {
	t.Helper()
	title, description, _ := strings.Cut(note, "\n")
	c, err := e.svc.CreateCard(context.Background(), projectID, columnID, e.ownerID, model.CardDetails{Title: title, Description: description})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	return c
}

func TestProjectService_CreateProject(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		repo    string
		userID  int64
		wantErr error
		wantMsg string
	}{
		{name: "owner", repo: e.repoName, userID: e.ownerID},
		{name: "writer", repo: e.repoName, userID: e.writerID},
		{name: "stranger", repo: e.repoName, userID: e.otherID, wantErr: service.ErrForbidden},
		{name: "unknown repo", repo: "nope", userID: e.ownerID, wantMsg: "repo not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := e.svc.CreateProject(ctx, e.owner, tt.repo, tt.userID, "Board", "d")
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			case tt.wantMsg != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantMsg)
				}
			default:
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if p.ID == 0 || p.RepoID != e.repoID || p.Name != "Board" {
					t.Errorf("project = %+v", p)
				}
			}
		})
	}
}

func TestProjectService_GetProject_NotFound(t *testing.T) {
	e := newProjBoardEnv(t)
	if _, err := e.svc.GetProject(context.Background(), -1); !errors.Is(err, service.ErrProjectNotFound) {
		t.Fatalf("err = %v, want ErrProjectNotFound", err)
	}
}

func TestProjectService_SetProjectClosed(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "Closable")

	if _, err := e.svc.SetProjectClosed(ctx, p.ID, e.otherID, true); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("stranger close: err = %v, want ErrForbidden", err)
	}
	if _, err := e.svc.SetProjectClosed(ctx, -1, e.ownerID, true); !errors.Is(err, service.ErrProjectNotFound) {
		t.Fatalf("missing project: err = %v, want ErrProjectNotFound", err)
	}

	closed, err := e.svc.SetProjectClosed(ctx, p.ID, e.writerID, true)
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if closed.ClosedAt == nil {
		t.Error("ClosedAt = nil after close")
	}
	reopened, err := e.svc.SetProjectClosed(ctx, p.ID, e.writerID, false)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.ClosedAt != nil {
		t.Error("ClosedAt != nil after reopen")
	}
}

func TestProjectService_DeleteProject_RequiresManage(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "Doomed")

	if err := e.svc.DeleteProject(ctx, p.ID, e.writerID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("writer delete: err = %v, want ErrForbidden", err)
	}
	if err := e.svc.DeleteProject(ctx, -1, e.ownerID); !errors.Is(err, service.ErrProjectNotFound) {
		t.Fatalf("missing: err = %v, want ErrProjectNotFound", err)
	}
	if err := e.svc.DeleteProject(ctx, p.ID, e.adminID); err != nil {
		t.Fatalf("admin delete: %v", err)
	}
	if _, err := e.svc.GetProject(ctx, p.ID); !errors.Is(err, service.ErrProjectNotFound) {
		t.Fatalf("after delete: err = %v, want ErrProjectNotFound", err)
	}
}

func TestProjectService_ListByRepoWithStats(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()

	alpha := e.project(t, "Alpha Roadmap")
	e.project(t, "Beta")
	if _, err := e.svc.SetProjectClosed(ctx, alpha.ID, e.ownerID, true); err != nil {
		t.Fatal(err)
	}

	col := e.column(t, alpha.ID, "Todo")
	closedIssue := &model.Issue{RepoID: e.repoID, AuthorID: e.ownerID, Title: "done", State: model.IssueStateClosed, Visibility: "public"}
	if err := e.issues.Create(ctx, closedIssue); err != nil {
		t.Fatal(err)
	}
	openIssue := &model.Issue{RepoID: e.repoID, AuthorID: e.ownerID, Title: "wip", State: model.IssueStateOpen, Visibility: "public"}
	if err := e.issues.Create(ctx, openIssue); err != nil {
		t.Fatal(err)
	}
	for _, id := range []*int64{&closedIssue.ID, &openIssue.ID} {
		if _, err := e.svc.CreateCard(ctx, alpha.ID, col.ID, e.ownerID, model.CardDetails{IssueID: id}); err != nil {
			t.Fatal(err)
		}
	}
	e.note(t, alpha.ID, col.ID, "just a note")

	tests := []struct {
		name, status, query string
		wantNames           []string
	}{
		{"all", "", "", []string{"Alpha Roadmap", "Beta"}},
		{"open only", "open", "", []string{"Beta"}},
		{"closed only", "closed", "", []string{"Alpha Roadmap"}},
		{"query is case-insensitive", "", "ROADMAP", []string{"Alpha Roadmap"}},
		{"query without match", "", "zzz", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list, err := e.svc.ListByRepoWithStats(ctx, e.owner, e.repoName, tt.status, tt.query)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, p := range list.Projects {
				got = append(got, p.Name)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantNames, ",") {
				t.Errorf("names = %v, want %v", got, tt.wantNames)
			}
		})
	}

	list, err := e.svc.ListByRepoWithStats(ctx, e.owner, e.repoName, "open", "")
	if err != nil {
		t.Fatal(err)
	}
	if list.OpenCount != 1 || list.ClosedCount != 1 {
		t.Errorf("tab counts = %d open / %d closed, want 1/1", list.OpenCount, list.ClosedCount)
	}

	list, err = e.svc.ListByRepoWithStats(ctx, e.owner, e.repoName, "closed", "")
	if err != nil {
		t.Fatal(err)
	}
	a := list.Projects[0]
	if a.CardCount != 3 || a.LinkedCount != 2 || a.DoneCount != 1 || a.Progress() != 50 || !a.IsClosed() {
		t.Errorf("alpha stats = card %d linked %d done %d progress %d closed %v", a.CardCount, a.LinkedCount, a.DoneCount, a.Progress(), a.IsClosed())
	}

	if _, err := e.svc.ListByRepoWithStats(ctx, e.owner, "nope", "", ""); err == nil || !strings.Contains(err.Error(), "repo not found") {
		t.Errorf("unknown repo: err = %v", err)
	}
}

func TestProjectService_Columns(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "Cols")
	other := e.project(t, "Other")

	if _, err := e.svc.CreateColumn(ctx, p.ID, e.otherID, "x"); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("stranger create: err = %v", err)
	}
	if _, err := e.svc.CreateColumn(ctx, -1, e.ownerID, "x"); !errors.Is(err, service.ErrProjectNotFound) {
		t.Fatalf("missing project: err = %v", err)
	}
	col := e.column(t, p.ID, "Todo")

	if err := e.svc.DeleteColumn(ctx, p.ID, col.ID, e.otherID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("stranger delete: err = %v", err)
	}
	if err := e.svc.DeleteColumn(ctx, -1, col.ID, e.ownerID); !errors.Is(err, service.ErrProjectNotFound) {
		t.Fatalf("missing project delete: err = %v", err)
	}
	if err := e.svc.DeleteColumn(ctx, other.ID, col.ID, e.ownerID); err == nil {
		t.Fatal("deleting a column through a different project must fail")
	}
	if err := e.svc.DeleteColumn(ctx, p.ID, col.ID, e.writerID); err != nil {
		t.Fatalf("writer delete: %v", err)
	}
}

func TestProjectService_CreateCard_Guards(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "A")
	other := e.project(t, "B")
	col := e.column(t, p.ID, "Todo")
	otherCol := e.column(t, other.ID, "Todo")

	if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.otherID, model.CardDetails{Title: "n"}); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("stranger: err = %v", err)
	}
	if _, err := e.svc.CreateCard(ctx, -1, col.ID, e.ownerID, model.CardDetails{Title: "n"}); !errors.Is(err, service.ErrProjectNotFound) {
		t.Errorf("missing project: err = %v", err)
	}
	if _, err := e.svc.CreateCard(ctx, p.ID, otherCol.ID, e.ownerID, model.CardDetails{Title: "n"}); !errors.Is(err, service.ErrProjectNotFound) {
		t.Errorf("column of another project: err = %v", err)
	}
	if _, err := e.svc.CreateCard(ctx, p.ID, -1, e.ownerID, model.CardDetails{Title: "n"}); !errors.Is(err, service.ErrProjectNotFound) {
		t.Errorf("missing column: err = %v", err)
	}

	c1 := e.note(t, p.ID, col.ID, "first")
	c2 := e.note(t, p.ID, col.ID, "second")
	if c1.Position != 0 || c2.Position != 1 {
		t.Errorf("positions = %d, %d, want 0, 1", c1.Position, c2.Position)
	}
}

func TestProjectService_MoveCard(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "Move")
	other := e.project(t, "Elsewhere")
	todo := e.column(t, p.ID, "Todo")
	done := e.column(t, p.ID, "Done")
	foreign := e.column(t, other.ID, "Todo")
	a := e.note(t, p.ID, todo.ID, "a")
	b := e.note(t, p.ID, todo.ID, "b")
	e.note(t, p.ID, todo.ID, "c")

	errTests := []struct {
		name    string
		project int64
		card    int64
		col     int64
		pos     int
		user    int64
		want    error
	}{
		{"stranger", p.ID, a.ID, done.ID, 0, e.otherID, service.ErrForbidden},
		{"missing project", -1, a.ID, done.ID, 0, e.ownerID, service.ErrProjectNotFound},
		{"destination in another project", p.ID, a.ID, foreign.ID, 0, e.ownerID, service.ErrProjectNotFound},
		{"negative position", p.ID, a.ID, done.ID, -1, e.ownerID, service.ErrInvalidPosition},
		{"position past end", p.ID, a.ID, done.ID, 5, e.ownerID, service.ErrInvalidPosition},
		{"unknown card", p.ID, -1, done.ID, 0, e.ownerID, service.ErrProjectNotFound},
	}
	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			err := e.svc.MoveCard(ctx, tt.project, tt.card, tt.col, tt.pos, tt.user)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}

	if err := e.svc.MoveCard(ctx, p.ID, a.ID, done.ID, 0, e.writerID); err != nil {
		t.Fatalf("cross-column move: %v", err)
	}
	if err := e.svc.MoveCard(ctx, p.ID, b.ID, todo.ID, 1, e.writerID); err != nil {
		t.Fatalf("intra-column move: %v", err)
	}

	cols, err := e.svc.ListColumnsWithCards(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	notes := func(cards []model.ProjectCard) string {
		var out []string
		for _, c := range cards {
			out = append(out, c.Title)
		}
		return strings.Join(out, ",")
	}
	if got := notes(cols[0].Cards); got != "c,b" {
		t.Errorf("todo = %s, want c,b", got)
	}
	if got := notes(cols[1].Cards); got != "a" {
		t.Errorf("done = %s, want a", got)
	}
}

func TestProjectService_DeleteCard(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "Del")
	other := e.project(t, "Other")
	col := e.column(t, p.ID, "Todo")
	card := e.note(t, p.ID, col.ID, "x")

	if err := e.svc.DeleteCard(ctx, p.ID, card.ID, e.otherID); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("stranger: err = %v", err)
	}
	if err := e.svc.DeleteCard(ctx, -1, card.ID, e.ownerID); !errors.Is(err, service.ErrProjectNotFound) {
		t.Errorf("missing project: err = %v", err)
	}
	if err := e.svc.DeleteCard(ctx, other.ID, card.ID, e.ownerID); err == nil {
		t.Error("deleting a card through a different project must fail")
	}
	if err := e.svc.DeleteCard(ctx, p.ID, card.ID, e.ownerID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := e.svc.DeleteCard(ctx, p.ID, card.ID, e.ownerID); err == nil {
		t.Error("second delete of the same card must fail")
	}
}

func TestProjectService_ListColumnsWithCards_EmptyColumnHasNonNilCards(t *testing.T) {
	e := newProjBoardEnv(t)
	p := e.project(t, "Empty")
	e.column(t, p.ID, "Todo")

	cols, err := e.svc.ListColumnsWithCards(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || cols[0].Cards == nil || len(cols[0].Cards) != 0 {
		t.Fatalf("cols = %+v, want one column with empty non-nil Cards", cols)
	}
}

func TestProjectService_ListColumnsWithCardsExpanded(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "Expanded")
	col := e.column(t, p.ID, "Todo")

	issue := &model.Issue{RepoID: e.repoID, AuthorID: e.ownerID, Title: "Bug", State: model.IssueStateOpen, Visibility: "public"}
	if err := e.issues.Create(ctx, issue); err != nil {
		t.Fatal(err)
	}
	pr := &model.PullRequest{RepoID: e.repoID, AuthorID: e.ownerID, Title: "Fix", State: model.PRStateOpen, HeadBranch: "feat", BaseBranch: "main"}
	if err := e.pulls.Create(ctx, pr); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.ownerID, model.CardDetails{IssueID: &issue.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.ownerID, model.CardDetails{PullID: &pr.ID}); err != nil {
		t.Fatal(err)
	}
	e.note(t, p.ID, col.ID, "first line\nsecond line")
	e.note(t, p.ID, col.ID, strings.Repeat("é", 130))

	views, err := e.svc.ListColumnsWithCardsExpanded(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || len(views[0].Cards) != 4 {
		t.Fatalf("views = %+v", views)
	}
	cards := views[0].Cards
	full := e.owner + "/" + e.repoName
	if cards[0].Kind != "issue" || cards[0].Title != "Bug" || cards[0].Number != issue.Number || cards[0].State != "open" || cards[0].RepoFullName != full {
		t.Errorf("issue card = %+v", cards[0])
	}
	if cards[1].Kind != "pull" || cards[1].Title != "Fix" || cards[1].Number != pr.Number {
		t.Errorf("pull card = %+v", cards[1])
	}
	if cards[2].Kind != "note" || cards[2].Title != "first line" || cards[2].Number != 0 {
		t.Errorf("note card = %+v", cards[2])
	}
	if got := []rune(cards[3].Title); len(got) != 130 {
		t.Errorf("explicit title = %d runes, want 130 (only derived titles are truncated)", len(got))
	}

	if _, err := e.svc.ListColumnsWithCardsExpanded(ctx, -1); !errors.Is(err, service.ErrProjectNotFound) {
		t.Errorf("missing project: err = %v", err)
	}
}
