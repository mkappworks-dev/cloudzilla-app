package components

import (
	"bytes"
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

var anchorTag = regexp.MustCompile(`<a\b[^>]*>`)

func refAnchor(t *testing.T, out, ref string) string {
	t.Helper()
	for _, a := range anchorTag.FindAllString(out, -1) {
		if strings.Contains(a, `data-ref="`+ref+`"`) {
			return a
		}
	}
	t.Fatalf("no menu item for %q in: %s", ref, out)
	return ""
}

func highlighted(anchor string) bool {
	m := classAttr.FindStringSubmatch(anchor)
	return m != nil && slices.Contains(strings.Fields(m[1]), "bg-accent")
}

func TestRefPicker_ListsEveryBranchAndTagAndMarksCurrent(t *testing.T) {
	var buf bytes.Buffer
	err := RefPicker(RefPickerProps{
		Current: "feature-x",
		Branches: []service.BranchInfo{
			{Name: "main", IsDefault: true},
			{Name: "feature-x"},
			{Name: "fix/Login"},
		},
		Tags:    []service.TagInfo{{Name: "v1.0"}},
		Href:    func(ref string) templ.SafeURL { return templ.SafeURL("/o/r/blob/" + ref + "/a.go") },
		RefsURL: "/o/r/refs",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	for ref, href := range map[string]string{
		"main":      "/o/r/blob/main/a.go",
		"feature-x": "/o/r/blob/feature-x/a.go",
		"fix/login": "/o/r/blob/fix/Login/a.go",
		"v1.0":      "/o/r/blob/v1.0/a.go",
	} {
		if a := refAnchor(t, out, ref); !strings.Contains(a, `href="`+href+`"`) {
			t.Errorf("%s item = %s, want href %q", ref, a, href)
		}
	}
	if a := refAnchor(t, out, "feature-x"); !highlighted(a) {
		t.Errorf("current ref not highlighted: %s", a)
	}
	if a := refAnchor(t, out, "main"); highlighted(a) {
		t.Errorf("non-current default branch highlighted: %s", a)
	}
	if !strings.Contains(out, `aria-label="Switch branch or tag, current: feature-x"`) {
		t.Errorf("trigger does not name the current ref: %s", out)
	}
	if !strings.Contains(out, `href="/o/r/refs"`) {
		t.Errorf("missing View all branches link: %s", out)
	}
}
