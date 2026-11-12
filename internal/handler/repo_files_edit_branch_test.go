package handler_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func (r editRepo) branchExists(branch string) bool {
	_, err := r.git.Reference(plumbing.NewBranchReferenceName(branch), true)
	return err == nil
}

func (r editRepo) postBranchEdit(t *testing.T, extra url.Values) (rrCode int, location, body string) {
	t.Helper()
	form := url.Values{
		"path": {"a.txt"}, "content": {crlf("proposed\n")}, "blob_sha": {r.blobSHA(t, "a.txt")},
		"target": {"branch"},
	}
	for k, v := range extra {
		form[k] = v
	}
	rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/a.txt", form, false)
	return rr.Code, rr.Header().Get("Location"), rr.Body.String()
}

func TestEditFile_CommitsToANewBranchAndOpensThePullRequestForm(t *testing.T) {
	r := seedEditRepo(t)
	tip := branchHash(t, r.git, "main")
	code, loc, body := r.postBranchEdit(t, url.Values{"new_branch": {"fix-typo"}})
	if want := r.path + "/pulls/new?base=main&head=fix-typo"; code != http.StatusSeeOther || loc != want {
		t.Fatalf("want 303 to %s, got %d %q: %.300s", want, code, loc, body)
	}
	if got := branchHash(t, r.git, "main"); got != tip {
		t.Errorf("main = %s, want it left at %s", got, tip)
	}
	f, ok := r.file(t, "fix-typo", "a.txt")
	if !ok {
		t.Fatal("a.txt missing on fix-typo")
	}
	if got, _ := f.Contents(); got != "proposed\n" {
		t.Errorf("a.txt on fix-typo = %q", got)
	}
	commit, err := r.git.CommitObject(branchHash(t, r.git, "fix-typo"))
	if err != nil || len(commit.ParentHashes) != 1 || commit.ParentHashes[0] != tip {
		t.Errorf("fix-typo should sit one commit above the main tip %s: %v %v", tip, commit, err)
	}
}

func TestEditFile_PullRequestFormHonoursTheRedirectQuery(t *testing.T) {
	r := seedEditRepo(t)
	_, loc, _ := r.postBranchEdit(t, url.Values{"new_branch": {"feature/x y"}})
	if loc != "" {
		t.Fatalf("an invalid name must not redirect, got %q", loc)
	}
	_, loc, _ = r.postBranchEdit(t, url.Values{"new_branch": {"fix/typo"}})
	rr := send(t, r.api, http.MethodGet, r.owner.token, loc, nil, false)
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, `fix/typo`) {
		t.Fatalf("PR form: %d, lacks head fix/typo: %.300s", rr.Code, body)
	}
}

func TestEditFile_PrefilledBranchNameGetsACounter(t *testing.T) {
	r := seedEditRepo(t)
	name := r.owner.name + "-patch-1"
	if err := r.code.CreateBranch(r.owner.name, r.name, name, "main"); err != nil {
		t.Fatal(err)
	}
	code, loc, body := r.postBranchEdit(t, url.Values{"new_branch": {name}, "suggested_branch": {name}})
	want := r.path + "/pulls/new?base=main&head=" + r.owner.name + "-patch-2"
	if code != http.StatusSeeOther || loc != want {
		t.Fatalf("want 303 to %s, got %d %q: %.300s", want, code, loc, body)
	}
	if !r.branchExists(r.owner.name + "-patch-2") {
		t.Error("the renumbered branch was not created")
	}
}

