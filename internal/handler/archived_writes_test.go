package handler_test

// Integration tests: every path that writes git content refuses an archived
// repo. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const archivedJSON = `{"error":"repository is archived"}` + "\n"

func TestArchivedRepo_ContentWrites_Refused(t *testing.T) {
	tests := []struct {
		name     string
		wantBody string
		send     func(t *testing.T, api http.Handler, r raceRepo, suggestionID int64) *httptest.ResponseRecorder
	}{
		{"create branch", archivedJSON, func(t *testing.T, api http.Handler, r raceRepo, _ int64) *httptest.ResponseRecorder {
			return postForm(t, api, r.owner.token, "/api/repos"+r.path+"/branches", url.Values{"name": {"new"}})
		}},
		{"delete branch", archivedJSON, func(t *testing.T, api http.Handler, r raceRepo, _ int64) *httptest.ResponseRecorder {
			return requestDeleteBranch(api, r.seededRepo, "feature", false)
		}},
		{"create tag", archivedJSON, func(t *testing.T, api http.Handler, r raceRepo, _ int64) *httptest.ResponseRecorder {
			return postForm(t, api, r.owner.token, "/api/repos"+r.path+"/tags", url.Values{"name": {"v1"}})
		}},
		{"delete tag", archivedJSON, func(t *testing.T, api http.Handler, r raceRepo, _ int64) *httptest.ResponseRecorder {
			return requestAPI(api, http.MethodDelete, "/api/repos"+r.path+"/tags?name=v0", r.owner.token)
		}},
		{"merge pull", archivedJSON, func(t *testing.T, api http.Handler, r raceRepo, _ int64) *httptest.ResponseRecorder {
			return requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token,
				`{"state":"merged","merge_strategy":"merge"}`)
		}},
		{"apply suggestion", archivedJSON, func(t *testing.T, api http.Handler, r raceRepo, id int64) *httptest.ResponseRecorder {
			return requestAPI(api, http.MethodPost,
				"/api/repos"+r.path+"/pulls/1/line_comments/"+strconv.FormatInt(id, 10)+"/apply", r.owner.token)
		}},
		{"create release", archivedJSON, func(t *testing.T, api http.Handler, r raceRepo, _ int64) *httptest.ResponseRecorder {
			return requestAPIBody(api, http.MethodPost, "/api/repos"+r.path+"/releases", r.owner.token,
				`{"tag_name":"v2","name":"v2"}`)
		}},
		{"change default branch", "repository is archived\n", func(t *testing.T, api http.Handler, r raceRepo, _ int64) *httptest.ResponseRecorder {
			return postForm(t, api, r.owner.token, r.path+"/settings/general",
				url.Values{"description": {"d"}, "default_branch": {"feature"}})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			api := newAPIRouterAt(t, db, reposRoot)
			r := seedRaceRepo(t, db, reposRoot)
			if err := r.git.Storer.SetReference(plumbing.NewHashReference(plumbing.NewTagReferenceName("v0"), r.mainTip)); err != nil {
				t.Fatalf("seed tag: %v", err)
			}
			pullID := seedOpenPull(t, db, r.seededRepo)
			var suggestionID int64
			if err := db.QueryRow(
				`INSERT INTO pull_line_comments (pull_id, repo_id, author_id, author_name, path, diff_side, line, body, is_suggestion, suggestion_body)
				 VALUES ($1, $2, $3, $4, 'f.txt', 'right', 1, 'try this', true, 'uno') RETURNING id`,
				pullID, r.id, r.owner.id, r.owner.name,
			).Scan(&suggestionID); err != nil {
				t.Fatalf("seed suggestion: %v", err)
			}
			testutil.Exec(t, db, `UPDATE repositories SET is_archived = true, archived_at = NOW() WHERE id = $1`, r.id)
			before := allRefs(t, r)

			rr := tt.send(t, api, r, suggestionID)

			if rr.Code != http.StatusForbidden || rr.Body.String() != tt.wantBody {
				t.Errorf("want 403 %q, got %d %q", tt.wantBody, rr.Code, rr.Body.String())
			}
			after := allRefs(t, r)
			if len(after) != len(before) {
				t.Errorf("refs = %v, want %v", after, before)
			}
			for name, ref := range before {
				if after[name] != ref {
					t.Errorf("%s = %s, want %s", name, after[name], ref)
				}
			}
		})
	}
}

// allRefs maps every ref, HEAD included, to its hash or symbolic target.
func allRefs(t *testing.T, r raceRepo) map[string]string {
	t.Helper()
	iter, err := r.git.Storer.IterReferences()
	if err != nil {
		t.Fatalf("iterate refs: %v", err)
	}
	refs := map[string]string{}
	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		refs[ref.Name().String()] = ref.Strings()[1]
		return nil
	})
	head, err := r.git.Storer.Reference(plumbing.HEAD)
	if err != nil {
		t.Fatalf("read HEAD: %v", err)
	}
	refs["HEAD"] = head.Strings()[1]
	return refs
}

