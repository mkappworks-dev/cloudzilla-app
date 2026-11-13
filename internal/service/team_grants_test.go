package service_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type grantEnv struct {
	repoDirsEnv
	teams     *service.TeamService
	orgID     int64
	orgName   string
	ownerID   int64
	private   *model.Repository
	public    *model.Repository
	suffix    string
	teamCount int
}

func newGrantEnv(t *testing.T) *grantEnv {
	t.Helper()
	e := &grantEnv{repoDirsEnv: newRepoDirsEnv(t), suffix: testutil.UniqueSuffix(t)}
	e.teams = service.NewTeamService(store.NewTeamStore(e.db), e.orgs)
	e.ownerID, _ = e.seedUser(t)
	e.orgName = "grants_" + e.suffix
	if err := e.db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, e.orgName).Scan(&e.orgID); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, e.orgID)
	testutil.Exec(t, e.db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, e.orgID, e.ownerID)
	e.private = e.orgRepo(t, "priv", true)
	e.public = e.orgRepo(t, "pub", false)
	return e
}

func (e *grantEnv) orgRepo(t *testing.T, name string, private bool) *model.Repository {
	t.Helper()
	repo := &model.Repository{OrgID: e.orgID, OwnerName: e.orgName, Name: name, Private: private}
	if err := e.db.QueryRow(
		`INSERT INTO repositories (org_id, owner_name, name, description, private) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		e.orgID, e.orgName, name, "teamgrant"+e.suffix+" "+name, private,
	).Scan(&repo.ID); err != nil {
		t.Fatal(err)
	}
	return repo
}

// member seeds a user who belongs to the org.
func (e *grantEnv) member(t *testing.T) int64 {
	t.Helper()
	id, _ := e.seedUser(t)
	testutil.Exec(t, e.db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, e.orgID, id)
	return id
}

// team creates a team, grants it role on repo (when non-empty) and adds members.
func (e *grantEnv) team(t *testing.T, repo *model.Repository, role model.Role, members ...int64) string {
	t.Helper()
	ctx := context.Background()
	e.teamCount++
	tm, err := e.teams.Create(ctx, e.orgID, e.ownerID, fmt.Sprintf("team %d", e.teamCount), "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if role != "" {
		if _, err := e.teams.SetRepoGrant(ctx, e.orgID, e.ownerID, tm.Slug, repo.ID, string(role)); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}
	for _, m := range members {
		if _, err := e.teams.AddMember(ctx, e.orgID, e.ownerID, tm.Slug, m); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	return tm.Slug
}

func (e *grantEnv) direct(t *testing.T, repo *model.Repository, userID int64, role model.Role) {
	t.Helper()
	seedRepoRole(t, e.repoDirsEnv, repo.ID, userID, role)
}

func (e *grantEnv) access(repo *model.Repository, userID int64) (read, write, manage bool) {
	ctx := context.Background()
	return e.repos.CanRead(ctx, repo, &userID), e.repos.CanWrite(ctx, repo, userID), e.repos.CanManage(ctx, repo, userID)
}

func TestTeamGrants_ResolutionMatrix(t *testing.T) {
	const (
		R = model.RoleReader
		W = model.RoleWriter
		A = model.RoleAdmin
	)
	cases := []struct {
		name                string
		direct              model.Role
		teams               []model.Role
		read, write, manage bool
	}{
		{"no role", "", nil, false, false, false},
		{"direct reader", R, nil, true, false, false},
		{"direct writer", W, nil, true, true, false},
		{"direct admin", A, nil, true, true, true},
		{"team reader", "", []model.Role{R}, true, false, false},
		{"team writer", "", []model.Role{W}, true, true, false},
		{"team admin", "", []model.Role{A}, true, true, true},
		{"direct reader and team writer: team wins", R, []model.Role{W}, true, true, false},
		{"direct writer and team reader: direct wins", W, []model.Role{R}, true, true, false},
		{"direct admin and team reader: direct wins", A, []model.Role{R}, true, true, true},
		{"direct reader and team admin: team wins", R, []model.Role{A}, true, true, true},
		{"two teams reader and writer", "", []model.Role{R, W}, true, true, false},
		{"two teams writer and admin", "", []model.Role{W, A}, true, true, true},
		{"two teams both reader", "", []model.Role{R, R}, true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newGrantEnv(t)
			u := e.member(t)
			if tc.direct != "" {
				e.direct(t, e.private, u, tc.direct)
			}
			for _, role := range tc.teams {
				e.team(t, e.private, role, u)
			}
			read, write, manage := e.access(e.private, u)
			if read != tc.read || write != tc.write || manage != tc.manage {
				t.Errorf("read/write/manage = %v/%v/%v, want %v/%v/%v", read, write, manage, tc.read, tc.write, tc.manage)
			}
			if e.repos.IsOwner(context.Background(), e.private, u) {
				t.Error("a role or grant must never make IsOwner true")
			}
		})
	}
}

func TestTeamGrants_NonMemberOfTeamGetsNothing(t *testing.T) {
	e := newGrantEnv(t)
	u := e.member(t)
	e.team(t, e.private, model.RoleAdmin, e.member(t)) // someone else is on it
	if read, write, manage := e.access(e.private, u); read || write || manage {
		t.Errorf("org member outside the team: %v/%v/%v", read, write, manage)
	}
	stranger, _ := e.seedUser(t)
	if read, write, manage := e.access(e.private, stranger); read || write || manage {
		t.Errorf("stranger: %v/%v/%v", read, write, manage)
	}
	if e.repos.CanRead(context.Background(), e.private, nil) {
		t.Error("anonymous viewer reads a private repo")
	}
}

func TestTeamGrants_GrantIsPerRepository(t *testing.T) {
	e := newGrantEnv(t)
	u := e.member(t)
	other := e.orgRepo(t, "other", true)
	e.team(t, e.private, model.RoleAdmin, u)
	if read, _, _ := e.access(other, u); read {
		t.Error("a grant on one repository opened another private repository")
	}
}

func TestTeamGrants_PublicRepoKeepsPublicReadOnly(t *testing.T) {
	e := newGrantEnv(t)
	u := e.member(t)
	stranger, _ := e.seedUser(t)
	if read, write, manage := e.access(e.public, stranger); !read || write || manage {
		t.Errorf("stranger on public: %v/%v/%v, want read only", read, write, manage)
	}
	e.team(t, e.public, model.RoleReader, u)
	if read, write, manage := e.access(e.public, u); !read || write || manage {
		t.Errorf("team reader on public: %v/%v/%v, want read only", read, write, manage)
	}
}

func TestTeamGrants_DirectAndTeamAccessAreIndependent(t *testing.T) {
	ctx := context.Background()
	e := newGrantEnv(t)
	u := e.member(t)
	e.direct(t, e.private, u, model.RoleReader)
	slug := e.team(t, e.private, model.RoleWriter, u)

	if err := e.repos.RemoveCollaborator(ctx, e.private, e.ownerID, u); err != nil {
		t.Fatal(err)
	}
	if _, write, _ := e.access(e.private, u); !write {
		t.Error("removing the direct row dropped team access")
	}

	e.direct(t, e.private, u, model.RoleReader)
	if _, err := e.teams.RemoveRepoGrant(ctx, e.orgID, e.ownerID, slug, e.private.ID); err != nil {
		t.Fatal(err)
	}
	if read, write, _ := e.access(e.private, u); !read || write {
		t.Errorf("revoking the grant: read=%v write=%v, want the direct reader role", read, write)
	}

	slug = e.team(t, e.private, model.RoleWriter, u)
	if _, err := e.teams.RemoveMember(ctx, e.orgID, e.ownerID, slug, u); err != nil {
		t.Fatal(err)
	}
	if read, write, _ := e.access(e.private, u); !read || write {
		t.Errorf("leaving the team: read=%v write=%v, want the direct reader role", read, write)
	}
}

func TestTeamGrants_LosingOrgMembershipEndsTeamAccess(t *testing.T) {
	ctx := context.Background()
	e := newGrantEnv(t)
	u := e.member(t)
	e.team(t, e.private, model.RoleWriter, u)
	if err := e.orgs.RemoveMember(ctx, e.orgID, e.ownerID, u); err != nil {
		t.Fatal(err)
	}
	if read, _, _ := e.access(e.private, u); read {
		t.Error("a removed org member still reads through their old team")
	}
}

func TestTeamGrants_DeletingTeamEndsAccess(t *testing.T) {
	ctx := context.Background()
	e := newGrantEnv(t)
	u := e.member(t)
	slug := e.team(t, e.private, model.RoleWriter, u)
	if _, err := e.teams.Delete(ctx, e.orgID, e.ownerID, slug); err != nil {
		t.Fatal(err)
	}
	if read, _, _ := e.access(e.private, u); read {
		t.Error("deleting the team left its grant")
	}
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM team_repos WHERE repo_id = $1`, e.private.ID).Scan(&n)
	if n != 0 {
		t.Errorf("%d grants left after the team was deleted", n)
	}
}

