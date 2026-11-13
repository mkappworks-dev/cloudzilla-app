package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type queryCounter struct{ n atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// countingRouter serves the env's repo through a connection that counts every query it sends.
func (e *actionsEnv) countingRouter(t *testing.T) (http.Handler, *queryCounter) {
	t.Helper()
	cfg, err := pgx.ParseConfig(os.Getenv("TEST_DATABASE_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	counter := &queryCounter{}
	cfg.Tracer = counter
	db := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { db.Close() })
	app := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost:8080"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:    config.GitConfig{ReposRoot: filepath.Dir(filepath.Dir(e.r.gitDir))},
		CI:     config.DefaultCI(),
	}
	h, err := router.New(service.New(store.New(db), app), app, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	return h, counter
}

func getVia(h http.Handler, path, token string) string {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Body.String()
}

// commitsOnMain adds n commits to main and returns every commit on it, newest first.
func (e *actionsEnv) commitsOnMain(t *testing.T, n int) []string {
	t.Helper()
	code := service.NewCodeService(config.GitConfig{ReposRoot: filepath.Dir(filepath.Dir(e.r.gitDir))})
	for i := 0; i < n; i++ {
		if _, err := code.CommitFile(e.r.owner.name, e.r.name, "main", fmt.Sprintf("f%02d.txt", i), []byte("x\n"), raceAuthor, fmt.Sprintf("commit %02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	log, err := code.GetCommits(e.r.owner.name, e.r.name, "main", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	shas := make([]string, 0, len(log.Commits))
	for _, c := range log.Commits {
		shas = append(shas, c.FullHash)
	}
	return shas
}

// reportOnRun posts a status per SHA owned by the env's first Run.
func (e *actionsEnv) reportOnRun(t *testing.T, state model.CommitStatusState, shas ...string) {
	t.Helper()
	var runID int64
	if err := e.db.QueryRow(`SELECT id FROM workflow_runs WHERE repo_id = $1 AND number = 1`, e.r.id).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	for _, sha := range shas {
		testutil.Exec(t, e.db, `INSERT INTO commit_statuses (repo_id, sha, context, state, creator_id, run_id)
			VALUES ($1, $2, 'ci/test', $3, $4, $5) ON CONFLICT (repo_id, sha, context) DO UPDATE SET state = $3, run_id = $5`,
			e.r.id, sha, string(state), e.r.owner.id, runID)
	}
}

func TestCommitList_StatusBadgesLinkToTheRunAndAddNoPerRowQueries(t *testing.T) {
	e := newActionsEnv(t)
	shas := e.commitsOnMain(t, 30)[:30]
	h, queries := e.countingRouter(t)
	path := e.r.path + "/commits/main"

	getVia(h, path, e.reader.token) // warm the first-use lookups
	queries.n.Store(0)
	getVia(h, path, e.reader.token)
	before := queries.n.Load()

	e.reportOnRun(t, model.CommitStatusSuccess, shas...)
	queries.n.Store(0)
	page := getVia(h, path, e.reader.token)
	after := queries.n.Load()

	if got := strings.Count(page, `title="Succeeded — latest run"`); got != 30 {
		t.Errorf("badges = %d, want 30", got)
	}
	if got := strings.Count(page, `href="`+e.base()+`/runs/1"`); got < 30 {
		t.Errorf("links to the latest run = %d, want 30", got)
	}
	if after != before {
		t.Errorf("30 commits with statuses sent %d queries, 30 without sent %d: the badges must be batched", after, before)
	}
}

func TestCommitList_FailedAndPendingStatesShowTheirOwnBadge(t *testing.T) {
	e := newActionsEnv(t)
	shas := e.commitsOnMain(t, 2)
	e.reportOnRun(t, model.CommitStatusFailure, shas[0])
	e.reportOnRun(t, model.CommitStatusPending, shas[1])
	h, _ := e.countingRouter(t)

	page := getVia(h, e.r.path+"/commits/main", e.reader.token)

	mustContain(t, page, `title="Failed — latest run"`, `title="Running — latest run"`)
}

func TestTreeAndPullHeader_ShowTheCombinedStateOfTheirCommit(t *testing.T) {
	e := newActionsEnv(t)
	shas := e.commitsOnMain(t, 1)
	e.reportOnRun(t, model.CommitStatusFailure, shas...)
	h, _ := e.countingRouter(t)

	tree := getVia(h, e.r.path+"/tree/main", e.reader.token)

	mustContain(t, tree, `title="Failed — latest run"`, `href="`+e.base()+`/runs/1"`)
}

func TestPullHeader_ShowsTheHeadCommitsState(t *testing.T) {
	e := newActionsEnv(t)
	seedOpenPull(t, e.db, e.r.seededRepo)
	e.reportOnRun(t, model.CommitStatusFailure, e.r.featureTip.String())
	h, _ := e.countingRouter(t)

	for _, tab := range []string{"", "/commits", "/checks", "/files"} {
		page := getVia(h, e.r.path+"/pulls/1"+tab, e.reader.token)
		if !strings.Contains(page, `title="Failed — latest run"`) {
			t.Errorf("pull tab %q has no state beside the title", tab)
		}
	}
}

func (e *actionsEnv) checksTab(t *testing.T, token string) string {
	t.Helper()
	return e.get(t, e.r.path+"/pulls/1/checks", token, http.StatusOK)
}

func TestPullChecks_GroupsJobsUnderTheirRunAndKeepsExternalStatusesApart(t *testing.T) {
	e := newActionsEnv(t)
	seedOpenPull(t, e.db, e.r.seededRepo)
	head := e.r.featureTip.String()
	e.reportOnRun(t, model.CommitStatusSuccess, head)
	testutil.Exec(t, e.db, `INSERT INTO commit_statuses (repo_id, sha, context, state, description, creator_id)
		VALUES ($1, $2, 'buildkite/linux', 'failure', 'Build 7 failed', $3)`, e.r.id, head, e.r.owner.id)

	page := e.checksTab(t, e.reader.token)

	mustContain(t, page, `aria-label="Jobs in CI #1"`, "ci/test", `href="`+e.base()+`/runs/1"`, "View run",
		"Other statuses", "buildkite/linux", "Build 7 failed")
	if strings.Index(page, "ci/test") > strings.Index(page, "buildkite/linux") {
		t.Error("the Run's Jobs must come before the other statuses")
	}
	mustNotContain(t, page, "Approve")
}

func TestPullChecks_ApproveShowsOnlyForAWriterOnAWaitingRun(t *testing.T) {
	e := newActionsEnv(t)
	seedOpenPull(t, e.db, e.r.seededRepo)
	e.reportOnRun(t, model.CommitStatusPending, e.r.featureTip.String())
	testutil.Exec(t, e.db, `UPDATE workflow_runs SET status = 'waiting' WHERE repo_id = $1 AND number = 1`, e.r.id)

	writer := e.checksTab(t, e.writer.token)
	reader := e.checksTab(t, e.reader.token)

	mustContain(t, writer, "Approve", `action="`+e.base()+`/runs/1/approve"`, "Approval required")
	mustContain(t, reader, "Approval required")
	mustNotContain(t, reader, `/approve"`)

	testutil.Exec(t, e.db, `UPDATE workflow_runs SET status = 'queued' WHERE repo_id = $1 AND number = 1`, e.r.id)
	mustNotContain(t, e.checksTab(t, e.writer.token), `/approve"`)
}
