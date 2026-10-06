package handler_test

// Integration tests: a pull mirror is read-only and says so. All tests
// require TEST_DATABASE_DSN and skip otherwise.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestMirror_GitHTTPPush_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	app := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, false)
	makeMirror(t, db, repo.id)
	owner := gitCaller{name: "owner", basic: seedPAT(t, db, repo.owner.id, model.ScopeRepoWrite)}

	for _, path := range []string{"/info/refs?service=git-receive-pack", "/git-receive-pack"} {
		t.Run(path, func(t *testing.T) {
			method := http.MethodGet
			if path == "/git-receive-pack" {
				method = http.MethodPost
			}
			rr := requestGit(app, method, repo.path+path, owner)

			if want := "Repository is a mirror and is read-only.\n"; rr.Code != http.StatusForbidden || rr.Body.String() != want {
				t.Errorf("want 403 %q, got %d %q", want, rr.Code, rr.Body.String())
			}
		})
	}
}

const pullIntoMirrorMsg = "Pull mirrors are read-only; open the pull request upstream."

func TestMirror_CreatePull_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	makeMirror(t, db, r.id)

	rr := requestAPIBody(api, http.MethodPost, "/api/repos"+r.path+"/pulls", r.owner.token,
		`{"title":"change","head_branch":"feature","base_branch":"main"}`)
	if want := `{"error":"` + pullIntoMirrorMsg + `"}` + "\n"; rr.Code != http.StatusUnprocessableEntity || rr.Body.String() != want {
		t.Errorf("API: want 422 %q, got %d %q", want, rr.Code, rr.Body.String())
	}

	rr = postForm(t, api, r.owner.token, r.path+"/pulls/new",
		map[string][]string{"title": {"change"}, "head_branch": {"feature"}, "base_branch": {"main"}})
	if !strings.Contains(rr.Body.String(), pullIntoMirrorMsg) {
		t.Errorf("form: got %d, want the page to say %q", rr.Code, pullIntoMirrorMsg)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM pull_requests WHERE repo_id = $1`, r.id).Scan(&n)
	if n != 0 {
		t.Errorf("%d pull requests created on a mirror", n)
	}

	page := requestAPI(api, http.MethodGet, r.path+"/pulls", r.owner.token)
	if strings.Contains(page.Body.String(), r.path+"/pulls/new") {
		t.Error("the pulls page offers New pull request on a mirror")
	}
}

func TestMirror_Banner(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	makeMirror(t, db, r.id)
	testutil.Exec(t, db, `UPDATE repo_mirrors SET last_sync_at = NOW() - interval '5 minutes', last_success_at = NOW() - interval '5 minutes' WHERE repo_id = $1`, r.id)
	stranger := seedSignedInUser(t, db)

	rr := requestAPI(api, http.MethodGet, r.path, r.owner.token)
	body := rr.Body.String()
	for _, want := range []string{"Mirror of", "example.com/upstream", "synced 5 minutes ago", ">Mirror</span>", `hx-post="/api/repos` + r.path + `/mirror/sync"`} {
		if !strings.Contains(body, want) {
			t.Errorf("owner's page lacks %q", want)
		}
	}
	if strings.Contains(body, "last sync failed") {
		t.Error("a healthy mirror shows a failure")
	}
	if strings.Contains(requestAPI(api, http.MethodGet, r.path, stranger.token).Body.String(), "/mirror/sync") {
		t.Error("a reader is offered Sync now")
	}

	testutil.Exec(t, db, `UPDATE repo_mirrors SET last_error = 'The source rejected the token.', consecutive_failures = 1,
		last_sync_at = NOW() - interval '2 hours', next_sync_at = NOW() + interval '6 hours' WHERE repo_id = $1`, r.id)
	body = requestAPI(api, http.MethodGet, r.path, r.owner.token).Body.String()
	for _, want := range []string{"last sync failed 2 hours ago", "The source rejected the token.", "Next try in 6 hours", `href="` + r.path + `/settings#mirror"`} {
		if !strings.Contains(body, want) {
			t.Errorf("failing mirror's page lacks %q", want)
		}
	}
}

func TestMirror_SyncNow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, false)
	makeMirror(t, db, repo.id)
	stranger := seedSignedInUser(t, db)

	if rr := requestAPI(api, http.MethodPost, "/api/repos"+repo.path+"/mirror/sync", stranger.token); rr.Code != http.StatusForbidden {
		t.Errorf("reader: want 403, got %d %s", rr.Code, rr.Body.String())
	}
	rr := requestAPI(api, http.MethodPost, "/api/repos"+repo.path+"/mirror/sync", repo.owner.token)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("owner: want 202, got %d %s", rr.Code, rr.Body.String())
	}
	var due bool
	_ = db.QueryRow(`SELECT next_sync_at <= NOW() FROM repo_mirrors WHERE repo_id = $1`, repo.id).Scan(&due)
	if !due {
		t.Error("Sync now didn't make the mirror due")
	}

	plain := seedOwnedRepo(t, db, false)
	if rr := requestAPI(api, http.MethodPost, "/api/repos"+plain.path+"/mirror/sync", plain.owner.token); rr.Code != http.StatusNotFound {
		t.Errorf("not a mirror: want 404, got %d", rr.Code)
	}
}

func TestMirror_ForkIsWritable(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	makeMirror(t, db, r.id)
	forker := seedSignedInUser(t, db)

	rr := requestAPIBody(api, http.MethodPost, "/api/repos"+r.path+"/fork", forker.token, `{}`)
	if rr.Code != http.StatusCreated && rr.Code != http.StatusOK {
		t.Fatalf("fork: got %d %s", rr.Code, rr.Body.String())
	}
	var fork struct{ Owner, Name string }
	if err := json.Unmarshal(rr.Body.Bytes(), &fork); err != nil {
		t.Fatalf("decode fork: %v", err)
	}
	var mirrored bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM repo_mirrors m JOIN repositories r ON r.id = m.repo_id
		WHERE r.owner_name = $1 AND r.name = $2)`, fork.Owner, fork.Name).Scan(&mirrored); err != nil || mirrored {
		t.Errorf("fork %s/%s: mirror row = %v, %v; want a plain repo", fork.Owner, fork.Name, mirrored, err)
	}
	if rr := postForm(t, api, forker.token, "/api/repos/"+fork.Owner+"/"+fork.Name+"/branches",
		map[string][]string{"name": {"mine"}}); rr.Code != http.StatusCreated {
		t.Errorf("branch on the fork: want 201, got %d %s", rr.Code, rr.Body.String())
	}
}