func TestTeamGrants_DeletingRepoCascades(t *testing.T) {
	e := newGrantEnv(t)
	repo := e.orgRepo(t, "doomed", true)
	e.team(t, repo, model.RoleReader, e.member(t))
	testutil.Exec(t, e.db, `DELETE FROM repositories WHERE id = $1`, repo.ID)
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM team_repos WHERE repo_id = $1`, repo.ID).Scan(&n)
	if n != 0 {
		t.Errorf("%d grants left after the repository was deleted", n)
	}
}

func TestTeamGrants_TransferOutDeletesGrants(t *testing.T) {
	ctx := context.Background()
	e := newGrantEnv(t)
	u := e.member(t)
	e.team(t, e.private, model.RoleWriter, u)
	newOwnerID, newOwner := e.seedUser(t)
	if err := store.NewRepoStore(e.db).UpdateOwner(ctx, e.private.ID, e.orgName, e.private.Name, newOwnerID, 0, newOwner); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM team_repos WHERE repo_id = $1`, e.private.ID).Scan(&n)
	if n != 0 {
		t.Errorf("%d grants survived the transfer out of the org", n)
	}
}

func TestTeamGrants_OnlyOrgOwnerGrantsAndRevokes(t *testing.T) {
	ctx := context.Background()
	e := newGrantEnv(t)
	member := e.member(t)
	teamAdmin := e.member(t)
	slug := e.team(t, e.private, model.RoleAdmin, teamAdmin)
	other := e.orgRepo(t, "other", true)

	for who, id := range map[string]int64{"member": member, "team admin": teamAdmin} {
		if _, err := e.teams.SetRepoGrant(ctx, e.orgID, id, slug, other.ID, "admin"); !errors.Is(err, service.ErrTeamOwnerOnly) {
			t.Errorf("%s granting: got %v, want ErrTeamOwnerOnly", who, err)
		}
		if _, err := e.teams.RemoveRepoGrant(ctx, e.orgID, id, slug, e.private.ID); !errors.Is(err, service.ErrTeamOwnerOnly) {
			t.Errorf("%s revoking: got %v, want ErrTeamOwnerOnly", who, err)
		}
	}
	if _, err := e.teams.SetRepoGrant(ctx, e.orgID, e.ownerID, slug, other.ID, "owner"); !errors.Is(err, service.ErrInvalidGrantRole) {
		t.Errorf("bad role: got %v", err)
	}
	if read, _, _ := e.access(other, teamAdmin); read {
		t.Error("a refused grant took effect")
	}
}

