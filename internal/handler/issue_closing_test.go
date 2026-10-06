package handler_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// closingRepo is a raceRepo with a topic branch one commit, commitMsg, ahead of
// main, so every merge strategy applies, and an issue reporter other than the
// owner, so closes notify someone.
type closingRepo struct {
	raceRepo
	topicTip plumbing.Hash
	reporter signedInUser
}

func seedClosingRepo(t *testing.T, db *sql.DB, reposRoot, commitMsg string) closingRepo {
	t.Helper()
	r := closingRepo{raceRepo: seedRaceRepo(t, db, reposRoot), reporter: seedSignedInUser(t, db)}
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	if err := code.CreateBranch(r.owner.name, r.name, "topic", "main"); err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if commitMsg == "" {
		commitMsg = "Add c.txt"
	}
	if err := code.CommitFile(r.owner.name, r.name, "topic", "c.txt", []byte("c\n"), raceAuthor, commitMsg); err != nil {
		t.Fatalf("commit on topic: %v", err)
	}
	r.topicTip = branchHash(t, r.git, "topic")
	return r
}

func seedRepoIssue(t *testing.T, db *sql.DB, repoID, authorID int64, number int) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(
		`INSERT INTO issues (repo_id, number, author_id, title, body, state) VALUES ($1, $2, $3, 'bug', '', 'open') RETURNING id`,
		repoID, number, authorID,
	).Scan(&id); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return id
}

