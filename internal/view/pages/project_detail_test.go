package pages_test

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func boardData(canWrite bool) view.ProjectDetailData {
	note := service.KanbanCardView{
		ID: 41, Kind: "note", Title: "Ship the beta",
		Description:     "Blocked on #1",
		DescriptionHTML: `<p>Blocked on <a href="/acme/widgets/issues/1">#1</a></p>`,
		DueDate:         "2020-01-02", Overdue: true,
		Assignees: []model.CardUser{{ID: 7, Username: "daisy"}},
		Labels:    []model.Label{{ID: 3, Name: "bug", Color: "#d73a4a"}},
		LinkKind:  "pull", LinkID: 90, LinkNumber: 5, LinkState: "open", LinkTitle: "Beta PR",
	}
	plain := service.KanbanCardView{
		ID: 42, Kind: "issue", Title: "Crash on start", Number: 9, State: "closed",
		LinkKind: "issue", LinkID: 91, LinkNumber: 9, LinkState: "closed",
	}
	d := view.ProjectDetailData{
		Owner: "acme", RepoName: "widgets",
		Project:  model.Project{ID: 14, Name: "Roadmap"},
		Columns:  []service.KanbanColumnView{{ID: 2, Name: "Todo", Cards: []service.KanbanCardView{note, plain}}},
		CanWrite: canWrite,
	}
	if canWrite {
		d.BasePage = view.BasePage{CurrentUser: &middleware.Claims{UserID: 1, Username: "acme"}}
		d.Labels = []model.Label{{ID: 3, Name: "bug", Color: "#d73a4a"}, {ID: 4, Name: "docs", Color: "#fef2c0"}}
		d.People = []model.CardUser{{ID: 1, Username: "acme"}, {ID: 7, Username: "daisy"}}
	}
	return d
}

