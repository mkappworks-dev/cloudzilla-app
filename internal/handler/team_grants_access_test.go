package handler_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// teamOnly adds a new org member to a new team holding role on the vault repo, with no collaborator row.
func (e orgRepoEnv) teamOnly(t *testing.T, role model.Role) signedInUser {
	t.Helper()
	ctx := context.Background()
	u := seedSignedInUser(t, e.db)
	if err := e.svc.Org.AddMember(ctx, e.org.ID, e.owner.id, u.id, model.OrgRoleMember); err != nil {
		t.Fatalf("add org member: %v", err)
	}
	team, err := e.svc.Team.Create(ctx, e.org.ID, e.owner.id, "t "+string(role)+" "+testutil.UniqueSuffix(t), "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := e.svc.Team.SetRepoGrant(ctx, e.org.ID, e.owner.id, team.Slug, e.repo.ID, string(role)); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := e.svc.Team.AddMember(ctx, e.org.ID, e.owner.id, team.Slug, u.id); err != nil {
		t.Fatalf("add team member: %v", err)
	}
	return u
}

func (e orgRepoEnv) pushBranch(t *testing.T, token, branch string) *httptest.ResponseRecorder {
	t.Helper()
	upd := packp.NewReferenceUpdateRequest()
	upd.Commands = []*packp.Command{{Name: plumbing.NewBranchReferenceName(branch), Old: plumbing.ZeroHash, New: plumbing.NewHash(e.head)}}
	_ = upd.Capabilities.Set(capability.ReportStatus)
	var pack bytes.Buffer
	if _, err := packfile.NewEncoder(&pack, memory.NewStorage(), false).Encode(nil, 0); err != nil {
		t.Fatalf("encode pack: %v", err)
	}
	upd.Packfile = io.NopCloser(&pack)
	var body bytes.Buffer
	if err := upd.Encode(&body); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, e.repoPath+"/git-receive-pack", &body)
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e orgRepoEnv) branchExists(t *testing.T, branch string) bool {
	t.Helper()
	rr := requestAPI(e.router, http.MethodGet, e.repoPath+"/info/refs?service=git-upload-pack", e.owner.token)
	return strings.Contains(rr.Body.String(), "refs/heads/"+branch)
}

func TestTeamGrants_TeamOnlyWriter_CloneAndPush(t *testing.T) {
	e := newOrgRepoEnv(t)
	w := e.teamOnly(t, model.RoleWriter)

	if code, body := e.gitRefs(t, "git-upload-pack", w.token); code != http.StatusOK || !strings.Contains(body, e.head) {
		t.Fatalf("clone as team writer: want 200 with the head, got %d", code)
	}
	if code, _ := e.gitRefs(t, "git-receive-pack", w.token); code != http.StatusOK {
		t.Fatalf("push advertisement as team writer: want 200, got %d", code)
	}
	rr := e.pushBranch(t, w.token, "from-team")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "ok refs/heads/from-team") {
		t.Fatalf("push as team writer: %d %q", rr.Code, rr.Body.String())
	}
	if !e.branchExists(t, "from-team") {
		t.Error("the pushed branch is missing")
	}
}

func TestTeamGrants_TeamOnlyReader_CanCloneButNotPush(t *testing.T) {
	e := newOrgRepoEnv(t)
	r := e.teamOnly(t, model.RoleReader)

	if code, body := e.gitRefs(t, "git-upload-pack", r.token); code != http.StatusOK || !strings.Contains(body, e.head) {
		t.Fatalf("clone as team reader: want 200 with the head, got %d", code)
	}
	if code, _ := e.gitRefs(t, "git-receive-pack", r.token); code != http.StatusForbidden {
		t.Errorf("push advertisement as team reader: want 403, got %d", code)
	}
	if rr := e.pushBranch(t, r.token, "nope"); rr.Code != http.StatusForbidden {
		t.Errorf("push as team reader: want 403, got %d", rr.Code)
	}
	if e.branchExists(t, "nope") {
		t.Error("a team reader created a branch")
	}
}

// A user in the org but on no team that holds the repo sees a missing repo, exactly like a stranger.
func TestTeamGrants_PrivateRepoStays404ForNonReaders(t *testing.T) {
	e := newOrgRepoEnv(t)
	e.teamOnly(t, model.RoleAdmin)
	bystander := seedSignedInUser(t, e.db)
	if err := e.svc.Org.AddMember(context.Background(), e.org.ID, e.owner.id, bystander.id, model.OrgRoleMember); err != nil {
		t.Fatal(err)
	}
	stranger := seedSignedInUser(t, e.db)

	e.wantNoAccess(t, bystander.token)
	e.wantNoAccess(t, stranger.token)
}

