package handler_test

// Integration tests: only a repo's owner grants, changes or removes the admin
// role, while admin collaborators still manage readers and writers. All tests
// require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const collabPassword = "collab-password"

type collabEnv struct {
	db     *sql.DB
	router http.Handler
	repo   seededRepo
	admin  signedInUser
}

func newCollabEnv(t *testing.T) collabEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, true)
	admin := seedSignedInUser(t, db)
	grantRepoRole(t, db, repo.id, admin.id, model.RoleAdmin)
	testutil.SetPassword(t, db, repo.owner.id, collabPassword)
	testutil.SetPassword(t, db, admin.id, collabPassword)
	return collabEnv{db: db, router: newAPIRouter(t, db), repo: repo, admin: admin}
}

func grantRepoRole(t *testing.T, db *sql.DB, repoID, userID int64, role model.Role) {
	t.Helper()
	testutil.Exec(t, db, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, $3)`, userID, repoID, string(role))
}

func (e collabEnv) add(t *testing.T, as signedInUser, username, role string) *httptest.ResponseRecorder {
	t.Helper()
	return postForm(t, e.router, as.token, "/api/repos"+e.repo.path+"/collaborators", url.Values{
		"username": {username}, "role": {role}, "password": {collabPassword},
	})
}

func (e collabEnv) remove(as signedInUser, userID int64) *httptest.ResponseRecorder {
	return requestAPI(e.router, http.MethodDelete, "/api/repos"+e.repo.path+"/collaborators?user_id="+strconv.FormatInt(userID, 10), as.token)
}

// roleOf returns userID's role on the repo, or "" without one.
func (e collabEnv) roleOf(t *testing.T, userID int64) string {
	t.Helper()
	var role string
	err := e.db.QueryRow(`SELECT role FROM permissions WHERE repo_id = $1 AND user_id = $2`, e.repo.id, userID).Scan(&role)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("read role: %v", err)
	}
	return role
}

func wantOwnerOnly(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "only the repository owner") {
		t.Errorf("want 403 naming the owner, got %d %s", rr.Code, rr.Body.String())
	}
}

// An admin's second account made admin would outlive the admin's own removal.
func TestAddCollaborator_AdminCannotAppointAdmin(t *testing.T) {
	env := newCollabEnv(t)
	sock := seedSignedInUser(t, env.db)

	wantOwnerOnly(t, env.add(t, env.admin, sock.name, "admin"))

	if role := env.roleOf(t, sock.id); role != "" {
		t.Errorf("admin appointed %q", role)
	}
}

func TestAddCollaborator_AdminCannotChangeAdminRole(t *testing.T) {
	env := newCollabEnv(t)
	peer := seedSignedInUser(t, env.db)
	grantRepoRole(t, env.db, env.repo.id, peer.id, model.RoleAdmin)

	wantOwnerOnly(t, env.add(t, env.admin, peer.name, "writer"))

	if role := env.roleOf(t, peer.id); role != "admin" {
		t.Errorf("peer admin is now %q", role)
	}
}

func TestRemoveCollaborator_AdminCannotRemoveAdmin(t *testing.T) {
	env := newCollabEnv(t)
	peer := seedSignedInUser(t, env.db)
	grantRepoRole(t, env.db, env.repo.id, peer.id, model.RoleAdmin)

	wantOwnerOnly(t, env.remove(env.admin, peer.id))

	if role := env.roleOf(t, peer.id); role != "admin" {
		t.Errorf("peer admin is now %q", role)
	}
}

// "owner" passes the column's CHECK yet grants only read, under an Owner label.
func TestAddCollaborator_RejectsUnknownRole(t *testing.T) {
	env := newCollabEnv(t)
	for _, role := range []string{"owner", "superadmin"} {
		t.Run(role, func(t *testing.T) {
			user := seedSignedInUser(t, env.db)

			rr := env.add(t, env.repo.owner, user.name, role)

			if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "role must be reader, writer or admin") {
				t.Errorf("want 400 naming the roles, got %d %s", rr.Code, rr.Body.String())
			}
			if got := env.roleOf(t, user.id); got != "" {
				t.Errorf("stored role %q", got)
			}
		})
	}
}

func TestCollaborators_AdminManagesWriters(t *testing.T) {
	env := newCollabEnv(t)
	user := seedSignedInUser(t, env.db)

	if rr := env.add(t, env.admin, user.name, "writer"); rr.Code != http.StatusCreated {
		t.Fatalf("add writer: want 201, got %d %s", rr.Code, rr.Body.String())
	}
	if rr := env.add(t, env.admin, user.name, "reader"); rr.Code != http.StatusCreated || env.roleOf(t, user.id) != "reader" {
		t.Fatalf("demote to reader: want 201, got %d %s", rr.Code, rr.Body.String())
	}
	if rr := env.remove(env.admin, user.id); rr.Code != http.StatusNoContent || env.roleOf(t, user.id) != "" {
		t.Errorf("remove reader: want 204, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestCollaborators_OwnerManagesAdmins(t *testing.T) {
	env := newCollabEnv(t)
	user := seedSignedInUser(t, env.db)

	if rr := env.add(t, env.repo.owner, user.name, "admin"); rr.Code != http.StatusCreated || env.roleOf(t, user.id) != "admin" {
		t.Fatalf("appoint admin: want 201, got %d %s", rr.Code, rr.Body.String())
	}
	if rr := env.add(t, env.repo.owner, env.admin.name, "writer"); rr.Code != http.StatusCreated || env.roleOf(t, env.admin.id) != "writer" {
		t.Errorf("demote admin: want 201, got %d %s", rr.Code, rr.Body.String())
	}
	if rr := env.remove(env.repo.owner, user.id); rr.Code != http.StatusNoContent || env.roleOf(t, user.id) != "" {
		t.Errorf("remove admin: want 204, got %d %s", rr.Code, rr.Body.String())
	}
}

const adminRoleOption = `<option value="admin">`

// The closing quote keeps user 5's button from matching user 55's.
func removeButtonFor(userID int64) string {
	return "/collaborators?user_id=" + strconv.FormatInt(userID, 10) + `"`
}