func renderBoard(t *testing.T, data view.ProjectDetailData) string {
	t.Helper()
	ctx := components.WithAvatarKeys(context.Background(), map[string]string{"daisy": "avatars/user/7/abc.png"})
	var sb strings.Builder
	if err := pages.ProjectDetail(data).Render(ctx, &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

// jsonEscaped spells s the way json.Marshal escapes < and > inside an HTML attribute.
func jsonEscaped(s string) string {
	bs := "\\"
	return strings.NewReplacer("<", bs+"u003c", ">", bs+"u003e", `"`, bs+"&#34;").Replace(s)
}

func TestProjectDetail_CardFace(t *testing.T) {
	out := renderBoard(t, boardData(true))

	for _, want := range []string{
		"Ship the beta",
		"Blocked on #1",
		`style="background-color: #d73a4a; color: #ffffff;"`,
		`<time datetime="2020-01-02">Jan 2, 2020</time>`,
		`src="/avatars/user/7/abc.png"`,
		`data-card-json=`,
		`data-card-open`,
		`role="button"`,
		`href="/acme/widgets/issues/9"`,
		"&#34;description_html&#34;:&#34;" + jsonEscaped(`<p>Blocked on <a href="/acme/widgets/issues/1">#1</a></p>`) + "&#34;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("board missing %q", want)
		}
	}
	if !regexp.MustCompile(`class="[^"]*text-destructive[^"]*" data-card-due`).MatchString(out) {
		t.Error("an overdue due date is not marked text-destructive")
	}
	if !regexp.MustCompile(`data-card-link>(?s:.*?)#5`).MatchString(out) {
		t.Error("the linked PR badge #5 is missing from the note card")
	}
	if strings.Contains(out, "<template id=\"desc-") {
		t.Error("the board still renders per-card description templates; the card JSON carries the HTML")
	}
}

// The service hands over a hidden card with nothing but its ID, column and position.
func TestProjectDetail_HiddenCardIsAPlaceholderWithoutEditorOrLink(t *testing.T) {
	for _, canWrite := range []bool{false, true} {
		data := boardData(canWrite)
		data.Columns[0].Cards = []service.KanbanCardView{{ID: 43, Kind: "hidden", RepoFullName: "acme/widgets", ColumnID: 2}}
		out := renderBoard(t, data)

		li := regexp.MustCompile(`(?s)<li[^>]*data-card-id="43".*?</li>`).FindString(out)
		if !strings.Contains(li, "Private issue") {
			t.Fatalf("hidden card = %q, want the Private issue placeholder", li)
		}
		for _, gone := range []string{"data-card-json", "data-card-open", "data-card-panel", `role="button"`, "href=", "data-card-link", "#0", "<template"} {
			if strings.Contains(li, gone) {
				t.Errorf("canWrite=%v: hidden card renders %q: %s", canWrite, gone, li)
			}
		}
	}
}

func cardDialog(t *testing.T, out string) string {
	t.Helper()
	d := regexp.MustCompile(`(?s)<dialog id="card-dialog".*?</dialog>`).FindString(out)
	if d == "" {
		t.Fatal("board renders no card dialog")
	}
	return d
}

func TestProjectDetail_CardDialogWriteControls(t *testing.T) {
	out := renderBoard(t, boardData(true))
	dlg := cardDialog(t, out)
	for _, want := range []string{
		`aria-labelledby="card-dialog-title"`, `id="card-dialog-title"`, `x-text="heading"`,
		`@submit.prevent="submit()"`,
		`<label for="card-title-input"`, `id="card-title-input"`,
		`id="card-desc-label"`, `data-md-editor`, `id="card-description"`, `Write</button>`, `Preview</button>`,
		`x-data="linkPicker"`, `<label for="card-link-input"`, `data-picker-list`,
		`data-card-people`, `x-model="editor.assignees"`, `data-value="7"`, "daisy",
		`data-card-labels`, `x-model="editor.labels"`, `data-value="4"`, "docs", `style="background-color: #fef2c0; color: #1f2328;"`,
		`<label for="card-due-input"`, `id="card-due-input"`, `x-data="datePicker"`, `x-model="editor.dueDate"`,
		`aria-label="Choose due date"`, `name="due_date"`,
		`type="submit"`, "Create card", `@click="close()"`, `role="alert"`,
		`@click="ask(&#39;convert&#39;)"`, `@click="ask(&#39;delete&#39;)"`, `data-card-confirm`,
		"Create an issue from this card? The card will link to the new issue.", "Delete this card?",
		`@click="convertCard()"`, "Create issue", `@click="deleteCard()"`, "Delete card", `@click="cancelConfirm()"`,
		"Today</button>", "Clear</button>",
		`@keydown.escape="onEscape($event)"`, `@cancel="onEscape($event)"`,
		`@click.outside="reset()"`, `@focusout="onFocusOut($event)"`,
	} {
		if !strings.Contains(dlg, want) {
			t.Errorf("writer card dialog missing %q", want)
		}
	}
	if !regexp.MustCompile(`<button[^>]*@click="close\(\)"[^>]*x-bind:disabled="editor.busy"|<button[^>]*x-bind:disabled="editor.busy"[^>]*@click="close\(\)"`).MatchString(dlg) {
		t.Error("Cancel stays enabled while a request is in flight")
	}
	for _, want := range []string{`data-add-card="2"`, `data-column-name="Todo"`, "+ Add item"} {
		if !strings.Contains(out, want) {
			t.Errorf("writer board missing %q", want)
		}
	}
	if strings.Contains(out, "data-card-panel") {
		t.Error("writer board still renders the side panel")
	}

	// The fields scroll in the body and the submit button sits in the footer after them.
	title, due, submit := strings.Index(dlg, `id="card-title-input"`), strings.Index(dlg, `id="card-due-input"`), strings.Index(dlg, `type="submit"`)
	if title < 0 || title >= due || due >= submit {
		t.Errorf("card dialog order = title %d, due date %d, submit %d; want fields before the footer submit", title, due, submit)
	}
}

// The dialog is a child of the board's space-y-6 container, whose margin-block-end would
// otherwise override the browser's auto margins and pin the dialog to the top of the viewport.
func TestProjectDetail_CardDialogIsCentred(t *testing.T) {
	for _, canWrite := range []bool{true, false} {
		open := regexp.MustCompile(`<dialog id="card-dialog"[^>]*>`).FindString(renderBoard(t, boardData(canWrite)))
		if !regexp.MustCompile(`class="[^"]*\bm-auto\b`).MatchString(open) {
			t.Errorf("canWrite=%v: card dialog has no m-auto to beat the space-y margins: %s", canWrite, open)
		}
	}
}

func TestProjectDetail_AddColumnDialog(t *testing.T) {
	out := renderBoard(t, boardData(true))
	for _, want := range []string{
		`<dialog id="add-column-dialog"`, `aria-labelledby="add-column-dialog-title"`,
		`id="add-column-dialog-title"`, "Columns group the cards on this board.",
		`x-data="columnDialog"`, `<label for="add-column-name"`, `id="add-column-name"`,
		`placeholder="To do"`, `role="alert"`, "showModal()", "+ Add column",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("writer board missing %q", want)
		}
	}
	if !regexp.MustCompile(`<button[^>]*type="submit"[^>]*x-bind:disabled="busy \|\| blank"|<button[^>]*x-bind:disabled="busy \|\| blank"[^>]*type="submit"`).MatchString(out) {
		t.Error("the Add column submit button is not disabled while busy or blank")
	}
	for _, gone := range []string{"new-col-name", "adding: false"} {
		if strings.Contains(out, gone) {
			t.Errorf("board still renders the inline add-column form (%q)", gone)
		}
	}

	ro := renderBoard(t, boardData(false))
	for _, gone := range []string{"add-column-dialog", "columnDialog", "+ Add column"} {
		if strings.Contains(ro, gone) {
			t.Errorf("read-only board renders %q", gone)
		}
	}
}

