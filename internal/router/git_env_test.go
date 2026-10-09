package router_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newGitMetaEnv is a metaEnv over a repo with a real bare git repository and a README commit.
func newGitMetaEnv(t *testing.T) metaEnv {
	t.Helper()
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	user := func() metaUser {
		sfx := testutil.UniqueSuffix(t)
		id := testutil.SeedUser(t, db, sfx)
		name := "testuser_" + sfx
		return metaUser{id: id, name: name, token: makeJWT(t, id, name)}
	}
	e := metaEnv{h: h, svc: svc, db: db, owner: user(), writer: user(), outsider: user(), repoName: "gitrepo"}
	repo, err := svc.Repo.Create(context.Background(), e.owner.id, e.owner.name, e.repoName, "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	e.repoID = repo.ID
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'writer')`, e.repoID, e.writer.id)
	return e
}

func (e metaEnv) archive(t *testing.T) {
	t.Helper()
	testutil.Exec(t, e.db, `UPDATE repositories SET is_archived = TRUE WHERE id = $1`, e.repoID)
}

func (e metaEnv) makePrivate(t *testing.T) {
	t.Helper()
	testutil.Exec(t, e.db, `UPDATE repositories SET private = TRUE WHERE id = $1`, e.repoID)
}

func (e metaEnv) seedIssue(t *testing.T, title, state string) (id int64, number int) {
	t.Helper()
	err := e.db.QueryRow(`INSERT INTO issues (repo_id, number, author_id, title, state)
		VALUES ($1, (SELECT COALESCE(MAX(number), 0) + 1 FROM issues WHERE repo_id = $1), $2, $3, $4) RETURNING id, number`,
		e.repoID, e.owner.id, title, state).Scan(&id, &number)
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return id, number
}

func (e metaEnv) seedPull(t *testing.T, title, state string) (id int64, number int) {
	t.Helper()
	err := e.db.QueryRow(`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		VALUES ($1, (SELECT COALESCE(MAX(number), 0) + 1 FROM pull_requests WHERE repo_id = $1), $2, $3, $4, 'feature', 'main') RETURNING id, number`,
		e.repoID, e.owner.id, title, state).Scan(&id, &number)
	if err != nil {
		t.Fatalf("seed pull: %v", err)
	}
	return id, number
}

func (e metaEnv) page(t *testing.T, target, token string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, metaReq{method: "GET", target: target, token: token, htmx: htmx})
}

func (e metaEnv) pagePath(format string, args ...any) string {
	return "/" + e.owner.name + "/" + e.repoName + fmt.Sprintf(format, args...)
}
