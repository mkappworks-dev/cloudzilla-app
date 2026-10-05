package components

import (
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func TestDiffHunkTable_RendersHighlightedAndPlainLines(t *testing.T) {
	t.Parallel()
	const hl = `<span class="hl-k">int</span>`
	out := renderToString(t, DiffHunkTable([]service.DiffHunk{{
		Header: "@@ -1,2 +1,2 @@",
		Lines: []service.DiffLine{
			{Type: "ctx", Content: "int", OldNum: 1, NewNum: 1, HTML: hl},
			{Type: "add", Content: "a<b", NewNum: 2},
		},
	}}))

	for _, want := range []string{
		`class="hl overflow-x-auto"`,
		`whitespace-pre align-top">` + hl + `</td>`,
		`whitespace-pre align-top">a&lt;b</td>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("diff table missing %q:\n%s", want, out)
		}
	}
}
