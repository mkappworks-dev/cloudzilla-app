package pages

import (
	"context"
	"html"
	"net/url"
	"reflect"
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
	tableRow        = regexp.MustCompile(`(?s)<tr\b([^>]*)>(.*?)</tr>`)
	tableCell       = regexp.MustCompile(`(?s)<td\b[^>]*>(.*?)</td>`)
	paragraph       = regexp.MustCompile(`(?s)<p>(.*?)</p>`)
	htmlTag         = regexp.MustCompile(`<[^>]*>`)
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

func filesData(comments map[view.LineCommentKey][]view.RenderedLineComment) view.PullFilesData {
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

// insertedLineFile adds a line above a context line, which makes that context
// line 2 of the base file but line 3 of the head file.
func insertedLineFile(path string) service.FileDiff {
	return service.FileDiff{
		OldPath: path,
		NewPath: path,
		Hunks: []service.DiffHunk{{
			Header: "@@ -1,3 +1,3 @@",
			Lines: []service.DiffLine{
				{Type: "ctx", Content: "package app", OldNum: 1, NewNum: 1},
				{Type: "add", Content: "var x = 2", NewNum: 2},
				{Type: "ctx", Content: "func main() {}", OldNum: 2, NewNum: 3},
				{Type: "del", Content: "// old trailer", OldNum: 3},
			},
		}},
	}
}

type diffRow struct {
	code        string   // the line without its +/-/space marker
	gutter      string   // the line number shown beside it
	commentLine string   // the line its "+" button opens a form for
	threads     []string // comment bodies rendered right below it
}

func diffRows(t *testing.T, out string) []diffRow {
	t.Helper()
	text := func(s string) string { return html.UnescapeString(htmlTag.ReplaceAllString(s, "")) }
	var rows []diffRow
	for _, m := range tableRow.FindAllStringSubmatch(out, -1) {
		attrs, inner := m[1], m[2]
		if !strings.Contains(attrs, `class="diff-line`) {
			if len(rows) > 0 {
				for _, p := range paragraph.FindAllStringSubmatch(inner, -1) {
					rows[len(rows)-1].threads = append(rows[len(rows)-1].threads, text(p[1]))
				}
			}
			continue
		}
		cells := tableCell.FindAllStringSubmatch(inner, -1)
		if len(cells) != 2 {
			t.Fatalf("diff row has %d cells, want 2: %s", len(cells), inner)
		}
		row := diffRow{gutter: strings.Fields(text(cells[0][1]))[0], code: text(cells[1][1])[1:]}
		if b := addCommentBtn.FindStringSubmatch(inner); b != nil {
			u, err := url.Parse(html.UnescapeString(b[1]))
			if err != nil {
				t.Fatalf("parse %q: %v", b[1], err)
			}
			row.commentLine = u.Query().Get("line")
		}
		rows = append(rows, row)
	}
	return rows
}

func lineComment(path string, line int) view.RenderedLineComment {
	return view.RenderedLineComment{
		PullLineComment: model.PullLineComment{ID: 11, AuthorName: "daisy", Path: path, Line: line, Body: "nit", CreatedAt: time.Now()},
		BodyHTML:        "<p>nit</p>",
	}
}

// Every hx-target on the Files tab must select exactly one element, even for
// paths with '/' and '.', for two files whose paths differ only there, for a
// context line whose base and head line numbers differ, and for left- and
// right-side threads on the same line number.
func TestPRFileDiff_LineCommentTargetsSelectOneElement(t *testing.T) {
	data := filesData(map[view.LineCommentKey][]view.RenderedLineComment{
		{Side: "right", Path: "src/app.go", Line: 3}: {lineComment("src/app.go", 3)},
		{Side: "left", Path: "src/app.go", Line: 3}:  {lineComment("src/app.go", 3)},
		{Side: "left", Path: "src/app.go", Line: 2}:  {lineComment("src/app.go", 2)},
	})
	out := renderHTML(t, prFileDiff(data, modifiedFile("src/app.go"))) +
		renderHTML(t, prFileDiff(data, modifiedFile("src-app.go"))) +
		renderHTML(t, prFileDiff(data, insertedLineFile("lib/app.go")))

	targets := hxTargets(out)
	// Three "+" buttons per file, plus the delete button on each comment.
	if len(targets) != 12 {
		t.Fatalf("got %d hx-targets, want 12: %q", len(targets), targets)
	}
	ids := idCounts(out)
	for _, sel := range targets {
		if id := targetedID(t, sel); ids[id] != 1 {
			t.Errorf("hx-target %q selects %d elements, want 1", sel, ids[id])
		}
	}
}

// The comment form posts diff_side=right, and applying a suggestion edits the
// head branch, so every row but a deletion must carry its head-file number.
func TestPRFileDiff_RowsCarryTheirHeadFileLine(t *testing.T) {
	got := diffRows(t, renderHTML(t, prFileDiff(filesData(nil), insertedLineFile("src/app.go"))))
	want := []diffRow{
		{code: "package app", gutter: "1", commentLine: "1"},
		{code: "var x = 2", gutter: "2", commentLine: "2"},
		{code: "func main() {}", gutter: "3", commentLine: "3"},
		{code: "// old trailer", gutter: "3"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %+v\nwant %+v", got, want)
	}
}

// A right-side comment's line is a head-file line, so its thread renders once,
// under the row with that head-file line.
func TestPRFileDiff_ThreadsRenderUnderTheirHeadFileLine(t *testing.T) {
	onAdded := lineComment("src/app.go", 2)
	onAdded.BodyHTML = "<p>on the added line</p>"
	onContext := lineComment("src/app.go", 3)
	onContext.BodyHTML = "<p>on the context line</p>"
	data := filesData(map[view.LineCommentKey][]view.RenderedLineComment{
		{Side: "right", Path: "src/app.go", Line: 2}: {onAdded},
		{Side: "right", Path: "src/app.go", Line: 3}: {onContext},
	})

	got := map[string][]string{}
	for _, row := range diffRows(t, renderHTML(t, prFileDiff(data, insertedLineFile("src/app.go")))) {
		if len(row.threads) > 0 {
			got[row.code] = row.threads
		}
	}
	want := map[string][]string{
		"var x = 2":      {"on the added line"},
		"func main() {}": {"on the context line"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("threads by row = %q, want %q", got, want)
	}
}

// A left-side comment's line is a base-file line, so its thread renders under
// the deleted or context row showing that line, below any right-side thread
// there, and never under the head-file line with the same number.
func TestPRFileDiff_LeftSideThreadsRenderUnderTheirBaseFileLine(t *testing.T) {
	data := filesData(map[view.LineCommentKey][]view.RenderedLineComment{})
	for k, body := range map[view.LineCommentKey]string{
		{Side: "right", Path: "src/app.go", Line: 2}: "on head line 2",
		{Side: "right", Path: "src/app.go", Line: 3}: "on head line 3",
		{Side: "left", Path: "src/app.go", Line: 2}:  "on base line 2",
		{Side: "left", Path: "src/app.go", Line: 3}:  "on base line 3",
	} {
		c := lineComment(k.Path, k.Line)
		c.BodyHTML = "<p>" + body + "</p>"
		data.LineComments[k] = []view.RenderedLineComment{c}
	}

	got := map[string][]string{}
	for _, row := range diffRows(t, renderHTML(t, prFileDiff(data, insertedLineFile("src/app.go")))) {
		if len(row.threads) > 0 {
			got[row.code] = row.threads
		}
	}
	want := map[string][]string{
		"var x = 2":      {"on head line 2"},
		"func main() {}": {"on head line 3", "on base line 2"},
		"// old trailer": {"on base line 3"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("threads by row = %q, want %q", got, want)
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
		Owner: "acme", RepoName: "widgets", PullNumber: 7, Key: view.LineCommentKey{Side: "right", Path: path, Line: line},
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

// The "+" button must hand the form handler the exact path, including
// characters that delimit a query string or start a fragment.
func TestPRFileDiff_AddCommentURLKeepsThePath(t *testing.T) {
	const path = "docs/R&D#1+2.md"
	page := renderHTML(t, prFileDiff(filesData(nil), modifiedFile(path)))

	btns := addCommentBtn.FindAllStringSubmatch(page, -1)
	if len(btns) == 0 {
		t.Fatal("no + buttons rendered")
	}
	for _, m := range btns {
		u, err := url.Parse(html.UnescapeString(m[1]))
		if err != nil {
			t.Fatalf("parse %q: %v", m[1], err)
		}
		if got := u.Query().Get("path"); got != path {
			t.Errorf("form URL %q carries path %q, want %q", u, got, path)
		}
	}
}

const (
	hlCtx = `<span class="hl-k">CTX</span>`
	hlDel = `<span class="hl-k">DEL</span>`
	hlAdd = `<span class="hl-k">ADD</span>`
)

func highlightedModifiedFile() service.FileDiff {
	f := modifiedFile("app.go")
	f.Hunks[0].Lines[0].HTML = hlCtx
	f.Hunks[0].Lines[1].HTML = hlDel
	f.Hunks[0].Lines[2].HTML = hlAdd
	return f
}

func requireContains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPRFileDiff_HighlightedLinesKeepOnlyTheMarkerColor(t *testing.T) {
	out := renderHTML(t, prFileDiff(filesData(nil), highlightedModifiedFile()))
	requireContains(t, out,
		`whitespace-pre"><span class="text-destructive">-</span>`+hlDel+`</td>`,
		`whitespace-pre"><span class="text-success">+</span>`+hlAdd+`</td>`,
		`whitespace-pre text-foreground/85"> func main() {}</td>`,
	)
}

func TestPRFileDiff_PlainLinesKeepTheWholeLineColor(t *testing.T) {
	out := renderHTML(t, prFileDiff(filesData(nil), modifiedFile("app.go")))
	requireContains(t, out,
		`whitespace-pre text-destructive">-var x = 1</td>`,
		`whitespace-pre text-success">+var x = 2</td>`,
		`whitespace-pre text-foreground/85"> package app</td>`,
	)
}

func TestPRFileSplitDiff_HighlightsBothSides(t *testing.T) {
	out := renderHTML(t, prFileSplitDiff(highlightedModifiedFile()))
	requireContains(t, out,
		`bg-destructive/10"><span class="text-destructive">-</span>`+hlDel+`</td>`,
		`bg-success/10"><span class="text-success">+</span>`+hlAdd+`</td>`,
	)
	if got := strings.Count(out, hlCtx); got != 2 {
		t.Errorf("context line highlighted %d times, want once per side", got)
	}
}

func TestPRFileSplitDiff_PlainCellsKeepTheWholeLineColor(t *testing.T) {
	out := renderHTML(t, prFileSplitDiff(modifiedFile("app.go")))
	requireContains(t, out,
		`bg-destructive/10 text-destructive">-var x = 1</td>`,
		`bg-success/10 text-success">+var x = 2</td>`,
	)
}

func TestPRFileSplitDiff_NewFileRendersHighlightedLines(t *testing.T) {
	f := highlightedModifiedFile()
	f.IsNew = true
	out := renderHTML(t, prFileSplitDiff(f))
	requireContains(t, out,
		`<span class="text-success">+</span>`+hlAdd+`</td>`,
		`whitespace-pre text-foreground/85"> func main() {}</td>`,
	)
}