func TestTeamGrants_RepoMustBelongToTeamsOrg(t *testing.T) {
	ctx := context.Background()
	e := newGrantEnv(t)
	slug := e.team(t, nil, "")
	personalOwner, personalName := e.seedUser(t)
	personal := testutil.SeedRepo(t, e.db, personalOwner, personalName, e.suffix)
	otherOrg := newGrantEnv(t)

	for name, repoID := range map[string]int64{"personal repo": personal, "another org's repo": otherOrg.private.ID, "missing": 1 << 40} {
		if _, err := e.teams.SetRepoGrant(ctx, e.orgID, e.ownerID, slug, repoID, "reader"); !errors.Is(err, service.ErrRepoNotInOrg) {
			t.Errorf("%s: got %v, want ErrRepoNotInOrg", name, err)
		}
	}
}

func TestTeamGrants_TeamAdminCannotTouchOwnerOnlyActions(t *testing.T) {
	ctx := context.Background()
	e := newGrantEnv(t)
	teamAdmin := e.member(t)
	e.team(t, e.private, model.RoleAdmin, teamAdmin)
	target := e.member(t)
	targetName := fmt.Sprintf("testuser_%s", e.userSuffix(t, target))

	if err := e.repos.AddCollaborator(ctx, e.private, teamAdmin, targetName, "admin"); !errors.Is(err, service.ErrAdminRoleOwnerOnly) {
		t.Errorf("team admin appointing admin: got %v", err)
	}
	if err := e.repos.AddCollaborator(ctx, e.private, teamAdmin, targetName, "reader"); err != nil {
		t.Errorf("team admin adding a reader: %v", err)
	}
	e.direct(t, e.private, e.member(t), model.RoleAdmin)
	var adminID int64
	_ = e.db.QueryRow(`SELECT user_id FROM permissions WHERE repo_id = $1 AND role = 'admin'`, e.private.ID).Scan(&adminID)
	if err := e.repos.RemoveCollaborator(ctx, e.private, teamAdmin, adminID); !errors.Is(err, service.ErrAdminRoleOwnerOnly) {
		t.Errorf("team admin removing an admin: got %v", err)
	}
	if e.repos.IsOwner(ctx, e.private, teamAdmin) {
		t.Error("team admin is an owner: transfer, archive and delete would open")
	}
	if !e.repos.CanManage(ctx, e.private, teamAdmin) {
		t.Error("team admin cannot manage settings")
	}
}

