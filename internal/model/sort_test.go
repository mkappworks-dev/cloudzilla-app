package model_test

import (
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestSortSpec(t *testing.T) {
	spec := model.SortSpec{{Key: "created", DescFirst: true}, {Key: "name"}}
	for _, tc := range []struct {
		key, dir string
		wantKey  string
		wantDesc bool
	}{
		{"", "", "created", true},
		{"bogus", "sideways", "created", true},
		{"name", "", "name", false},
		{"name", "desc", "name", true},
		{"created", "asc", "created", false},
		{"", "asc", "created", false},
	} {
		if k, d := spec.Resolve(tc.key, tc.dir); k != tc.wantKey || d != tc.wantDesc {
			t.Errorf("Resolve(%q, %q) = %q, %v; want %q, %v", tc.key, tc.dir, k, d, tc.wantKey, tc.wantDesc)
		}
	}
	if !spec.Toggle("name", false, "created") {
		t.Error("switching to created should start newest first")
	}
	if spec.Toggle("name", false, "name") != true || spec.Toggle("name", true, "name") != false {
		t.Error("clicking the sorted column should flip its direction")
	}
}
