package pages_test

import (
	"context"
	"regexp"
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

func TestProjectDetail_CardFace(t *testing.T) {
	out := renderBoard(t, boardData(true))

	for _, want := range []string{
		"Ship the beta",
		"Blocked on #1",
		`style="background-color: #d73a4a; color: #ffffff;"`,
		`<time datetime="2020-01-02">Jan 2, 2020</time>`,
		`src="/avatars/user/7/abc.png"`,
		`data-card-json=`,
		`role="button"`,
		`href="/acme/widgets/issues/9"`,
		`<template id="desc-41"><p>Blocked on <a href="/acme/widgets/issues/1">#1</a></p></template>`,
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
	if strings.Contains(out, `id="desc-42"`) {
		t.Error("a plain linked card must not carry a description template")
	}
}

// The service hands over a hidden card with nothing but its ID, column and position.
func TestProjectDetail_HiddenCardIsAPlaceholderWithoutPanelOrLink(t *testing.T) {
	data := boardData(false)
	data.Columns[0].Cards = []service.KanbanCardView{{ID: 43, Kind: "hidden", RepoFullName: "acme/widgets", ColumnID: 2}}
	out := renderBoard(t, data)

	li := regexp.MustCompile(`(?s)<li[^>]*data-card-id="43".*?</li>`).FindString(out)
	if !strings.Contains(li, "Private issue") {
		t.Fatalf("hidden card = %q, want the Private issue placeholder", li)
	}
	for _, gone := range []string{"data-card-json", "data-card-panel", `role="button"`, "href=", "data-card-link", "#0"} {
		if strings.Contains(li, gone) {
			t.Errorf("hidden card renders %q: %s", gone, li)
		}
	}
}

func TestProjectDetail_PanelWriteControls(t *testing.T) {
	out := renderBoard(t, boardData(true))
	for _, want := range []string{
		`@click="savePanel()"`, `@click="convertPanel()"`, `@click="deletePanel()"`,
		`x-data="linkPicker"`, `x-data="cardComposer(2)"`, `data-panel-people`, `data-panel-labels`,
		`value="7"`, "docs",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("writer board missing %q", want)
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

func TestProjectDetail_ReadOnlyPanel(t *testing.T) {
	out := renderBoard(t, boardData(false))
	for _, gone := range []string{
		"savePanel", "convertPanel", "deletePanel", "linkPicker", "cardComposer",
		"data-panel-people", "data-panel-labels", "Remove from board", `draggable="true"`,
	} {
		if strings.Contains(out, gone) {
			t.Errorf("read-only board renders write control %q", gone)
		}
	}
	for _, want := range []string{`id="card-panel-title-input"`, `@click="closePanel()"`, `<template id="desc-41">`} {
		if !strings.Contains(out, want) {
			t.Errorf("read-only board missing %q", want)
		}
	}
	if !regexp.MustCompile(`id="card-panel-title-input"[^>]*disabled`).MatchString(out) {
		t.Error("read-only panel title input is not disabled")
	}
}