func TestEditFile_TypedBranchNameThatExistsIsRefusedInline(t *testing.T) {
	r := seedEditRepo(t)
	code, _, body := r.postBranchEdit(t, url.Values{"new_branch": {"main"}, "suggested_branch": {"alice-patch-1"}})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %.300s", code, body)
	}
	for _, want := range []string{"A branch named main already exists", `id="ef-branch-error"`, `value="main"`, "proposed"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

func TestEditFile_InvalidBranchNamesAreRefusedInline(t *testing.T) {
	for _, name := range []string{"", "-x", "HEAD", "a..b", "x.lock"} {
		t.Run(name, func(t *testing.T) {
			r := seedEditRepo(t)
			tip := branchHash(t, r.git, "main")
			code, _, body := r.postBranchEdit(t, url.Values{"new_branch": {name}})
			if code != http.StatusUnprocessableEntity || !strings.Contains(body, `id="ef-branch-error"`) {
				t.Fatalf("want 422 with the inline error, got %d: %.300s", code, body)
			}
			if got := branchHash(t, r.git, "main"); got != tip {
				t.Errorf("main moved")
			}
		})
	}
}

func TestEditFile_StaleFileToANewBranchGoesThroughTheConflictFlow(t *testing.T) {
	r := seedEditRepo(t)
	sha := r.blobSHA(t, "a.txt")
	r.commitMain(t, "a.txt", "upstream\n")
	code, _, body := r.postBranchEdit(t, url.Values{"new_branch": {"fix-typo"}, "blob_sha": {sha}})
	if code != http.StatusConflict || !strings.Contains(body, "changed on main after you opened it") {
		t.Fatalf("want the 409 conflict page, got %d: %.300s", code, body)
	}
	if r.branchExists("fix-typo") {
		t.Error("a stale save created the branch")
	}
	if !strings.Contains(body, `value="fix-typo"`) {
		t.Error("the typed branch name was lost")
	}
}

func TestEditFile_ProtectedBranchDisablesTheDirectChoice(t *testing.T) {
	r := seedEditRepo(t)
	r.requirePullRequest(t, "main")
	rr := send(t, r.api, http.MethodGet, r.owner.token, r.path+"/edit/main/a.txt", nil, false)
	body := rr.Body.String()
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	if !strings.Contains(body, "pull request required") {
		t.Error("page doesn't give the reason")
	}
	if !strings.Contains(body, `value="direct"`) || !strings.Contains(body, "disabled") {
		t.Error("direct radio isn't disabled")
	}

	tip := branchHash(t, r.git, "main")
	code, loc, b := r.postBranchEdit(t, url.Values{"new_branch": {"fix-typo"}})
	if code != http.StatusSeeOther || !strings.Contains(loc, "head=fix-typo") {
		t.Fatalf("the new-branch choice should work on a protected branch, got %d %q: %.300s", code, loc, b)
	}
	if got := branchHash(t, r.git, "main"); got != tip {
		t.Error("main moved")
	}

	// A forged direct choice is still refused server-side.
	rr = send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/a.txt", url.Values{
		"path": {"a.txt"}, "content": {crlf("x\n")}, "blob_sha": {r.blobSHA(t, "a.txt")}, "target": {"direct"},
	}, false)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("direct commit to a protected branch: want 422, got %d", rr.Code)
	}
}

func TestEditFile_NewBranchNameMatchingAProtectionRuleIsRefused(t *testing.T) {
	r := seedEditRepo(t)
	r.requirePullRequest(t, "release/*")
	code, _, body := r.postBranchEdit(t, url.Values{"new_branch": {"release/1"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, `id="ef-branch-error"`) {
		t.Fatalf("want an inline 422, got %d: %.300s", code, body)
	}
	if r.branchExists("release/1") {
		t.Error("branch created")
	}
}

func TestEditFile_PageSuggestsAPatchBranchAndFormKeepsTheChoice(t *testing.T) {
	r := seedEditRepo(t)
	rr := send(t, r.api, http.MethodGet, r.owner.token, r.path+"/edit/main/a.txt", nil, false)
	want := `value="` + r.owner.name + `-patch-1"`
	if !strings.Contains(rr.Body.String(), want) {
		t.Errorf("page lacks the prefilled name %s", want)
	}
	code, _, body := r.postBranchEdit(t, url.Values{"new_branch": {"main"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "{ target: &#39;branch&#39; }") {
		t.Errorf("re-rendered form should keep the new-branch choice: %d", code)
	}
}