func TestArchivedRepo_ArmedAutoMerge_DoesNotMerge(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	pullID := seedOpenPull(t, db, r.seededRepo)
	author := seedSignedInUser(t, db)
	testutil.Exec(t, db, `UPDATE pull_requests SET author_id = $1 WHERE id = $2`, author.id, pullID)
	rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token,
		`{"auto_merge":"enable","auto_merge_strategy":"merge"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("enable auto-merge: want 200, got %d %s", rr.Code, rr.Body.String())
	}
	testutil.Exec(t, db, `UPDATE repositories SET is_archived = true, archived_at = NOW() WHERE id = $1`, r.id)

	// An approval starts tryAutoMerge in a goroutine after the response.
	rr = requestAPIBody(api, http.MethodPost, "/api/repos"+r.path+"/pulls/1/reviews", r.owner.token, `{"state":"approved"}`)
	if rr.Code != http.StatusCreated && rr.Code != http.StatusOK {
		t.Fatalf("approve: got %d %s", rr.Code, rr.Body.String())
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := pullState(t, db, pullID); got != "open" {
			t.Fatalf("pull state = %q on an archived repo, want open", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := branchHash(t, r.git, "main"); got != r.mainTip {
		t.Errorf("main = %s, want it left at %s", got, r.mainTip)
	}
}

func TestArchivedRepo_EnableAutoMerge_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	seedOpenPull(t, db, r.seededRepo)
	testutil.Exec(t, db, `UPDATE repositories SET is_archived = true, archived_at = NOW() WHERE id = $1`, r.id)

	rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token,
		`{"auto_merge":"enable","auto_merge_strategy":"merge"}`)

	if rr.Code != http.StatusForbidden || rr.Body.String() != archivedJSON {
		t.Errorf("want 403 %q, got %d %q", archivedJSON, rr.Code, rr.Body.String())
	}
}

func TestArchivedRepo_DescriptionStillEditable(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	testutil.Exec(t, db, `UPDATE repositories SET is_archived = true, archived_at = NOW() WHERE id = $1`, r.id)
	var branch string
	if err := db.QueryRow(`SELECT default_branch FROM repositories WHERE id = $1`, r.id).Scan(&branch); err != nil {
		t.Fatalf("read default branch: %v", err)
	}

	rr := postForm(t, api, r.owner.token, r.path+"/settings/general",
		url.Values{"description": {"kept for reference"}, "default_branch": {branch}})

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d %q", rr.Code, rr.Body.String())
	}
	var desc string
	if err := db.QueryRow(`SELECT description FROM repositories WHERE id = $1`, r.id).Scan(&desc); err != nil {
		t.Fatalf("read description: %v", err)
	}
	if desc != "kept for reference" {
		t.Errorf("description = %q, want it saved", desc)
	}
}

func TestArchivedRepo_PagesHideContentWriteControls(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "archived"}[archived], func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			api := newAPIRouterAt(t, db, reposRoot)
			r := seedRaceRepo(t, db, reposRoot)
			seedOpenPull(t, db, r.seededRepo)
			if archived {
				testutil.Exec(t, db, `UPDATE repositories SET is_archived = true, archived_at = NOW() WHERE id = $1`, r.id)
			}
			controls := []struct{ page, marker string }{
				{"/refs", `aria-label="Delete branch feature"`},
				{"/refs", `hx-post="/api/repos` + r.path + `/branches"`},
				{"/refs", `hx-post="/api/repos` + r.path + `/tags"`},
				{"/releases", `href="` + r.path + `/releases/new"`},
				{"/pulls/1", `merge_strategy`},
			}
			for _, c := range controls {
				rr := requestAPI(api, http.MethodGet, r.path+c.page, r.owner.token)
				if rr.Code != http.StatusOK {
					t.Fatalf("GET %s: %d", c.page, rr.Code)
				}
				if got := strings.Contains(rr.Body.String(), c.marker); got == archived {
					t.Errorf("%s: shows %s = %v, want %v", c.page, c.marker, got, !archived)
				}
			}
			rr := requestAPI(api, http.MethodGet, r.path+"/settings", r.owner.token)
			readOnly := regexp.MustCompile(`id="repo-default-branch"[^>]*readonly`).MatchString(rr.Body.String())
			if readOnly != archived {
				t.Errorf("default branch field readonly = %v, want %v", readOnly, archived)
			}
		})
	}
}
