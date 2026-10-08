package handler_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// requirePullRequest flags a rule on pattern in r's repo.
func (r editRepo) requirePullRequest(t *testing.T, pattern string) {
	t.Helper()
	testutil.Exec(t, r.db,
		`INSERT INTO branch_protections (repo_id, pattern, require_pull_request) VALUES ($1, $2, TRUE)`,
		r.id, pattern)
}

func TestRequirePullRequest_RefusesWebCommitsToTheBranch(t *testing.T) {
	r := seedEditRepo(t)
	r.requirePullRequest(t, "main")
	tip := branchHash(t, r.git, "main")
	sha := r.blobSHA(t, "a.txt")

	tests := []struct {
		name, path string
		form       url.Values
		hx         bool
	}{
		{"new file", "/new/main", url.Values{"path": {"n.txt"}, "content": {"n\n"}}, false},
		{"edit", "/edit/main/a.txt", url.Values{"path": {"a.txt"}, "content": {crlf("changed\n")}, "blob_sha": {sha}}, false},
		{"rename", "/edit/main/a.txt", url.Values{"path": {"b.txt"}, "content": {crlf("one\ntwo\n")}, "blob_sha": {sha}}, false},
		{"delete", "/delete/main/a.txt", url.Values{"blob_sha": {sha}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+tt.path, tt.form, tt.hx)
			if rr.Code != http.StatusUnprocessableEntity {
				t.Fatalf("want 422, got %d: %.300s", rr.Code, rr.Body.String())
			}
			if body := rr.Body.String(); !strings.Contains(body, "pull request required") || !strings.Contains(body, "main") {
				t.Errorf("body doesn't say the rule requires a pull request: %.300s", body)
			}
			if got := branchHash(t, r.git, "main"); got != tip {
				t.Errorf("main moved to %s, want it left at %s", got, tip)
			}
		})
	}
}

func TestRequirePullRequest_EditFormKeepsTheText(t *testing.T) {
	r := seedEditRepo(t)
	r.requirePullRequest(t, "main")
	rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/a.txt",
		url.Values{"path": {"a.txt"}, "content": {crlf("my unsaved words\n")}, "blob_sha": {r.blobSHA(t, "a.txt")}}, false)
	if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "my unsaved words") {
		t.Fatalf("want 422 with the form and the typed text, got %d: %.300s", rr.Code, rr.Body.String())
	}
}

func TestRequirePullRequest_LeavesOtherBranchesEditable(t *testing.T) {
	r := seedEditRepo(t)
	r.requirePullRequest(t, "main")
	rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/new/feature",
		url.Values{"path": {"n.txt"}, "content": {"n\n"}}, false)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303 for an unprotected branch, got %d: %.300s", rr.Code, rr.Body.String())
	}
}

func TestRequirePullRequest_RefusesAFastForwardPushAndAppliesTheRest(t *testing.T) {
	r := seedEditRepo(t)
	r.requirePullRequest(t, "main")
	tip := branchHash(t, r.git, "main") // seedEditRepo committed past r.mainTip

	refs := receivePack(t, r.api, r.raceRepo,
		&packp.Command{Name: mainRef, Old: tip, New: r.mainPushed},
		&packp.Command{Name: featureRef, Old: r.featureTip, New: r.featurePushed},
	)

	if got, want := refs[mainRef], `pull request required by branch protection: rule "main"`; got != want {
		t.Errorf("main status = %q, want %q", got, want)
	}
	if got := refs[featureRef]; got != "ok" {
		t.Errorf("feature status = %q, want ok", got)
	}
	assertRef(t, r.raceRepo, "main", tip)
	assertRef(t, r.raceRepo, "feature", r.featurePushed)
}

func TestRequirePullRequest_RuleFormSetsTheFlag(t *testing.T) {
	r := seedEditRepo(t)
	rules := "/api/repos/" + r.owner.name + "/" + r.name + "/branches/protections"

	rr := send(t, r.api, http.MethodPost, r.owner.token, rules,
		url.Values{"pattern": {"main"}, "require_pull_request": {"true"}}, false)
	if rr.Code != http.StatusCreated || !strings.Contains(rr.Body.String(), `"require_pull_request":true`) {
		t.Fatalf("create: %d %.300s", rr.Code, rr.Body.String())
	}
	var id int64
	if err := r.db.QueryRow(`SELECT id FROM branch_protections WHERE repo_id = $1`, r.id).Scan(&id); err != nil {
		t.Fatalf("find rule: %v", err)
	}

	ruleURL := rules + "/" + strconv.FormatInt(id, 10)
	if rr := send(t, r.api, http.MethodPatch, r.owner.token, ruleURL, url.Values{}, false); rr.Code != http.StatusNoContent {
		t.Fatalf("update: %d %.300s", rr.Code, rr.Body.String())
	}
	rr = send(t, r.api, http.MethodGet, r.owner.token, rules, nil, false)
	if !strings.Contains(rr.Body.String(), `"require_pull_request":false`) {
		t.Errorf("an update without the field should clear it: %.300s", rr.Body.String())
	}
}

// A push that creates a matching branch is refused, so the API can't create one either.
func TestRequirePullRequest_RefusesBranchCreates(t *testing.T) {
	r := seedEditRepo(t)
	r.requirePullRequest(t, "release/*")
	branches := "/api/repos/" + r.owner.name + "/" + r.name + "/branches"

	rr := send(t, r.api, http.MethodPost, r.owner.token, branches, url.Values{"name": {"release/1"}, "from": {"main"}}, false)
	if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "pull request required") {
		t.Fatalf("want 422 naming the requirement, got %d: %.300s", rr.Code, rr.Body.String())
	}
	if _, err := r.git.Reference("refs/heads/release/1", false); err == nil {
		t.Error("release/1 was created")
	}

	rr = send(t, r.api, http.MethodPost, r.owner.token, branches, url.Values{"name": {"topic"}, "from": {"main"}}, false)
	if rr.Code >= 300 {
		t.Errorf("an unmatched name should still be created, got %d: %.300s", rr.Code, rr.Body.String())
	}
}

func TestRequirePullRequest_RefusesBranchDeletes(t *testing.T) {
	r := seedEditRepo(t)
	r.requirePullRequest(t, "feature")
	apiPath := "/api/repos/" + r.owner.name + "/" + r.name + "/branches?name=feature"

	rr := send(t, r.api, http.MethodDelete, r.owner.token, apiPath, nil, false)
	if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "pull request required") {
		t.Fatalf("want 422 naming the requirement, got %d: %.300s", rr.Code, rr.Body.String())
	}
	if _, err := r.git.Reference("refs/heads/feature", false); err != nil {
		t.Errorf("feature was deleted: %v", err)
	}
}
