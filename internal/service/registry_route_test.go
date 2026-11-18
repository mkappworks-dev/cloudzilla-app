package service

import (
	"slices"
	"strings"
	"testing"
)

func TestParseRegistryRoute(t *testing.T) {
	d := "sha256:" + "a1"
	tests := []struct {
		path string
		want RegistryRoute
		ok   bool
	}{
		{"o/img/tags/list", RegistryRoute{"o/img", RegistryOpTags, ""}, true},
		{"o/img/manifests/latest", RegistryRoute{"o/img", RegistryOpManifest, "latest"}, true},
		{"o/img/blobs/" + d, RegistryRoute{"o/img", RegistryOpBlob, d}, true},
		{"o/img/blobs/uploads/", RegistryRoute{"o/img", RegistryOpUploadStart, ""}, true},
		{"o/img/blobs/uploads", RegistryRoute{"o/img", RegistryOpUploadStart, ""}, true},
		{"o/img/blobs/uploads/u-1", RegistryRoute{"o/img", RegistryOpUpload, "u-1"}, true},
		{"o/img/referrers/" + d, RegistryRoute{"o/img", RegistryOpReferrers, d}, true},
		{"o/blobs/img/blobs/" + d, RegistryRoute{"o/blobs/img", RegistryOpBlob, d}, true},
		{"o/manifests/tags/list/tags/list", RegistryRoute{"o/manifests/tags/list", RegistryOpTags, ""}, true},
		{"o/blobs/uploads/manifests/v1", RegistryRoute{"o/blobs/uploads", RegistryOpManifest, "v1"}, true},
		{"_catalog", RegistryRoute{}, false},
		{"o/img", RegistryRoute{}, false},
		{"tags/list", RegistryRoute{}, false},
		{"manifests/latest", RegistryRoute{}, false},
	}
	for _, tc := range tests {
		got, ok := ParseRegistryRoute(tc.path)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseRegistryRoute(%q) = %+v %v, want %+v %v", tc.path, got, ok, tc.want, tc.ok)
		}
	}
}

func TestValidRegistryName(t *testing.T) {
	long := "o/" + strings.Repeat("a", 254)
	for name, want := range map[string]bool{
		"alice/app": true, "alice/app/sub": true, "a.b/c_d/e__f/g--h": true, "alice/Bad": false, "alice//x": false,
		"alice/-x": false, "alice/x_": false, "": false, long: false,
	} {
		if got := ValidRegistryName(name); got != want {
			t.Errorf("ValidRegistryName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestParseRegistryScope(t *testing.T) {
	got, ok := ParseRegistryScope("repository:alice/app:pull,push,pull,bogus")
	if !ok || got.Name != "alice/app" || !slices.Equal(got.Actions, []string{"pull", "push"}) {
		t.Errorf("got %+v %v, want alice/app with pull and push", got, ok)
	}
	for _, s := range []string{"registry:catalog:*", "repository:alice/app", "repository::pull", "alice/app:pull"} {
		if _, ok := ParseRegistryScope(s); ok {
			t.Errorf("ParseRegistryScope(%q) accepted", s)
		}
	}
}
