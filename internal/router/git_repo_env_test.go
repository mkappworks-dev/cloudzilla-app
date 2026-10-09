package router_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const r2Password = "r2-owner-password"

// r2Env is a public repo with a real bare repository (one README commit on main)
// plus an owner, a writer, an admin collaborator and an outsider.
type r2Env struct {
	h                              http.Handler
	svc                            *service.Services
	db                             *sql.DB
	owner, writer, admin, outsider metaUser
	repo                           *model.Repository
}

func newR2Env(t *testing.T) r2Env {
	t.Helper()
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	user := func() metaUser {
		sfx := testutil.UniqueSuffix(t)
		id := testutil.SeedUser(t, db, sfx)
		name := "testuser_" + sfx
		return metaUser{id: id, name: name, token: makeJWT(t, id, name)}
	}
	e := r2Env{h: h, svc: svc, db: db, owner: user(), writer: user(), admin: user(), outsider: user()}
	testutil.SetPassword(t, db, e.owner.id, r2Password)
	repo, err := svc.Repo.Create(context.Background(), e.owner.id, e.owner.name, "proj", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	e.repo = repo
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'writer')`, repo.ID, e.writer.id)
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'admin')`, repo.ID, e.admin.id)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE id = $1`, repo.ID) })
	return e
}

func (e r2Env) api(suffix string) string {
	return "/api/repos/" + e.owner.name + "/proj" + suffix
}

func (e r2Env) form(t *testing.T, method, target, token string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := browserRequest(method, target, token, form)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	return serve(e.h, req)
}

func (e r2Env) jsonBody(t *testing.T, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := jsonPost(target, token, body)
	req.Method = method
	return serve(e.h, req)
}

func r2DecodeJSON[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return v
}

func (e r2Env) hasRef(t *testing.T, kind, name string) bool {
	t.Helper()
	result, err := e.svc.Code.ListRefsPeeled(e.owner.name, "proj", e.repo.DefaultBranch)
	if err != nil {
		t.Fatalf("ListRefsPeeled: %v", err)
	}
	if kind == "tag" {
		for _, tag := range result.Tags {
			if tag.Name == name {
				return true
			}
		}
		return false
	}
	for _, b := range result.Branches {
		if b.Name == name {
			return true
		}
	}
	return false
}

func r2ErrorMessage(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	m := r2DecodeJSON[map[string]string](t, rr)
	return m["error"]
}

// exec runs a statement whose only argument is the repository id.
func (e r2Env) exec(t *testing.T, query string) {
	t.Helper()
	testutil.Exec(t, e.db, query, e.repo.ID)
}
