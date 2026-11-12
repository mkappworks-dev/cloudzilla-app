package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type teamEnv struct {
	db                *sql.DB
	teams             *service.TeamService
	orgs              *service.OrgService
	orgID             int64
	ownerID, memberID int64
	outsiderID        int64
}

func newTeamEnv(t *testing.T) teamEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	e := teamEnv{
		db:         db,
		ownerID:    testutil.SeedUser(t, db, "o"+suffix),
		memberID:   testutil.SeedUser(t, db, "m"+suffix),
		outsiderID: testutil.SeedUser(t, db, "x"+suffix),
	}
	e.orgs = service.NewOrgService(store.NewOrgStore(db), store.NewRepoStore(db), store.NewUserStore(db), config.GitConfig{})
	e.teams = service.NewTeamService(store.NewTeamStore(db), e.orgs)
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, "teams_"+suffix).Scan(&e.orgID); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, e.orgID)
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'member')`, e.orgID, e.ownerID, e.memberID)
	return e
}

func (e teamEnv) members(t *testing.T, slug string) []model.TeamMember {
	t.Helper()
	ms, err := e.teams.ListMembers(context.Background(), e.orgID, e.ownerID, slug)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	return ms
}

func (e teamEnv) teamMemberRows(t *testing.T, userID int64) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM team_members WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTeamSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Core Team":     "core-team",
		"  Docs & Web ": "docs-web",
		"a--b__c":       "a-b-c",
		"Ünï":           "n",
		"***":           "",
	} {
		if got := service.TeamSlug(in); got != want {
			t.Errorf("TeamSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTeamService_OnlyOwnersMutate(t *testing.T) {
	e := newTeamEnv(t)
	ctx := context.Background()
	team, err := e.teams.Create(ctx, e.orgID, e.ownerID, "Core", "")
	if err != nil {
		t.Fatalf("owner Create: %v", err)
	}
	for who, id := range map[string]int64{"member": e.memberID, "outsider": e.outsiderID} {
		if _, err := e.teams.Create(ctx, e.orgID, id, "Other", ""); !errors.Is(err, service.ErrTeamOwnerOnly) {
			t.Errorf("%s Create: want ErrTeamOwnerOnly, got %v", who, err)
		}
		if _, err := e.teams.Update(ctx, e.orgID, id, team.Slug, "Renamed", ""); !errors.Is(err, service.ErrTeamOwnerOnly) {
			t.Errorf("%s Update: want ErrTeamOwnerOnly, got %v", who, err)
		}
		if _, err := e.teams.AddMember(ctx, e.orgID, id, team.Slug, e.memberID); !errors.Is(err, service.ErrTeamOwnerOnly) {
			t.Errorf("%s AddMember: want ErrTeamOwnerOnly, got %v", who, err)
		}
		if _, err := e.teams.RemoveMember(ctx, e.orgID, id, team.Slug, e.memberID); !errors.Is(err, service.ErrTeamOwnerOnly) {
			t.Errorf("%s RemoveMember: want ErrTeamOwnerOnly, got %v", who, err)
		}
		if _, err := e.teams.Delete(ctx, e.orgID, id, team.Slug); !errors.Is(err, service.ErrTeamOwnerOnly) {
			t.Errorf("%s Delete: want ErrTeamOwnerOnly, got %v", who, err)
		}
	}
	if _, err := e.teams.Get(ctx, e.orgID, e.ownerID, team.Slug); err != nil {
		t.Errorf("team gone after refused mutations: %v", err)
	}
}

func TestTeamService_VisibleToMembersOnly(t *testing.T) {
	e := newTeamEnv(t)
	ctx := context.Background()
	team, _ := e.teams.Create(ctx, e.orgID, e.ownerID, "Core", "")
	if _, err := e.teams.AddMember(ctx, e.orgID, e.ownerID, team.Slug, e.memberID); err != nil {
		t.Fatal(err)
	}

	if teams, err := e.teams.List(ctx, e.orgID, e.memberID); err != nil || len(teams) != 1 || teams[0].MemberCount != 1 {
		t.Errorf("member List = %v, %v; want one team with one member", teams, err)
	}
	if ms, err := e.teams.ListMembers(ctx, e.orgID, e.memberID, team.Slug); err != nil || len(ms) != 1 {
		t.Errorf("member ListMembers = %v, %v", ms, err)
	}
	if _, err := e.teams.List(ctx, e.orgID, e.outsiderID); !errors.Is(err, service.ErrTeamNotFound) {
		t.Errorf("outsider List: want ErrTeamNotFound, got %v", err)
	}
	if _, err := e.teams.Get(ctx, e.orgID, e.outsiderID, team.Slug); !errors.Is(err, service.ErrTeamNotFound) {
		t.Errorf("outsider Get: want ErrTeamNotFound, got %v", err)
	}
}

func TestTeamService_SlugNormalisationAndUniqueness(t *testing.T) {
	e := newTeamEnv(t)
	ctx := context.Background()
	team, err := e.teams.Create(ctx, e.orgID, e.ownerID, "  Core Team ", " the core ")
	if err != nil {
		t.Fatal(err)
	}
	if team.Slug != "core-team" || team.Name != "Core Team" || team.Description != "the core" {
		t.Errorf("got %+v", team)
	}
	if _, err := e.teams.Create(ctx, e.orgID, e.ownerID, "core-team", ""); !errors.Is(err, service.ErrTeamSlugTaken) {
		t.Errorf("same slug: want ErrTeamSlugTaken, got %v", err)
	}
	if _, err := e.teams.Create(ctx, e.orgID, e.ownerID, "!!!", ""); !errors.Is(err, service.ErrInvalidTeamName) {
		t.Errorf("no letters: want ErrInvalidTeamName, got %v", err)
	}
	other, _ := e.teams.Create(ctx, e.orgID, e.ownerID, "Docs", "")
	if _, err := e.teams.Update(ctx, e.orgID, e.ownerID, other.Slug, "Core  Team", ""); !errors.Is(err, service.ErrTeamSlugTaken) {
		t.Errorf("rename onto a taken slug: want ErrTeamSlugTaken, got %v", err)
	}
	renamed, err := e.teams.Update(ctx, e.orgID, e.ownerID, other.Slug, "Documentation", "all docs")
	if err != nil || renamed.Slug != "documentation" {
		t.Fatalf("Update = %+v, %v", renamed, err)
	}
	if _, err := e.teams.Get(ctx, e.orgID, e.ownerID, "docs"); !errors.Is(err, service.ErrTeamNotFound) {
		t.Errorf("old slug still resolves: %v", err)
	}

	// The slug is unique per organization, not globally.
	otherOrg := newTeamEnv(t)
	if _, err := otherOrg.teams.Create(ctx, otherOrg.orgID, otherOrg.ownerID, "Core Team", ""); err != nil {
		t.Errorf("same name in another org: %v", err)
	}
}

func TestTeamService_OnlyOrgMembersJoin(t *testing.T) {
	e := newTeamEnv(t)
	ctx := context.Background()
	team, _ := e.teams.Create(ctx, e.orgID, e.ownerID, "Core", "")
	if _, err := e.teams.AddMember(ctx, e.orgID, e.ownerID, team.Slug, e.outsiderID); !errors.Is(err, service.ErrNotOrgMember) {
		t.Fatalf("want ErrNotOrgMember, got %v", err)
	}
	if n := e.teamMemberRows(t, e.outsiderID); n != 0 {
		t.Errorf("outsider has %d team_members rows", n)
	}
	// Adding twice is a no-op.
	for range 2 {
		if _, err := e.teams.AddMember(ctx, e.orgID, e.ownerID, team.Slug, e.memberID); err != nil {
			t.Fatal(err)
		}
	}
	if got := e.members(t, team.Slug); len(got) != 1 {
		t.Errorf("members = %v", got)
	}
}

func TestTeamService_TeamOfAnotherOrgIsNotReachable(t *testing.T) {
	e := newTeamEnv(t)
	other := newTeamEnv(t)
	ctx := context.Background()
	foreign, _ := other.teams.Create(ctx, other.orgID, other.ownerID, "Core", "")
	if _, err := e.teams.AddMember(ctx, e.orgID, e.ownerID, foreign.Slug, e.memberID); !errors.Is(err, service.ErrTeamNotFound) {
		t.Errorf("want ErrTeamNotFound, got %v", err)
	}
}

func TestTeamService_LeavingOrRemovedFromOrgLeavesEveryTeam(t *testing.T) {
	e := newTeamEnv(t)
	ctx := context.Background()
	a, _ := e.teams.Create(ctx, e.orgID, e.ownerID, "A", "")
	b, _ := e.teams.Create(ctx, e.orgID, e.ownerID, "B", "")
	other := newTeamEnv(t)
	otherTeam, _ := other.teams.Create(ctx, other.orgID, other.ownerID, "A", "")
	testutil.Exec(t, e.db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, other.orgID, e.memberID)
	if _, err := other.teams.AddMember(ctx, other.orgID, other.ownerID, otherTeam.Slug, e.memberID); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{a.Slug, b.Slug} {
		if _, err := e.teams.AddMember(ctx, e.orgID, e.ownerID, slug, e.memberID); err != nil {
			t.Fatal(err)
		}
	}

	if err := e.orgs.RemoveMember(ctx, e.orgID, e.memberID, e.memberID); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if got := len(e.members(t, a.Slug)) + len(e.members(t, b.Slug)); got != 0 {
		t.Errorf("%d team memberships left after leaving", got)
	}
	if n := e.teamMemberRows(t, e.memberID); n != 1 {
		t.Errorf("team_members rows = %d, want only the other org's", n)
	}

	if _, err := e.teams.AddMember(ctx, e.orgID, e.ownerID, a.Slug, e.memberID); !errors.Is(err, service.ErrNotOrgMember) {
		t.Errorf("rejoining the team after leaving the org: want ErrNotOrgMember, got %v", err)
	}
}

func TestTeamService_OwnerRemovingAMemberDropsTheirTeams(t *testing.T) {
	e := newTeamEnv(t)
	ctx := context.Background()
	team, _ := e.teams.Create(ctx, e.orgID, e.ownerID, "A", "")
	_, _ = e.teams.AddMember(ctx, e.orgID, e.ownerID, team.Slug, e.memberID)
	if err := e.orgs.RemoveMember(ctx, e.orgID, e.ownerID, e.memberID); err != nil {
		t.Fatal(err)
	}
	if n := e.teamMemberRows(t, e.memberID); n != 0 {
		t.Errorf("%d rows left", n)
	}
}

func TestTeamService_DeletingAUserLeavesNoTeamRow(t *testing.T) {
	e := newTeamEnv(t)
	ctx := context.Background()
	team, _ := e.teams.Create(ctx, e.orgID, e.ownerID, "A", "")
	_, _ = e.teams.AddMember(ctx, e.orgID, e.ownerID, team.Slug, e.memberID)

	git := config.GitConfig{ReposRoot: t.TempDir()}
	repos := service.NewRepoService(store.NewRepoStore(e.db), store.NewUserStore(e.db), store.NewOrgStore(e.db), nil, nil, git)
	users := service.NewUserService(store.NewUserStore(e.db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}).WithRepoService(repos)
	if err := users.DeleteUser(ctx, e.memberID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if n := e.teamMemberRows(t, e.memberID); n != 0 {
		t.Errorf("%d team_members rows for a deleted user", n)
	}
}

func TestTeamService_DeletingTheTeamOrOrgCascades(t *testing.T) {
	e := newTeamEnv(t)
	ctx := context.Background()
	gone, _ := e.teams.Create(ctx, e.orgID, e.ownerID, "Gone", "")
	_, _ = e.teams.AddMember(ctx, e.orgID, e.ownerID, gone.Slug, e.memberID)
	kept, _ := e.teams.Create(ctx, e.orgID, e.ownerID, "Kept", "")
	_, _ = e.teams.AddMember(ctx, e.orgID, e.ownerID, kept.Slug, e.memberID)

	if _, err := e.teams.Delete(ctx, e.orgID, e.ownerID, gone.Slug); err != nil {
		t.Fatal(err)
	}
	if n := e.teamMemberRows(t, e.memberID); n != 1 {
		t.Errorf("rows after deleting a team = %d, want 1", n)
	}
	if _, err := e.teams.Get(ctx, e.orgID, e.ownerID, "gone"); !errors.Is(err, service.ErrTeamNotFound) {
		t.Errorf("deleted team: %v", err)
	}

	if err := e.orgs.Delete(ctx, e.orgID, e.ownerID); err != nil {
		t.Fatalf("org Delete: %v", err)
	}
	var teams, rows int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM teams WHERE org_id = $1`, e.orgID).Scan(&teams)
	rows = e.teamMemberRows(t, e.memberID)
	if teams != 0 || rows != 0 {
		t.Errorf("after org delete: %d teams, %d team_members", teams, rows)
	}
}