func issueStateByID(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var state string
	if err := db.QueryRow(`SELECT state FROM issues WHERE id = $1`, id).Scan(&state); err != nil {
		t.Fatalf("issue state: %v", err)
	}
	return state
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func keywordSources(t *testing.T, db *sql.DB, pullID int64) map[int64]string {
	t.Helper()
	rows, err := db.Query(`SELECT issue_id, source FROM pull_issue_links WHERE pull_id = $1`, pullID)
	if err != nil {
		t.Fatalf("links: %v", err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var src string
		if err := rows.Scan(&id, &src); err != nil {
			t.Fatal(err)
		}
		out[id] = src
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func createPull(t *testing.T, api http.Handler, r raceRepo, token, title, body, base string) int64 {
	t.Helper()
	rr := requestAPIBody(api, http.MethodPost, "/api/repos"+r.path+"/pulls", token,
		fmt.Sprintf(`{"title":%q,"body":%q,"head_branch":"topic","base_branch":%q}`, title, body, base))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create pull: %d %s", rr.Code, rr.Body.String())
	}
	var id int64
	if _, err := fmt.Sscanf(rr.Body.String()[strings.Index(rr.Body.String(), `"id":`)+5:], "%d", &id); err != nil {
		t.Fatalf("pull id in %s: %v", rr.Body.String(), err)
	}
	return id
}

func TestMergePull_ClosesLinkedAndReferencedIssues(t *testing.T) {
	for _, strategy := range []string{"ff", "merge", "squash"} {
		t.Run(strategy, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			api := newAPIRouterAt(t, db, reposRoot)
			r := seedClosingRepo(t, db, reposRoot, "Make it work\n\nCloses #3")
			other := seedOwnedRepo(t, db, false)
			testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'writer')`, other.id, r.owner.id)

			byKeyword := seedRepoIssue(t, db, r.id, r.reporter.id, 1)
			byHand := seedRepoIssue(t, db, r.id, r.reporter.id, 2)
			byCommit := seedRepoIssue(t, db, r.id, r.reporter.id, 3)
			untouched := seedRepoIssue(t, db, r.id, r.reporter.id, 4)
			crossRepo := seedRepoIssue(t, db, other.id, other.owner.id, 1)

			pullID := createPull(t, api, r.raceRepo, r.owner.token, "Fix things", "Fixes #1\nand fixes "+strings.TrimPrefix(other.path, "/")+"#1", "main")
			if rr := requestAPIBody(api, http.MethodPost, "/api/repos"+r.path+"/pulls/1/linked-issues/2", r.owner.token, ""); rr.Code != http.StatusNoContent {
				t.Fatalf("link #2: %d %s", rr.Code, rr.Body.String())
			}

			rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token,
				`{"state":"merged","merge_strategy":"`+strategy+`"}`)
			if rr.Code != http.StatusOK {
				t.Fatalf("merge: %d %s", rr.Code, rr.Body.String())
			}

			for name, id := range map[string]int64{"keyword": byKeyword, "manual": byHand, "commit": byCommit, "cross-repo": crossRepo} {
				if got := issueStateByID(t, db, id); got != "closed" {
					t.Errorf("%s issue = %q, want closed", name, got)
				}
				if n := countRows(t, db, `SELECT COUNT(*) FROM issue_events WHERE issue_id = $1 AND event_type = 'closed' AND pull_id = $2 AND actor_id = $3`, id, pullID, r.owner.id); n != 1 {
					t.Errorf("%s issue has %d closed events by the pull, want 1", name, n)
				}
			}
			if got := issueStateByID(t, db, untouched); got != "open" {
				t.Errorf("unreferenced issue = %q, want open", got)
			}
			if n := countRows(t, db, `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = 'issue_closed' AND repo_id = $2`, r.reporter.id, r.id); n != 3 {
				t.Errorf("reporter got %d issue_closed notifications, want 3", n)
			}
			if n := countRows(t, db, `SELECT COUNT(*) FROM events WHERE actor_id = $1 AND event_type = 'issue_closed' AND repo_id = $2`, r.owner.id, other.id); n != 1 {
				t.Errorf("cross-repo close recorded %d activity events, want 1", n)
			}
		})
	}
}

func TestMergePull_IntoOtherBranch_ClosesNothing(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedClosingRepo(t, db, reposRoot, "Closes #2")
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	if err := code.CreateBranch(r.owner.name, r.name, "release", "main"); err != nil {
		t.Fatalf("create release: %v", err)
	}
	linked := seedRepoIssue(t, db, r.id, r.reporter.id, 1)
	referenced := seedRepoIssue(t, db, r.id, r.reporter.id, 2)
	createPull(t, api, r.raceRepo, r.owner.token, "Backport", "Fixes #1", "release")

	rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token, `{"state":"merged","merge_strategy":"ff"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("merge: %d %s", rr.Code, rr.Body.String())
	}

	for name, id := range map[string]int64{"linked": linked, "referenced": referenced} {
		if got := issueStateByID(t, db, id); got != "open" {
			t.Errorf("%s issue = %q, want open", name, got)
		}
	}
}

func TestPullText_KeepsKeywordLinksInStep(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedClosingRepo(t, db, reposRoot, "")
	i1 := seedRepoIssue(t, db, r.id, r.reporter.id, 1)
	i2 := seedRepoIssue(t, db, r.id, r.reporter.id, 2)
	i3 := seedRepoIssue(t, db, r.id, r.reporter.id, 3)
	hidden := seedOwnedRepo(t, db, true)
	seedRepoIssue(t, db, hidden.id, hidden.owner.id, 1)

	pullID := createPull(t, api, r.raceRepo, r.owner.token, "Fixes #1", "resolves #2, and fixes "+strings.TrimPrefix(hidden.path, "/")+"#1", "main")
	if got, want := keywordSources(t, db, pullID), map[int64]string{i1: "keyword", i2: "keyword"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("after create = %v, want %v (an unreadable repo's issue stays unlinked)", got, want)
	}

	if rr := requestAPIBody(api, http.MethodPost, "/api/repos"+r.path+"/pulls/1/linked-issues/2", r.owner.token, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("link #2: %d", rr.Code)
	}
	if rr := requestAPIBody(api, http.MethodPost, "/api/repos"+r.path+"/pulls/1/linked-issues/3", r.owner.token, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("link #3: %d", rr.Code)
	}
	if rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token, `{"body":"no references"}`); rr.Code != http.StatusOK {
		t.Fatalf("edit body: %d %s", rr.Code, rr.Body.String())
	}
	if got, want := keywordSources(t, db, pullID), map[int64]string{i1: "keyword", i2: "manual", i3: "manual"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("after body edit = %v, want %v", got, want)
	}
	if rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token, `{"title":"Tidy"}`); rr.Code != http.StatusOK {
		t.Fatalf("edit title: %d %s", rr.Code, rr.Body.String())
	}
	if got, want := keywordSources(t, db, pullID), map[int64]string{i2: "manual", i3: "manual"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("after title edit = %v, want %v", got, want)
	}
}

func awaitPull(t *testing.T, db *sql.DB, pullID int64, query string, want any) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var got any
		if err := db.QueryRow(query, pullID).Scan(&got); err != nil {
			t.Fatalf("await pull: %v", err)
		}
		if fmt.Sprint(got) == fmt.Sprint(want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s = %v after 5s, want %v", query, got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func awaitIssueClosed(t *testing.T, db *sql.DB, issueID int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for issueStateByID(t, db, issueID) != "closed" {
		if time.Now().After(deadline) {
			t.Fatalf("issue %d still open after 5s", issueID)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAutoMerge_ActsAsTheArmingUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedClosingRepo(t, db, reposRoot, "Fixes #2")
	linked := seedRepoIssue(t, db, r.id, r.reporter.id, 1)
	referenced := seedRepoIssue(t, db, r.id, r.reporter.id, 2)
	pullID := createPull(t, api, r.raceRepo, r.owner.token, "Fix", "Fixes #1", "main")
	requireCIOnMain(t, db, r.id)
	if rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token, `{"auto_merge":"enable","auto_merge_strategy":"merge"}`); rr.Code != http.StatusOK {
		t.Fatalf("enable auto-merge: %d %s", rr.Code, rr.Body.String())
	}

	postStatus(t, api, r.raceRepo, r.topicTip, "success")

	// The merge goroutine closes the issues after it marks the pull merged.
	awaitIssueClosed(t, db, linked)
	awaitIssueClosed(t, db, referenced)
	for name, id := range map[string]int64{"linked": linked, "referenced": referenced} {
		if n := countRows(t, db, `SELECT COUNT(*) FROM issue_events WHERE issue_id = $1 AND event_type = 'closed' AND actor_id = $2 AND pull_id = $3`, id, r.owner.id, pullID); n != 1 {
			t.Errorf("%s issue: %d closed events by the arming user, want 1 (state %q)", name, n, issueStateByID(t, db, id))
		}
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM pull_events WHERE pull_id = $1 AND event_type = 'merged' AND actor_id = $2`, pullID, r.owner.id); n != 1 {
		t.Errorf("merged pull events by the arming user = %d, want 1", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM events WHERE repo_id = $1 AND event_type = 'pr_merged' AND actor_id = $2`, r.id, r.owner.id); n != 1 {
		t.Errorf("pr_merged activity events = %d, want 1", n)
	}
}

func TestAutoMerge_DisarmsWithoutAWritingArmingUser(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, db *sql.DB, r closingRepo, pullID int64)
	}{
		{"no arming user", func(t *testing.T, db *sql.DB, r closingRepo, pullID int64) {
			testutil.Exec(t, db, `UPDATE pull_requests SET auto_merge_by = NULL WHERE id = $1`, pullID)
		}},
		{"arming user lost write access", func(t *testing.T, db *sql.DB, r closingRepo, pullID int64) {
			testutil.Exec(t, db, `UPDATE pull_requests SET auto_merge_by = $2 WHERE id = $1`, pullID, r.reporter.id)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			api := newAPIRouterAt(t, db, reposRoot)
			r := seedClosingRepo(t, db, reposRoot, "")
			issue := seedRepoIssue(t, db, r.id, r.reporter.id, 1)
			pullID := createPull(t, api, r.raceRepo, r.owner.token, "Fix", "Fixes #1", "main")
			requireCIOnMain(t, db, r.id)
			if rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token, `{"auto_merge":"enable","auto_merge_strategy":"merge"}`); rr.Code != http.StatusOK {
				t.Fatalf("enable auto-merge: %d %s", rr.Code, rr.Body.String())
			}
			tc.setup(t, db, r, pullID)

			postStatus(t, api, r.raceRepo, r.topicTip, "success")

			awaitPull(t, db, pullID, `SELECT auto_merge_enabled FROM pull_requests WHERE id = $1`, false)
			if got := pullState(t, db, pullID); got != "open" {
				t.Errorf("pull = %q, want open", got)
			}
			if got := issueStateByID(t, db, issue); got != "open" {
				t.Errorf("issue = %q, want open", got)
			}
		})
	}
}