func TestTeamGrants_PullRequestsAndPrivateIssues(t *testing.T) {
	e := newOrgRepoEnv(t)
	writer := e.teamOnly(t, model.RoleWriter)
	reader := e.teamOnly(t, model.RoleReader)
	if rr := e.pushBranch(t, writer.token, "topic"); rr.Code != http.StatusOK {
		t.Fatalf("push topic: %d", rr.Code)
	}
	api := "/api/repos" + e.repoPath

	rr := requestAPIBody(e.router, http.MethodPost, api+"/pulls", writer.token, `{"title":"from a team","head_branch":"topic","base_branch":"main"}`)
	if rr.Code != http.StatusCreated {
		t.Errorf("team writer opening a pull: %d %s", rr.Code, rr.Body.String())
	}

	rr = requestAPIBody(e.router, http.MethodPost, api+"/issues", e.owner.token, `{"title":"secret plan","visibility":"private"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("owner opening a private issue: %d %s", rr.Code, rr.Body.String())
	}
	if rr := requestAPI(e.router, http.MethodGet, api+"/issues/1", writer.token); rr.Code != http.StatusOK {
		t.Errorf("team writer reading a private issue: want 200, got %d", rr.Code)
	}
	if rr := requestAPI(e.router, http.MethodGet, api+"/issues/1", reader.token); rr.Code != http.StatusNotFound {
		t.Errorf("team reader reading a private issue: want 404, got %d", rr.Code)
	}
	testutil.Exec(t, e.db, `DELETE FROM issues WHERE repo_id = $1`, e.repo.ID)
	testutil.Exec(t, e.db, `DELETE FROM pull_requests WHERE repo_id = $1`, e.repo.ID)
}

func TestTeamGrants_APIIsOwnerOnly(t *testing.T) {
	e := newOrgRepoEnv(t)
	ctx := context.Background()
	teamAdmin := e.teamOnly(t, model.RoleAdmin)
	team, err := e.svc.Team.Create(ctx, e.org.ID, e.owner.id, "api team", "")
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/orgs/" + e.org.Name + "/teams/" + team.Slug + "/repos"

	if rr := requestAPIBody(e.router, http.MethodPost, base, teamAdmin.token, `{}`); rr.Code != http.StatusForbidden && rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("team admin granting: want 403, got %d %s", rr.Code, rr.Body.String())
	}
	form := func(token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, base, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		e.router.ServeHTTP(rr, req)
		return rr
	}
	if rr := form(teamAdmin.token, "repo=vault&role=admin"); rr.Code != http.StatusForbidden {
		t.Errorf("team admin granting: want 403, got %d %s", rr.Code, rr.Body.String())
	}
	if rr := form(e.owner.token, "repo=vault&role=owner"); rr.Code != http.StatusBadRequest {
		t.Errorf("bad role: want 400, got %d", rr.Code)
	}
	if rr := form(e.owner.token, "repo=vault&role=writer"); rr.Code != http.StatusCreated {
		t.Fatalf("owner granting: want 201, got %d %s", rr.Code, rr.Body.String())
	}
	if rr := requestAPI(e.router, http.MethodGet, base, teamAdmin.token); rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "vault") {
		t.Errorf("non-owner listing grants: want 200 and none, got %d %s", rr.Code, rr.Body.String())
	}
	if rr := requestAPI(e.router, http.MethodGet, base, e.owner.token); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"role":"writer"`) {
		t.Errorf("owner listing grants: %d %s", rr.Code, rr.Body.String())
	}
	if rr := requestAPI(e.router, http.MethodDelete, base+"/vault", teamAdmin.token); rr.Code != http.StatusForbidden {
		t.Errorf("team admin revoking: want 403, got %d", rr.Code)
	}
	if rr := requestAPI(e.router, http.MethodDelete, base+"/vault", e.owner.token); rr.Code != http.StatusNoContent {
		t.Errorf("owner revoking: want 204, got %d", rr.Code)
	}
}

// The repository's collaborator section lists team grants read-only, for a manager.
func TestTeamGrants_CollaboratorSectionShowsViaTeam(t *testing.T) {
	e := newOrgRepoEnv(t)
	e.teamOnly(t, model.RoleReader)
	rr := requestAPI(e.router, http.MethodGet, e.repoPath+"/settings", e.owner.token)
	if rr.Code != http.StatusOK {
		t.Fatalf("repo settings: %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "via team") || !strings.Contains(body, "/teams/t-reader-") {
		t.Errorf("settings page has no via-team line")
	}
}
