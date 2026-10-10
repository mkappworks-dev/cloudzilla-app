package components_test

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

func renderMultiSelect(t *testing.T, p components.MultiSelectProps) string {
	t.Helper()
	var sb strings.Builder
	if err := components.MultiSelect(p).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func TestMultiSelect_Structure(t *testing.T) {
	out := renderMultiSelect(t, components.MultiSelectProps{
		ID: "ms", LabelID: "ms-label", Placeholder: "Nobody", Empty: "No matches",
		Options: []components.MultiSelectOption{
			{Value: "1", Label: "bug", Style: "background-color: #d73a4a; color: #ffffff"},
			{Value: "2", Label: "ann", Avatar: true},
		},
		Attrs: templ.Attributes{"x-model": "editor.things"},
	})
	for _, want := range []string{
		`x-data="multiSelect"`, `x-modelable="selected"`, `x-model="editor.things"`,
		`id="ms"`, `aria-haspopup="listbox"`, `x-bind:aria-expanded="open.toString()"`, `aria-labelledby="ms-label ms"`,
		`role="listbox"`, `aria-multiselectable="true"`, `id="ms-list"`,
		`id="ms-opt-1"`, `id="ms-opt-2"`, `role="option"`, `data-value="1"`, `data-label="bug"`,
		`x-bind:aria-selected="has($el.dataset.value).toString()"`,
		`style="background-color: #d73a4a; color: #ffffff;"`, "Nobody", "No matches",
		`@keydown.escape.prevent.stop="close()"`, `@keydown.tab="close(false)"`, `@click.outside="close(false)"`,
		`@keydown.arrow-down.prevent="show()"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("multi-select missing %q", want)
		}
	}
	if strings.Contains(out, `type="checkbox"`) {
		t.Error("multi-select renders checkboxes")
	}
	if !regexp.MustCompile(`<li[^>]*role="option"[^>]*@mousedown.prevent`).MatchString(out) {
		t.Error("options take focus on mousedown, so the popover would lose its keyboard focus")
	}
}

func TestMultiSelect_FilterBoxOnlyAboveTheThreshold(t *testing.T) {
	options := func(n int) []components.MultiSelectOption {
		var o []components.MultiSelectOption
		for i := range n {
			o = append(o, components.MultiSelectOption{Value: strconv.Itoa(i), Label: "opt" + strconv.Itoa(i)})
		}
		return o
	}
	if strings.Contains(renderMultiSelect(t, components.MultiSelectProps{ID: "ms", Options: options(6)}), "data-multiselect-filter") {
		t.Error("six options get a filter box")
	}
	if !strings.Contains(renderMultiSelect(t, components.MultiSelectProps{ID: "ms", Options: options(7)}), "data-multiselect-filter") {
		t.Error("seven options get no filter box")
	}
}
