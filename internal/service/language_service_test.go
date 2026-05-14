package service

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

func TestLanguageService_Composition(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"main.go":         "package main\n\nfunc main() {}\n", // 30 bytes Go
		"util.go":         "package main\n\nfunc util() {}\n", // 30 bytes Go
		"README.md":       "hello hi\n\n",                     // 10 bytes markdown
		"static/index.js": "console.log('hi');\n\n",           // 20 bytes JS
	}
	code := newTestRepoWithFiles(t, "alice", "lang", files)
	svc := NewLanguageService(code)

	comp, err := svc.Composition(context.Background(), "alice", "lang", "")
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	if comp["Go"] <= 0 {
		t.Errorf("expected Go > 0, got %d", comp["Go"])
	}
	if comp["JavaScript"] <= 0 {
		t.Errorf("expected JavaScript > 0, got %d", comp["JavaScript"])
	}
	if _, ok := comp["Markdown"]; ok {
		t.Errorf("Markdown should be excluded from composition; got %+v", comp)
	}
}

func TestLanguageService_Percentages_SortedDesc(t *testing.T) {
	t.Parallel()
	// Skew so Go is the clear majority.
	bigGo := make([]byte, 1000)
	for i := range bigGo {
		bigGo[i] = 'a'
	}
	files := map[string]string{
		"main.go":         "package main\n" + string(bigGo),
		"static/index.js": "console.log('hi');\n",
		"app.py":          "print('hi')\n",
	}
	code := newTestRepoWithFiles(t, "bob", "pct", files)
	svc := NewLanguageService(code)

	pcts, err := svc.Percentages(context.Background(), "bob", "pct", "")
	if err != nil {
		t.Fatalf("Percentages: %v", err)
	}
	if len(pcts) == 0 {
		t.Fatal("expected at least one language")
	}
	if !sort.SliceIsSorted(pcts, func(i, j int) bool {
		if pcts[i].Percent != pcts[j].Percent {
			return pcts[i].Percent > pcts[j].Percent
		}
		return pcts[i].Name < pcts[j].Name
	}) {
		t.Errorf("not sorted desc: %+v", pcts)
	}
	for _, p := range pcts {
		if p.Percent <= 0 {
			t.Errorf("expected positive percent, got %d for %s", p.Percent, p.Name)
		}
	}
	if pcts[0].Name != "Go" {
		t.Errorf("expected Go to be top language, got %s", pcts[0].Name)
	}
}

func TestLanguageService_Composition_CachesResults(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"main.go": "package main\n",
	}
	code := newTestRepoWithFiles(t, "carol", "cache", files)
	svc := NewLanguageService(code)

	first, err := svc.Composition(context.Background(), "carol", "cache", "")
	if err != nil {
		t.Fatalf("first Composition: %v", err)
	}
	second, err := svc.Composition(context.Background(), "carol", "cache", "")
	if err != nil {
		t.Fatalf("second Composition: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Errorf("expected equal maps; first=%+v second=%+v", first, second)
	}
	// Cache hit returns the same underlying map; assert identity by writing
	// to first and seeing the change reflected in second.
	first["__sentinel__"] = 42
	if second["__sentinel__"] != 42 {
		t.Errorf("expected second call to return cached (same) map, but maps are independent")
	}
	delete(first, "__sentinel__")

	// Also assert the cache entry is present under the expected key.
	if _, ok := svc.cache.Load("carol/cache:"); !ok {
		t.Errorf("expected cache entry under key carol/cache:")
	}
}
