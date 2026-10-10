package components_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

func renderDatePicker(t *testing.T, p components.DatePickerProps) string {
	t.Helper()
	var sb strings.Builder
	if err := components.DatePicker(p).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func TestDatePicker_Enabled(t *testing.T) {
	out := renderDatePicker(t, components.DatePickerProps{
		ID: "due", Name: "due_date", Label: "Due date", Value: "2026-10-14", Placeholder: "No due date",
		Attrs: templ.Attributes{"x-model": "editor.dueDate"},
	})
	for _, want := range []string{
		`x-data="datePicker"`, `x-modelable="value"`, `x-model="editor.dueDate"`, `data-value="2026-10-14"`,
		`data-placeholder="No due date"`,
		`id="due"`, `aria-haspopup="dialog"`, `x-bind:aria-expanded="open.toString()"`,
		`<input type="hidden" name="due_date" value="2026-10-14"`, `x-bind:value="value"`,
		"Oct 14, 2026",
		`role="dialog"`, `aria-label="Choose due date"`, `role="grid"`, `role="gridcell"`,
		`x-bind:aria-selected=`, `x-bind:aria-current=`, `x-bind:aria-label="cell.full"`,
		`aria-label="Previous month"`, `aria-label="Next month"`, `x-text="heading"`,
		"Today</button>", "Clear</button>", `@click.outside="close(false)"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("enabled date picker missing %q", want)
		}
	}
	heads := regexp.MustCompile(`role="columnheader"[^>]*>(\w+)<`).FindAllStringSubmatch(out, -1)
	if len(heads) != 7 || heads[0][1] != "Mo" || heads[6][1] != "Su" {
		t.Errorf("weekday headers = %v, want Mo..Su", heads)
	}
	if regexp.MustCompile(`<button[^>]*id="due"[^>]*\sdisabled[\s>]`).MatchString(out) {
		t.Error("enabled trigger is disabled")
	}
}

func TestDatePicker_DisabledShowsTheDateWithoutPopover(t *testing.T) {
	out := renderDatePicker(t, components.DatePickerProps{ID: "due", Name: "due_date", Label: "Due date", Value: "2026-03-31", Disabled: true, Placeholder: "No due date"})
	if !regexp.MustCompile(`<button[^>]*id="due"[^>]*\sdisabled[\s>]`).MatchString(out) {
		t.Error("disabled trigger is not disabled")
	}
	for _, want := range []string{"Mar 31, 2026", `<input type="hidden" name="due_date" value="2026-03-31"`} {
		if !strings.Contains(out, want) {
			t.Errorf("disabled date picker missing %q", want)
		}
	}
	for _, gone := range []string{`role="dialog"`, `role="grid"`, "Clear</button>", "Today</button>"} {
		if strings.Contains(out, gone) {
			t.Errorf("disabled date picker renders %q", gone)
		}
	}
}

// kanban's client formatShort prints the year without padding, so the server label must match.
func TestDatePicker_LabelHasNoYearPadding(t *testing.T) {
	out := renderDatePicker(t, components.DatePickerProps{ID: "due", Name: "due_date", Label: "Due date", Value: "0005-03-07", Placeholder: "No due date"})
	if !strings.Contains(out, ">Mar 7, 5<") || strings.Contains(out, "0005,") {
		t.Errorf("year 5 label is not %q: %s", "Mar 7, 5", out)
	}
}

func TestDatePicker_EmptyAndInvalidValuesShowThePlaceholder(t *testing.T) {
	for _, v := range []string{"", "2026-02-30", "0000-01-01", "not a date"} {
		out := renderDatePicker(t, components.DatePickerProps{ID: "due", Name: "due_date", Label: "Due date", Value: v, Placeholder: "No due date"})
		if !strings.Contains(out, ">No due date<") {
			t.Errorf("value %q: placeholder not shown", v)
		}
	}
}