func TestProjectDetail_ReadOnlyCardDialog(t *testing.T) {
	out := renderBoard(t, boardData(false))
	for _, gone := range []string{
		"convertCard", "deleteCard", "linkPicker", "data-add-card", "+ Add item",
		"data-card-people", "data-card-labels", "multiSelect", "Remove from board", `draggable="true"`,
	} {
		if strings.Contains(out, gone) {
			t.Errorf("read-only board renders write control %q", gone)
		}
	}
	dlg := cardDialog(t, out)
	for _, gone := range []string{
		`type="submit"`, "Create card", "data-md-editor", "Write</button>", "<input", "<textarea",
		`aria-label="Edit`, "Add a description", "data-field-status", "saveTitle", "saveDescription", "commitField",
		"data-card-confirm", "Create an issue from this card?", "Delete this card?", "cancelConfirm",
		"Remove link", "Choose due date", "Today</button>", "Clear</button>", `x-data="datePicker"`,
	} {
		if strings.Contains(dlg, gone) {
			t.Errorf("read-only card dialog renders %q", gone)
		}
	}
	for _, want := range []string{
		`@click="close()"`, "Close", `x-html="editor.saved.descriptionHTML"`, `data-card-preview`, "No description",
		`x-text="dueLabel(editor.saved.dueDate) || 'None'"`,
	} {
		if !strings.Contains(dlg, want) {
			t.Errorf("read-only card dialog missing %q", want)
		}
	}
	if !strings.Contains(out, "&#34;description_html&#34;:&#34;"+jsonEscaped("<p>Blocked on")) {
		t.Error("read-only board is missing the rendered description in the card JSON")
	}
}