// renderCollaborators returns the collaborator list as viewer sees it in the
// fragment an HTMX add swaps in, which makes writer a writer, and on the
// settings page.
func (e collabEnv) renderCollaborators(t *testing.T, viewer, writer signedInUser) map[string]string {
	t.Helper()
	form := url.Values{"username": {writer.name}, "role": {"writer"}, "password": {collabPassword}}
	req := httptest.NewRequest(http.MethodPost, "/api/repos"+e.repo.path+"/collaborators", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+viewer.token)
	req.Header.Set("HX-Request", "true")
	fragment := httptest.NewRecorder()
	e.router.ServeHTTP(fragment, req)
	if fragment.Code != http.StatusOK {
		t.Fatalf("add writer: want 200, got %d %s", fragment.Code, fragment.Body.String())
	}

	page := requestPage(t, e.db, http.MethodGet, e.repo.path+"/settings", viewer.token)
	if page.Code != http.StatusOK {
		t.Fatalf("settings page: want 200, got %d %s", page.Code, page.Body.String())
	}
	return map[string]string{"fragment": fragment.Body.String(), "settings page": page.Body.String()}
}

func TestCollaborators_AdminSeesNoOwnerOnlyControls(t *testing.T) {
	env := newCollabEnv(t)
	writer := seedSignedInUser(t, env.db)

	for view, body := range env.renderCollaborators(t, env.admin, writer) {
		t.Run(view, func(t *testing.T) {
			if strings.Contains(body, adminRoleOption) {
				t.Error("admin is offered the admin role")
			}
			if strings.Contains(body, removeButtonFor(env.admin.id)) {
				t.Error("admin is offered removing an admin")
			}
			assertContains(t, body, removeButtonFor(writer.id))
		})
	}
}

func TestCollaborators_OwnerSeesOwnerOnlyControls(t *testing.T) {
	env := newCollabEnv(t)
	writer := seedSignedInUser(t, env.db)

	for view, body := range env.renderCollaborators(t, env.repo.owner, writer) {
		t.Run(view, func(t *testing.T) {
			assertContains(t, body, adminRoleOption)
			assertContains(t, body, removeButtonFor(env.admin.id))
		})
	}
}
