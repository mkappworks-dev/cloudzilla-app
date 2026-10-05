package components

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestSelectMenu(t *testing.T) {
	t.Parallel()
	out := renderToString(t, SelectMenu("theme", "code_theme_dark", "Dark mode code theme", []SelectMenuOption{
		{Value: "nord", Label: "Nord", Swatch: []string{"#2e3440", "#81a1c1"}},
		{Value: "plain", Label: "Plain <b>"},
	}, "nord", templ.Attributes{"onchange": "pick(this.value)"}))

	for _, want := range []string{
		`<input type="hidden" name="code_theme_dark" value="nord" x-ref="input" onchange="pick(this.value)">`,
		`<button type="button" id="theme"`,
		`role="listbox"`,
		`aria-label="Dark mode code theme"`,
		`id="theme-opt-0" role="option" data-value="nord" aria-selected="true"`,
		`id="theme-opt-1" role="option" data-value="plain" aria-selected="false"`,
		`style="background-color: #2e3440;"`,
		`Plain &lt;b&gt;`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("SelectMenu output lacks %q\n%s", want, out)
		}
	}
	trigger := out[strings.Index(out, `x-ref="current"`):strings.Index(out, `role="listbox"`)]
	if !strings.Contains(trigger, ">Nord<") || strings.Contains(trigger, "Plain") {
		t.Errorf("trigger should show only the selected option, got %s", trigger)
	}
}