// A card opens as a detail view: writers get a pencil per text field, the dropdowns and the
// date save on their own when the user is done choosing, and nothing saves the whole form.
func TestProjectDetail_CardViewEditsFieldsInPlace(t *testing.T) {
	dlg := cardDialog(t, renderBoard(t, boardData(true)))
	for _, want := range []string{
		`aria-label="Edit title"`, `@click="editField(&#39;title&#39;)"`,
		`id="card-title-edit"`, `@keydown.enter.prevent="saveTitle()"`, `@click="saveTitle()"`, `@click="cancelField(&#39;title&#39;)"`,
		`aria-label="Edit description"`, `@click="editField(&#39;description&#39;)"`, "Add a description",
		`x-html="editor.saved.descriptionHTML"`, `@click="saveDescription()"`, `@click="cancelField(&#39;description&#39;)"`,
		`aria-label="Change linked item"`, `@click="clearLink()"`,
		`x-bind:disabled="editor.busy || anySaving || anyEditing"`,
	} {
		if !strings.Contains(dlg, want) {
			t.Errorf("writer card view missing %q", want)
		}
	}
	// The commit listeners wrap the dropdowns and the date picker: inside them they would run
	// in the component's own scope, where the board's $root is out of reach.
	for field, listener := range map[string]string{
		"assignees": `@multiselect-closed="commitField('assignees', $event.detail.selected)"`,
		"labels":    `@multiselect-closed="commitField('labels', $event.detail.selected)"`,
		"due":       `@date-picked="commitField('due', $event.detail.value)"`,
	} {
		wrap := regexp.MustCompile(`<fieldset[^>]*` + regexp.QuoteMeta(listener) + `[^>]*>\s*<legend[^>]*>[^<]*</legend>\s*<div[^>]*x-data="(multiSelect|datePicker)"`).FindString(dlg)
		if wrap == "" {
			t.Errorf("%s: no fieldset around the component carrying %s", field, listener)
		}
	}
	for _, field := range []string{"title", "description", "assignees", "labels", "due", "link"} {
		status := regexp.MustCompile(`<span[^>]*data-field-status="` + field + `"[^>]*>`).FindString(dlg)
		for _, want := range []string{`role="status"`, `aria-live="polite"`, `x-text="fieldStatus(&#39;` + field + `&#39;)"`} {
			if !strings.Contains(status, want) {
				t.Errorf("%s status missing %s: %q", field, want, status)
			}
		}
	}

	// The issues page's editor: Write/Preview tabs, the toolbar, and a preview of the typed text.
	if n := strings.Count(dlg, "data-md-editor"); n != 1 {
		t.Errorf("markdown editors = %d, want 1 shared by create and edit", n)
	}
	for _, want := range []string{
		`data-md-tab="write"`, `data-md-tab="preview"`, `hx-post="/api/markdown/preview"`,
		`document.getElementById(&#39;card-description&#39;).value`, `hx-target="#card-description-preview"`,
		`data-md-action="bold"`, `data-md-action="link"`,
	} {
		if !strings.Contains(dlg, want) {
			t.Errorf("description editor missing %q", want)
		}
	}
	editor := regexp.MustCompile(`<div x-show="([^"]*)"[^>]*data-card-description-editor`).FindStringSubmatch(dlg)
	if len(editor) < 2 || editor[1] != "editor.mode === 'create' || editor.fields.description.editing" {
		t.Errorf("the description editor should show in create mode and while editing: %q", editor)
	}

	// Create is the only submit, and only the create footer shows it.
	if n := strings.Count(dlg, `type="submit"`); n != 1 {
		t.Errorf("submit buttons = %d, want 1 (Create card)", n)
	}
	if !regexp.MustCompile(`(?s)<div x-show="editor.mode === 'create'"[^>]*>\s*<button[^>]*>\s*Cancel\s*</button>\s*<button[^>]*type="submit"`).MatchString(dlg) {
		t.Error("the submit button is not inside the create-only footer group")
	}
	// Convert and Delete wait for every field save; Convert also for fields edited in place.
	for ref, want := range map[string]string{
		"confirmConvert": `x-bind:disabled="editor.busy || anySaving || anyEditing"`,
		"confirmDelete":  `x-bind:disabled="editor.busy || anySaving"`,
		"deleteButton":   `x-bind:disabled="editor.busy || anySaving"`,
		"convertButton":  `x-bind:disabled="editor.busy || anySaving || anyEditing"`,
	} {
		if btn := regexp.MustCompile(`<button[^>]*x-ref="` + ref + `"[^>]*>`).FindString(dlg); !strings.Contains(btn, want) {
			t.Errorf("%s = %q, want %s", ref, btn, want)
		}
	}
	for _, gone := range []string{"descDirty", "Save to refresh the preview", "editor.tab", "previewHTML"} {
		if strings.Contains(dlg, gone) {
			t.Errorf("the old whole-form editor's %q is still rendered", gone)
		}
	}
}

// The linked item chip is an anchor whose href kanban.js builds from the board's owner and
// repo plus the link kind and number in the card JSON; the unlink button sits outside it.
func TestProjectDetail_LinkedItemChipIsALink(t *testing.T) {
	for _, canWrite := range []bool{true, false} {
		out := renderBoard(t, boardData(canWrite))
		chip := regexp.MustCompile(`(?s)<div[^>]*data-card-link-chip[^>]*>.*?</a>`).FindString(cardDialog(t, out))
		if !regexp.MustCompile(`<a[^>]*x-bind:href="linkURL"`).MatchString(chip) {
			t.Errorf("canWrite=%v: linked item chip has no anchor bound to linkURL: %q", canWrite, chip)
		}
		if strings.Contains(chip, "Remove link") {
			t.Errorf("canWrite=%v: the unlink button sits inside the link", canWrite)
		}
		for _, want := range []string{`data-owner="acme"`, `data-repo-name="widgets"`, `&#34;link_kind&#34;:&#34;pull&#34;`, `&#34;link_number&#34;:5`, `&#34;link_state&#34;:&#34;open&#34;`} {
			if !strings.Contains(out, want) {
				t.Errorf("canWrite=%v: board missing %q for the chip href", canWrite, want)
			}
		}
	}
}

