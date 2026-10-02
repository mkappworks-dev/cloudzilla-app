package pages

import (
	"context"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

var (
	idAttr          = regexp.MustCompile(`\sid="([^"]*)"`)
	hxTargetAttr    = regexp.MustCompile(`\shx-target="([^"]*)"`)
	addCommentBtn   = regexp.MustCompile(`<button type="button" hx-get="([^"]*)" hx-target="([^"]*)"`)
	plainIDSelector = regexp.MustCompile(`^#(-?[A-Za-z_][A-Za-z0-9_-]*)$`)
)

func renderHTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func idCounts(out string) map[string]int {
	counts := map[string]int{}
	for _, m := range idAttr.FindAllStringSubmatch(out, -1) {
		counts[html.UnescapeString(m[1])]++
	}
	return counts
}

func hxTargets(out string) []string {
	var targets []string
	for _, m := range hxTargetAttr.FindAllStringSubmatch(out, -1) {
		targets = append(targets, html.UnescapeString(m[1]))
	}
	return targets
}

// targetedID returns the id an hx-target selects. htmx hands the selector to
// querySelector, which reads an unescaped '.' as a class and rejects a '/', so
// only "#" plus a plain CSS identifier can reach an element whose id is a path.
func targetedID(t *testing.T, selector string) string {
	t.Helper()
	m := plainIDSelector.FindStringSubmatch(selector)
	if m == nil {
		t.Fatalf("hx-target %q is not a plain #id selector", selector)
	}
	return m[1]
}

func filesData(comments map[string][]view.RenderedLineComment) view.PullFilesData {
	return view.PullFilesData{
		OwnerName:    "acme",
		Repo:         &model.Repository{Name: "widgets"},
		Pull:         &model.PullRequest{Number: 7, State: model.PRStateOpen},
		CanWrite:     true,
		LineComments: comments,
	}
}

func modifiedFile(path string) service.FileDiff {
	return service.FileDiff{
		OldPath: path,
		NewPath: path,
		Hunks: []service.DiffHunk{{
			Header: "@@ -1,3 +1,3 @@",
			Lines: []service.DiffLine{
				{Type: "ctx", Content: "package app", OldNum: 1, NewNum: 1},
				{Type: "del", Content: "var x = 1", OldNum: 2},
				{Type: "add", Content: "var x = 2", NewNum: 2},
				{Type: "ctx", Content: "func main() {}", OldNum: 3, NewNum: 3},
			},
		}},
	}
}

func lineComment(path string, line int) view.RenderedLineComment {
	return view.RenderedLineComment{
		PullLineComment: model.PullLineComment{ID: 11, AuthorName: "daisy", Path: path, Line: line, Body: "nit", CreatedAt: time.Now()},
		BodyHTML:        "<p>nit</p>",
	}
}

// Every hx-target on the Files tab must select exactly one element, even for
// paths with '/' and '.' and for two files whose paths differ only there.
func TestPRFileDiff_LineCommentTargetsSelectOneElement(t *testing.T) {
	data := filesData(map[string][]view.RenderedLineComment{
		"src/app.go:3": {lineComment("src/app.go", 3)},
	})
	out := renderHTML(t, prFileDiff(data, modifiedFile("src/app.go"))) +
		renderHTML(t, prFileDiff(data, modifiedFile("src-app.go")))

	targets := hxTargets(out)
	// Three "+" buttons per file, plus the delete button on the existing comment.
	if len(targets) != 7 {
		t.Fatalf("got %d hx-targets, want 7: %q", len(targets), targets)
	}
	ids := idCounts(out)
	for _, sel := range targets {
		if id := targetedID(t, sel); ids[id] != 1 {
			t.Errorf("hx-target %q selects %d elements, want 1", sel, ids[id])
		}
	}
}

// The handlers answer the "+" button with LineCommentForm and a post or delete
// with LineComments; their targets and ids must line up with the page's rows.
func TestLineCommentFragments_TargetThePageRow(t *testing.T) {
	const path, line = "src/app.go", 2
	page := renderHTML(t, prFileDiff(filesData(nil), modifiedFile(path)))

	var slotSel string
	for _, m := range addCommentBtn.FindAllStringSubmatch(page, -1) {
		u, err := url.Parse(html.UnescapeString(m[1]))
		if err != nil {
			t.Fatalf("parse %q: %v", m[1], err)
		}
		if q := u.Query(); q.Get("path") == path && q.Get("line") == strconv.Itoa(line) {
			slotSel = html.UnescapeString(m[2])
		}
	}
	if slotSel == "" {
		t.Fatalf("no + button for %s:%d in\n%s", path, line, page)
	}
	slotID := targetedID(t, slotSel)
	pageIDs := idCounts(page)
	if pageIDs[slotID] != 1 {
		t.Fatalf("+ button target %q selects %d elements, want 1", slotSel, pageIDs[slotID])
	}

	form := renderHTML(t, fragments.LineCommentForm(view.LineCommentFormFragData{
		Owner: "acme", RepoName: "widgets", PullNumber: 7, Path: path, Line: line,
	}))
	formTargets := hxTargets(form)
	if len(formTargets) != 1 {
		t.Fatalf("form has %d hx-targets, want 1", len(formTargets))
	}
	rowID := targetedID(t, formTargets[0])
	if pageIDs[rowID] != 1 {
		t.Fatalf("form target %q selects %d page elements, want 1", formTargets[0], pageIDs[rowID])
	}

	thread := renderHTML(t, fragments.LineComments(view.LineCommentsFragData{
		Owner: "acme", RepoName: "widgets", PullNumber: 7, Path: path, Line: line,
		Comments: []view.RenderedLineComment{lineComment(path, line)}, CanWrite: true,
	}))
	if m := idAttr.FindStringSubmatch(thread); m == nil || m[1] != rowID {
		t.Errorf("thread root id = %v, want %q so later posts and deletes find the row", m, rowID)
	}
	if idCounts(thread)[slotID] != 1 {
		t.Errorf("thread lacks the form slot %q the + button targets", slotID)
	}
	for _, sel := range hxTargets(thread) {
		if id := targetedID(t, sel); id != rowID {
			t.Errorf("delete target %q, want #%s", sel, rowID)
		}
	}
}
