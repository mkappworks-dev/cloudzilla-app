package router_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var (
	lineCommentRowRE = regexp.MustCompile(`(?s)<tr id="([^"]*)"[^>]*>(.*?)</tr>`)
	paragraphRE      = regexp.MustCompile(`(?s)<p>(.*?)</p>`)
)

type lineCommentRow struct {
	id     string
	bodies []string
}

func lineCommentRows(out string) []lineCommentRow {
	var rows []lineCommentRow
	for _, m := range lineCommentRowRE.FindAllStringSubmatch(out, -1) {
		row := lineCommentRow{id: html.UnescapeString(m[1])}
		for _, p := range paragraphRE.FindAllStringSubmatch(m[2], -1) {
			row.bodies = append(row.bodies, html.UnescapeString(p[1]))
		}
		rows = append(rows, row)
	}
	return rows
}

// Line 2 of f.txt is "old" in the base file and "new" in the head file, so a
// left-side and a right-side comment on line 2 belong under different rows, and
// the htmx answer to a post, edit or delete must re-render only that side's row.
func TestLineComment_EachSideOfALineGetsItsOwnRow(t *testing.T) {
	h, svc, db := newTestRouter(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	repoName := "sides_" + suffix
	repo, err := svc.Repo.Create(ctx, ownerID, owner, repoName, "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("Repo.Create: %v", err)
	}
	author := service.GitAuthor{Name: owner, Email: owner + "@test.invalid"}
	if err := svc.Code.CommitFile(owner, repoName, "main", "f.txt", []byte("a\nold\nc\n"), author, "base"); err != nil {
		t.Fatalf("CommitFile main: %v", err)
	}
	if err := svc.Code.CreateBranch(owner, repoName, "feature", "main"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := svc.Code.CommitFile(owner, repoName, "feature", "f.txt", []byte("a\nnew\nc\n"), author, "head"); err != nil {
		t.Fatalf("CommitFile feature: %v", err)
	}
	seedOpenPull(t, db, repo.ID, ownerID, 1)
	comment := func(side, body string) string {
		t.Helper()
		c, err := svc.PullLineComment.Create(ctx, owner, repoName, 1, ownerID, owner, "f.txt", side, 2, body)
		if err != nil {
			t.Fatalf("PullLineComment.Create: %v", err)
		}
		return strconv.FormatInt(c.ID, 10)
	}
	comment("right", "right one")
	leftOne := comment("left", "left one")
	leftTwo := comment("left", "left two")
	token := makeJWT(t, ownerID, owner)

	page := serve(h, browserRequest(http.MethodGet, "/"+owner+"/"+repoName+"/pulls/1/files", token, nil)).Body.String()
	unified, _, ok := strings.Cut(page, `class="diff-split"`)
	if !ok {
		t.Fatalf("Files tab has no split view:\n%s", page)
	}
	var threads []lineCommentRow
	leftRow := ""
	for _, row := range lineCommentRows(unified) {
		if len(row.bodies) > 0 {
			threads = append(threads, lineCommentRow{bodies: row.bodies})
		}
		if len(row.bodies) > 0 && row.bodies[0] == "left one" {
			leftRow = row.id
		}
	}
	if want := []lineCommentRow{{bodies: []string{"left one", "left two"}}, {bodies: []string{"right one"}}}; !reflect.DeepEqual(threads, want) {
		t.Errorf("threads = %+v, want %+v", threads, want)
	}
	if del, left, add := strings.Index(unified, "-old"), strings.Index(unified, "left one"), strings.Index(unified, "+new"); left <= del || add <= left {
		t.Errorf("left-side thread at %d is not between the deleted row at %d and the added row at %d", left, del, add)
	}

	api := "/api/repos/" + owner + "/" + repoName + "/pulls/1/line_comments"
	for _, step := range []struct {
		action, method, target string
		form                   url.Values
		want                   []string
	}{
		{"post", http.MethodPost, api, url.Values{"path": {"f.txt"}, "line": {"2"}, "diff_side": {"left"}, "body": {"left three"}}, []string{"left one", "left two", "left three"}},
		{"edit", http.MethodPatch, api + "/" + leftTwo, url.Values{"body": {"left two, edited"}}, []string{"left one", "left two, edited", "left three"}},
		{"delete", http.MethodDelete, api + "/" + leftOne, nil, []string{"left two, edited", "left three"}},
	} {
		req := httptest.NewRequest(step.method, step.target, strings.NewReader(step.form.Encode()))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		rr := serve(h, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: want 200, got %d: %s", step.action, rr.Code, rr.Body.String())
		}
		if got, want := lineCommentRows(rr.Body.String()), []lineCommentRow{{id: leftRow, bodies: step.want}}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: answer rows = %+v, want %+v", step.action, got, want)
		}
	}
}
