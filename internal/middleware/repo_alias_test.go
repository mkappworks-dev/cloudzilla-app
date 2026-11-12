package middleware

import (
	"net/url"
	"testing"
)

func TestSplitRepoPath(t *testing.T) {
	tests := []struct {
		path, owner, name, rest string
		ok                      bool
	}{
		{"/acme/api", "acme", "api", "", true},
		{"/acme/api/blob/main/a%20b.md", "acme", "api", "/blob/main/a%20b.md", true},
		{"/acme/api/tree/feat%2Fx/", "acme", "api", "/tree/feat%2Fx/", true},
		{"/api/repos/acme/api", "acme", "api", "", true},
		{"/api/repos/acme/api/issues/1", "acme", "api", "/issues/1", true},
		{"/acme", "", "", "", false},
		{"/", "", "", "", false},
		{"/api/user", "", "", "", false},
		{"/api/repos/acme", "", "", "", false},
		{"/static/js/app.js", "", "", "", false},
		{"/acme/api/info/refs", "", "", "", false},
		{"/acme/api/git-upload-pack", "", "", "", false},
	}
	for _, tc := range tests {
		u, err := url.Parse(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		owner, name, rest, ok := splitRepoPath(u)
		if owner != tc.owner || name != tc.name || rest != tc.rest || ok != tc.ok {
			t.Errorf("splitRepoPath(%q) = %q %q %q %v, want %q %q %q %v", tc.path, owner, name, rest, ok, tc.owner, tc.name, tc.rest, tc.ok)
		}
	}
}
