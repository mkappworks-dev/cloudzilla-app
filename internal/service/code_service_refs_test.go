package service

import "testing"

func TestSplitRefPath(t *testing.T) {
	r := newTipTestRepo(t)
	if err := r.svc.CreateBranch(r.owner, r.name, "fix/render-cache", "main"); err != nil {
		t.Fatalf("create fix/render-cache: %v", err)
	}
	if err := r.svc.CreateTag(r.owner, r.name, "v1/rc", "main"); err != nil {
		t.Fatalf("create v1/rc: %v", err)
	}

	tests := []struct{ in, ref, path string }{
		{"main", "main", ""},
		{"main/lib/a.go", "main", "lib/a.go"},
		{"fix/render-cache", "fix/render-cache", ""},
		{"fix/render-cache/", "fix/render-cache", ""},
		{"fix/render-cache/lib/a.go", "fix/render-cache", "lib/a.go"},
		{"v1/rc/a.go", "v1/rc", "a.go"},
		// A prefix only matches at a segment boundary.
		{"fix/render-cache-old/a.go", "fix", "render-cache-old/a.go"},
		// SHAs and unknown refs keep the first segment, as before.
		{r.mainTip.String() + "/a.txt", r.mainTip.String(), "a.txt"},
		{"nope/a.go", "nope", "a.go"},
	}
	check := func(t *testing.T) {
		for _, tt := range tests {
			ref, path := r.svc.SplitRefPath(r.owner, r.name, tt.in)
			if ref != tt.ref || path != tt.path {
				t.Errorf("SplitRefPath(%q) = %q, %q; want %q, %q", tt.in, ref, path, tt.ref, tt.path)
			}
		}
	}
	t.Run("loose refs", check)
	t.Run("packed refs", func(t *testing.T) {
		if err := r.repo.Storer.(interface{ PackRefs() error }).PackRefs(); err != nil {
			t.Fatalf("pack refs: %v", err)
		}
		check(t)
	})
}
