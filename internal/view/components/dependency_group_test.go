package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDependencyGroup_RendersRows(t *testing.T) {
	d := DependencyGroupData{
		PackageMgr: "go",
		Label:      "go.mod",
		Rows: []DependencyRow{
			{Package: "github.com/foo/bar", Version: "v1.2.3", IsDev: false},
			{Package: "github.com/baz/qux", Version: "v0.9.0", IsDev: true},
		},
	}

	var buf bytes.Buffer
	DependencyGroup(d).Render(context.Background(), &buf)
	out := buf.String()

	for _, want := range []string{
		"github.com/foo/bar",
		"v1.2.3",
		"github.com/baz/qux",
		"v0.9.0",
		"pill-dev",
		"pill-prod",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}