func TestGitReceivePack_FastForwardToDefaultBranch_ClosesIssues(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedClosingRepo(t, db, reposRoot, "")
	closes := seedRepoIssue(t, db, r.id, r.reporter.id, 1)
	onFeature := seedRepoIssue(t, db, r.id, r.reporter.id, 2)
	fix := testutil.WriteCommit(t, r.git.Storer, "Fixes #1", r.mainTip)
	featureFix := testutil.WriteCommit(t, r.git.Storer, "Fixes #2", r.featureTip)

	refs := receivePack(t, h, r.raceRepo,
		&packp.Command{Name: featureRef, Old: r.featureTip, New: featureFix},
		&packp.Command{Name: mainRef, Old: r.mainTip, New: fix})
	if refs[mainRef] != "ok" || refs[featureRef] != "ok" {
		t.Fatalf("push statuses = %v", refs)
	}

	awaitIssueClosed(t, db, closes)
	if n := countRows(t, db, `SELECT COUNT(*) FROM issue_events WHERE issue_id = $1 AND commit_sha = $2 AND actor_id = $3`, closes, fix.String(), r.owner.id); n != 1 {
		t.Errorf("closed events for the commit = %d, want 1", n)
	}
	if got := issueStateByID(t, db, onFeature); got != "open" {
		t.Errorf("issue named on another branch = %q, want open", got)
	}
}