func (e *grantEnv) userSuffix(t *testing.T, id int64) string {
	t.Helper()
	var name string
	if err := e.db.QueryRow(`SELECT username FROM users WHERE id = $1`, id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name[len("testuser_"):]
}

// Every store query that filters many repositories by readableBy or issueVisibleTo must agree with CanRead and CanWrite.
func TestTeamGrants_SQLFormsMatchServiceChecks(t *testing.T) {
	ctx := context.Background()
	e := newGrantEnv(t)

	type viewer struct {
		name string
		id   int64
	}
	var viewers []viewer
	add := func(name string, id int64) { viewers = append(viewers, viewer{name, id}) }

	repos := map[string]*model.Repository{"private": e.private, "public": e.public, "bare": e.orgRepo(t, "bare", true)}
	stranger, _ := e.seedUser(t)
	add("stranger", stranger)
	add("org owner", e.ownerID)
	add("org member, no grant", e.member(t))
	for _, role := range []model.Role{model.RoleReader, model.RoleWriter, model.RoleAdmin} {
		id := e.member(t)
		e.direct(t, e.private, id, role)
		add("direct "+string(role), id)
		id = e.member(t)
		e.team(t, e.private, role, id)
		add("team "+string(role), id)
	}
	both := e.member(t)
	e.direct(t, e.private, both, model.RoleReader)
	e.team(t, e.private, model.RoleWriter, both)
	add("direct reader + team writer", both)
	add("anonymous", 0)

	issues := store.NewIssueStore(e.db)
	for name, repo := range repos {
		for _, vis := range []string{"public", "private"} {
			testutil.Exec(t, e.db,
				`INSERT INTO issues (repo_id, number, author_id, title, visibility) VALUES ($1, $2, $3, $4, $5)`,
				repo.ID, map[string]int{"public": 1, "private": 2}[vis], e.ownerID, "teamgrant"+e.suffix+" "+name+" "+vis, vis)
		}
	}
	if err := store.NewCodeSearchStore(e.db).Index(ctx, e.private.ID, "main", "a.txt", "teamgrantcode"+e.suffix); err != nil {
		t.Fatal(err)
	}
	if err := store.NewCodeSearchStore(e.db).Index(ctx, e.public.ID, "main", "a.txt", "teamgrantcode"+e.suffix); err != nil {
		t.Fatal(err)
	}
	if err := store.NewCodeSearchStore(e.db).Index(ctx, repos["bare"].ID, "main", "a.txt", "teamgrantcode"+e.suffix); err != nil {
		t.Fatal(err)
	}

	search := store.NewSearchStore(e.db)
	code := store.NewCodeSearchStore(e.db)
	names := func(rs []model.Repository) []string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Name)
		}
		sort.Strings(out)
		return out
	}

	for _, v := range viewers {
		var uid *int64
		if v.id != 0 {
			id := v.id
			uid = &id
		}
		var wantRepos, wantIssues []string
		for _, name := range []string{"bare", "private", "public"} {
			repo := repos[name]
			canRead := e.repos.CanRead(ctx, repo, uid)
			if canRead {
				wantRepos = append(wantRepos, repo.Name)
			}
			canWrite := uid != nil && e.repos.CanWrite(ctx, repo, v.id)
			if canRead {
				wantIssues = append(wantIssues, name+" public")
				if canWrite {
					wantIssues = append(wantIssues, name+" private")
				}
			}
		}
		sort.Strings(wantIssues)

		got, err := search.SearchRepos(ctx, "teamgrant"+e.suffix, uid, 50)
		if err != nil {
			t.Fatal(err)
		}
		if !equal(names(got), wantRepos) {
			t.Errorf("%s: SearchRepos = %v, want %v", v.name, names(got), wantRepos)
		}

		hits, _, err := code.Search(ctx, "teamgrantcode"+e.suffix, uid, nil, "", 1, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != len(wantRepos) {
			t.Errorf("%s: code search found %d repos, want %d", v.name, len(hits), len(wantRepos))
		}

		found, err := search.SearchIssues(ctx, "teamgrant"+e.suffix, uid, 50)
		if err != nil {
			t.Fatal(err)
		}
		var gotIssues []string
		for _, i := range found {
			gotIssues = append(gotIssues, i.Title[len("teamgrant"+e.suffix)+1:])
		}
		sort.Strings(gotIssues)
		if !equal(gotIssues, wantIssues) {
			t.Errorf("%s: SearchIssues = %v, want %v", v.name, gotIssues, wantIssues)
		}

		for name, repo := range repos {
			list, _, err := issues.ListFiltered(ctx, repo.ID, uid, store.ListFilter{})
			if err != nil {
				t.Fatal(err)
			}
			canWrite := uid != nil && e.repos.CanWrite(ctx, repo, v.id)
			wantN := 1
			if canWrite {
				wantN = 2
			}
			if len(list) != wantN {
				t.Errorf("%s: %s issue list has %d, want %d", v.name, name, len(list), wantN)
			}
		}

		visible, err := e.orgs.ListReposVisibleTo(ctx, e.orgID, uid)
		if err != nil {
			t.Fatal(err)
		}
		if !equal(names(visible), wantRepos) {
			t.Errorf("%s: ListReposVisibleTo = %v, want %v", v.name, names(visible), wantRepos)
		}

		if uid != nil {
			mine, err := e.repos.ListForUser(ctx, v.id, "all")
			if err != nil {
				t.Fatal(err)
			}
			wantMine := false
			if e.repos.CanRead(ctx, e.private, uid) && v.id != e.ownerID {
				_, wantMine = roleOf(e, v.id, e.private.ID)
			}
			gotMine := false
			for _, r := range mine {
				if r.ID == e.private.ID {
					gotMine = true
				}
			}
			if v.id != e.ownerID && gotMine != wantMine {
				t.Errorf("%s: ListForUser has private repo = %v, want %v", v.name, gotMine, wantMine)
			}
		}
	}
}

// roleOf reports whether the user holds a direct role or any team grant on repoID.
func roleOf(e *grantEnv, userID, repoID int64) (string, bool) {
	var role string
	err := e.db.QueryRow(`SELECT COALESCE((SELECT role FROM permissions WHERE repo_id = $1 AND user_id = $2), (SELECT tr.role FROM team_repos tr JOIN team_members tm ON tm.team_id = tr.team_id WHERE tr.repo_id = $1 AND tm.user_id = $2 LIMIT 1), '')`, repoID, userID).Scan(&role)
	return role, err == nil && role != ""
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
