package router_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type metaUser struct {
	id    int64
	name  string
	token string
}

// metaEnv is a public repo with issue #1, pull #1, discussion #1 and a comment,
// plus an owner, a writer collaborator and an outsider who can only read.
type metaEnv struct {
	h                       http.Handler
	svc                     *service.Services
	db                      *sql.DB
	owner, writer, outsider metaUser
	repoID                  int64
	repoName                string
	issueID, pullID         int64
	discussionID            int64
	commentID               int64
}

func newMetaEnv(t *testing.T) metaEnv {
	t.Helper()
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	user := func() metaUser {
		sfx := testutil.UniqueSuffix(t)
		id := testutil.SeedUser(t, db, sfx)
		name := "testuser_" + sfx
		return metaUser{id: id, name: name, token: makeJWT(t, id, name)}
	}
	e := metaEnv{h: h, svc: svc, db: db, owner: user(), writer: user(), outsider: user()}
	sfx := testutil.UniqueSuffix(t)
	e.repoID = testutil.SeedRepo(t, db, e.owner.id, e.owner.name, sfx)
	e.repoName = "testrepo_" + sfx
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'writer')`, e.repoID, e.writer.id)
	scan := func(q string, args ...any) int64 {
		var id int64
		if err := db.QueryRow(q, args...).Scan(&id); err != nil {
			t.Fatalf("seed: %v", err)
		}
		return id
	}
	e.issueID = scan(`INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 1, $2, 'issue') RETURNING id`, e.repoID, e.owner.id)
	e.pullID = scan(`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch) VALUES ($1, 1, $2, 'p', 'open', 'feature', 'main') RETURNING id`, e.repoID, e.owner.id)
	e.discussionID = scan(`INSERT INTO discussions (repo_id, category_id, number, title, author_id, author_name) VALUES ($1, (SELECT MIN(id) FROM discussion_categories), 1, 'idea', $2, $3) RETURNING id`, e.repoID, e.owner.id, e.owner.name)
	e.commentID = scan(`INSERT INTO comments (repo_id, issue_id, author_id, body) VALUES ($1, $2, $3, 'hello') RETURNING id`, e.repoID, e.issueID, e.owner.id)
	return e
}

func (e metaEnv) path(format string, args ...any) string {
	return fmt.Sprintf("/api/repos/%s/%s", e.owner.name, e.repoName) + fmt.Sprintf(format, args...)
}

type metaReq struct {
	method, target, token string
	form                  url.Values
	json                  string
	htmx                  bool
}

func (e metaEnv) do(t *testing.T, m metaReq) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if m.json != "" {
		req = jsonPost(m.target, m.token, m.json)
		req.Method = m.method
	} else {
		req = browserRequest(m.method, m.target, m.token, m.form)
	}
	if m.htmx {
		req.Header.Set("HX-Request", "true")
	}
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	return rr
}

func wantStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Fatalf("status = %d, want %d; body: %.300s", rr.Code, want, rr.Body.String())
	}
}

func (e metaEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func bodyHas(t *testing.T, rr *httptest.ResponseRecorder, sub string) {
	t.Helper()
	if !strings.Contains(rr.Body.String(), sub) {
		t.Errorf("body lacks %q: %.400s", sub, rr.Body.String())
	}
}