func TestIssuePage_ShowsCloseAndReopenEvents(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedClosingRepo(t, db, reposRoot, "")
	other := seedOwnedRepo(t, db, false)
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'writer')`, r.id, other.owner.id)
	issue := seedRepoIssue(t, db, r.id, r.reporter.id, 1)
	testutil.Exec(t, db, `INSERT INTO pull_requests (repo_id, number, author_id, title, head_branch) VALUES ($1, 34, $2, 'p', 'x')`, other.id, other.owner.id)
	testutil.Exec(t, db,
		`INSERT INTO issue_events (issue_id, actor_id, actor_name, event_type, pull_id, source_repo_id, created_at)
		 SELECT $1, $2, $3, 'closed', p.id, $4, NOW() - interval '3 hours' FROM pull_requests p WHERE p.repo_id = $4 AND p.number = 34`,
		issue, other.owner.id, other.owner.name, other.id)
	testutil.Exec(t, db,
		`INSERT INTO issue_events (issue_id, actor_id, actor_name, event_type, created_at) VALUES ($1, $2, $3, 'reopened', NOW() - interval '2 hours')`,
		issue, r.owner.id, r.owner.name)
	testutil.Exec(t, db,
		`INSERT INTO issue_events (issue_id, actor_id, actor_name, event_type, commit_sha, source_repo_id, created_at)
		 VALUES ($1, $2, $3, 'closed', $4, $5, NOW() - interval '1 hour')`,
		issue, r.owner.id, r.owner.name, r.mainTip.String(), r.id)

	testutil.Exec(t, db, `UPDATE issues SET state = 'closed', closed_at = NOW() WHERE id = $1`, issue)
	rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/issues/1", r.owner.token, `{"state":"open"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("reopen: %d %s", rr.Code, rr.Body.String())
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM issue_events WHERE issue_id = $1 AND event_type = 'reopened' AND actor_id = $2`, issue, r.owner.id); n != 2 {
		t.Fatalf("reopened events = %d, want 2", n)
	}

	page := requestAPIBody(api, http.MethodGet, r.path+"/issues/1", r.owner.token, "")
	if page.Code != http.StatusOK {
		t.Fatalf("issue page: %d", page.Code)
	}
	body := page.Body.String()
	crossRef := strings.TrimPrefix(other.path, "/") + "#34"
	short := r.mainTip.String()[:7]
	order := []string{crossRef, "reopened this", short, "reopened this"}
	at := 0
	for _, want := range order {
		i := strings.Index(body[at:], want)
		if i < 0 {
			t.Fatalf("want %q after offset %d, in order %v; body:\n%s", want, at, order, body)
		}
		at += i + len(want)
	}
	assertContains(t, body, `href="`+other.path+`/pulls/34"`)
	assertContains(t, body, `href="`+r.path+`/commit/`+r.mainTip.String()+`"`)
}

func TestMergePull_ClosedPull_RefusedAndClosesNothing(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedClosingRepo(t, db, reposRoot, "")
	issue := seedRepoIssue(t, db, r.id, r.reporter.id, 1)
	pullID := createPull(t, api, r.raceRepo, r.owner.token, "Fix", "Fixes #1", "main")
	if rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token, `{"state":"closed"}`); rr.Code != http.StatusOK {
		t.Fatalf("close pull: %d %s", rr.Code, rr.Body.String())
	}

	rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token, `{"state":"merged","merge_strategy":"ff"}`)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("merge a closed pull: want 422, got %d %s", rr.Code, rr.Body.String())
	}
	if got := pullState(t, db, pullID); got != "closed" {
		t.Errorf("pull = %q, want closed", got)
	}
	if got := branchHash(t, r.git, "main"); got != r.mainTip {
		t.Errorf("main moved to %s", got)
	}
	if got := issueStateByID(t, db, issue); got != "open" {
		t.Errorf("issue = %q, want open", got)
	}
}