// The edit JSON passes a label colour to the browser only when it is a valid hex colour.
func TestProjectDetail_CardJSONDropsInvalidLabelColour(t *testing.T) {
	data := boardData(false)
	data.Columns[0].Cards[0].Labels = []model.Label{{ID: 5, Name: "x", Color: "red;background:url(x)"}}
	out := renderBoard(t, data)
	if strings.Contains(out, "url(x)") {
		t.Error("an invalid label colour reaches the card JSON")
	}
}

// Convert keeps the dialog open: the success line is a polite live region, and the chip it
// reveals is driven by editor.link, which showConverted sets with editor.saved.link.
func TestProjectDetail_CardDialogConvertStaysOpen(t *testing.T) {
	dlg := cardDialog(t, renderBoard(t, boardData(true)))
	live := regexp.MustCompile(`(?s)<div role="status" aria-live="polite">.*?data-card-notice.*?</div>`).FindString(dlg)
	if !strings.Contains(live, `x-text="editor.notice"`) {
		t.Errorf("no polite live region showing editor.notice: %q", live)
	}
	chip := regexp.MustCompile(`(?s)<div[^>]*data-card-link-chip[^>]*>.*?</a>`).FindString(dlg)
	for _, want := range []string{`x-show="editor.link"`, `x-ref="linkAnchor"`, `x-bind:href="linkURL"`, `editor.link.number`, `editor.link.title`, `editor.link.state`} {
		if !strings.Contains(chip, want) {
			t.Errorf("linked item chip missing %q: %q", want, chip)
		}
	}
	if !strings.Contains(dlg, `x-show="editor.mode === &#39;edit&#39; &amp;&amp; !editor.link &amp;&amp; editor.saved.title"`) {
		t.Error("Convert is not hidden once the editor has a link")
	}

	ro := cardDialog(t, renderBoard(t, boardData(false)))
	if strings.Contains(ro, "data-card-notice") {
		t.Error("read-only dialog renders the convert success line")
	}
}

func TestProjectDetail_CardDialogUsesDropdownsForAssigneesAndLabels(t *testing.T) {
	dlg := cardDialog(t, renderBoard(t, boardData(true)))
	if strings.Contains(dlg, `type="checkbox"`) {
		t.Error("the card dialog still renders an always-visible checkbox list")
	}
	for _, id := range []string{"card-assignees-trigger", "card-labels-trigger"} {
		trigger := regexp.MustCompile(`<button[^>]*id="` + id + `"[^>]*>`).FindString(dlg)
		for _, want := range []string{`aria-haspopup="listbox"`, `x-bind:aria-expanded="open.toString()"`, `aria-labelledby=`} {
			if !strings.Contains(trigger, want) {
				t.Errorf("%s missing %s: %q", id, want, trigger)
			}
		}
	}
	if n := strings.Count(dlg, `x-data="multiSelect"`); n != 2 {
		t.Errorf("multiSelect components = %d, want 2 (assignees, labels)", n)
	}
	if n := strings.Count(dlg, `role="listbox" aria-multiselectable="true"`); n != 2 {
		t.Errorf("multiselectable listboxes = %d, want 2", n)
	}
	for _, want := range []string{`role="option"`, `x-bind:aria-selected="has($el.dataset.value).toString()"`, `Unassigned`, `No labels`, `staleAssignees`} {
		if !strings.Contains(dlg, want) {
			t.Errorf("writer card dialog missing %q", want)
		}
	}
	// Two options each: no filter box below the threshold.
	if strings.Contains(dlg, "data-multiselect-filter") {
		t.Error("a short option list renders a filter box")
	}

	many := boardData(true)
	for i := int64(10); i < 18; i++ {
		many.People = append(many.People, model.CardUser{ID: i, Username: "user" + strconv.FormatInt(i, 10)})
	}
	if !strings.Contains(cardDialog(t, renderBoard(t, many)), "data-multiselect-filter") {
		t.Error("a long option list renders no filter box")
	}
}

// Linked item and due date sit in the right-hand column with the assignee and label
// dropdowns; title and description own the left column. Create and edit share this markup.
func TestProjectDetail_CardDialogSideColumn(t *testing.T) {
	for _, canWrite := range []bool{true, false} {
		dlg := cardDialog(t, renderBoard(t, boardData(canWrite)))
		mainAt, sideAt := strings.Index(dlg, "data-card-main"), strings.Index(dlg, "data-card-side")
		if mainAt < 0 || sideAt < 0 || mainAt > sideAt {
			t.Fatalf("canWrite=%v: main column %d, side column %d, want both with main first", canWrite, mainAt, sideAt)
		}
		main, side := dlg[mainAt:sideAt], dlg[sideAt:]
		wantMain := []string{`id="card-desc-label"`, `data-card-field="description"`}
		if canWrite {
			wantMain = append(wantMain, `id="card-title-input"`)
		}
		for _, want := range wantMain {
			if !strings.Contains(main, want) {
				t.Errorf("canWrite=%v: main column missing %q", canWrite, want)
			}
		}
		order := []string{`id="card-assignees-label"`, `id="card-labels-label"`, `data-card-field="due"`, "data-card-link-chip"}
		for _, want := range order {
			if !strings.Contains(side, want) || strings.Contains(main, want) {
				t.Errorf("canWrite=%v: %q is not only in the side column", canWrite, want)
			}
		}
		last := -1
		for _, want := range order {
			at := strings.Index(side, want)
			if at < last {
				t.Errorf("canWrite=%v: side column order is wrong at %q", canWrite, want)
			}
			last = at
		}
		grid := regexp.MustCompile(`<div class="[^"]*\bgrid\b[^"]*"><div data-card-main`).FindString(dlg)
		if !strings.Contains(grid, "lg:grid-cols-") || strings.Contains(grid, "editor.mode") {
			t.Errorf("canWrite=%v: the columns should split from lg up and stack below, whatever the mode: %q", canWrite, grid)
		}
	}
}

func TestProjectDetail_ReadOnlyCardDialogShowsSelectionAsText(t *testing.T) {
	dlg := cardDialog(t, renderBoard(t, boardData(false)))
	for _, want := range []string{`x-for="a in editor.saved.assignees"`, `x-for="l in editor.saved.labels"`} {
		if !strings.Contains(dlg, want) {
			t.Errorf("read-only dialog missing %q", want)
		}
	}
	for _, gone := range []string{`role="listbox"`, `role="option"`, `x-data="multiSelect"`, `aria-haspopup="listbox"`} {
		if strings.Contains(dlg, gone) {
			t.Errorf("read-only dialog renders %q", gone)
		}
	}
}

// The X sits in the header beside the title, for writers and read-only viewers alike, closes
// through the guarded close(), and is disabled while a request or a field save is running.
func TestProjectDetail_CardDialogHasACloseX(t *testing.T) {
	for _, canWrite := range []bool{true, false} {
		dlg := cardDialog(t, renderBoard(t, boardData(canWrite)))
		x := regexp.MustCompile(`<button[^>]*aria-label="Close"[^>]*>`).FindString(dlg)
		if !strings.Contains(x, `@click="close()"`) || !strings.Contains(x, `x-bind:disabled="closeBlocked"`) || !strings.Contains(x, `type="button"`) {
			t.Errorf("canWrite=%v: close X = %q, want a type=button wired to close() and disabled while closeBlocked", canWrite, x)
		}
		if at, body := strings.Index(dlg, `aria-label="Close"`), strings.Index(dlg, "data-card-main"); at < 0 || at > body {
			t.Errorf("canWrite=%v: the close X (%d) is not in the header above the fields (%d)", canWrite, at, body)
		}
		if !strings.Contains(dlg, `<span x-text="heading" class="block break-words">`) {
			t.Errorf("canWrite=%v: a long card title does not wrap beside the X", canWrite)
		}
	}
}
