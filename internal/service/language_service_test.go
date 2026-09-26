package service

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
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
	// repos and orgs are nil on purpose: only the aggregates may touch them, and
	// a per-repo method that starts to would panic here.
	svc := NewLanguageService(code, nil, nil)

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
	svc := NewLanguageService(code, nil, nil)

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
	svc := NewLanguageService(code, nil, nil)

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

func TestLanguageService_TopLanguageFor(t *testing.T) {
	t.Parallel()

	// Go content is much larger than Python.
	bigGo := make([]byte, 500)
	for i := range bigGo {
		bigGo[i] = 'a'
	}
	files := map[string]string{
		"main.go": "package main\n" + string(bigGo),
		"app.py":  "print('hi')\n",
	}
	code := newTestRepoWithFiles(t, "alice", "demo", files)
	svc := NewLanguageService(code, nil, nil)

	top, err := svc.TopLanguageFor(context.Background(), "alice", "demo", "")
	if err != nil {
		t.Fatalf("TopLanguageFor: %v", err)
	}
	if top != "Go" {
		t.Errorf("top language: want Go, got %q", top)
	}

	// Empty repo (no detected code) → "" with nil error.
	emptyFiles := map[string]string{
		"README.md": "just docs\n",
	}
	emptyCode := newTestRepoWithFiles(t, "alice", "empty", emptyFiles)
	emptySvc := NewLanguageService(emptyCode, nil, nil)
	top, err = emptySvc.TopLanguageFor(context.Background(), "alice", "empty", "")
	if err != nil {
		t.Fatalf("TopLanguageFor empty: %v", err)
	}
	if top != "" {
		t.Errorf("top language for empty repo: want %q, got %q", "", top)
	}
}

func TestRankLanguages_PercentagesOverKeptSlice(t *testing.T) {
	t.Parallel()
	weights := map[string]int64{"Go": 500, "Python": 300, "Rust": 200}

	got := rankLanguages(weights, 2)
	want := []LangPercent{{Name: "Go", Percent: 62}, {Name: "Python", Percent: 37}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("limit 2: got %+v, want %+v (percent of the kept 800 bytes, not all 1000)", got, want)
	}

	got = rankLanguages(weights, 0)
	want = []LangPercent{{Name: "Go", Percent: 50}, {Name: "Python", Percent: 30}, {Name: "Rust", Percent: 20}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("no limit: got %+v, want %+v", got, want)
	}

	if got := rankLanguages(map[string]int64{}, 5); len(got) != 0 {
		t.Errorf("empty weights: got %+v, want none", got)
	}
}

func seedLangRepo(t *testing.T, db *sql.DB, ownerID int64, ownerName, name string, private bool) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', $4, 'master')`,
		ownerID, ownerName, name, private,
	); err != nil {
		t.Fatalf("insert repo %s: %v", name, err)
	}
}

func langNames(pcts []LangPercent) string {
	names := make([]string, len(pcts))
	for i, p := range pcts {
		names[i] = p.Name
	}
	return strings.Join(names, ",")
}

func TestLanguageService_AggregateForUser_ViewerVisibility(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	visitorID := testutil.SeedUser(t, db, suffix+"_visitor")

	code := newTestRepoWithFiles(t, owner, "public", map[string]string{"main.go": "package main\n"})
	newTestRepoWithFilesAt(t, code.cfg.ReposRoot, owner, "secret", map[string]string{"app.py": "print('hi')\n"})
	seedLangRepo(t, db, ownerID, owner, "public", false)
	seedLangRepo(t, db, ownerID, owner, "secret", true)

	repoSvc := NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, code, config.GitConfig{ReposRoot: code.cfg.ReposRoot})
	svc := NewLanguageService(code, repoSvc, nil)

	for _, tc := range []struct {
		name   string
		viewer *int64
		want   string
	}{
		{"anonymous", nil, "Go"},
		{"visitor", &visitorID, "Go"},
		{"owner", &ownerID, "Go,Python"},
	} {
		pcts, err := svc.AggregateForUser(ctx, owner, tc.viewer, 5)
		if err != nil {
			t.Fatalf("%s: AggregateForUser: %v", tc.name, err)
		}
		if got := langNames(pcts); got != tc.want {
			t.Errorf("%s: languages = %q, want %q (%+v)", tc.name, got, tc.want, pcts)
		}
	}
}

func TestLanguageService_AggregateForOrg_ViewerVisibility(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	visitorID := testutil.SeedUser(t, db, suffix+"_visitor")

	repoStore := store.NewRepoStore(db)
	orgSvc := NewOrgService(store.NewOrgStore(db), repoStore, store.NewUserStore(db), config.GitConfig{})
	org, err := orgSvc.Create(ctx, ownerID, "testorg_"+suffix, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), `DELETE FROM organizations WHERE id = $1`, org.ID) })
	for _, r := range []struct {
		name, lang string
		private    bool
	}{{"web", "Go", false}, {"cli", "Go", false}, {"internal", "Python", true}} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO repositories (owner_id, owner_name, org_id, name, description, private, default_branch, primary_language)
			 VALUES ($1, $2, $3, $4, '', $5, 'main', $6)`,
			ownerID, org.Name, org.ID, r.name, r.private, r.lang,
		); err != nil {
			t.Fatalf("insert org repo %s: %v", r.name, err)
		}
	}

	svc := NewLanguageService(nil, nil, orgSvc)

	visitor, err := svc.AggregateForOrg(ctx, org.ID, &visitorID, 5)
	if err != nil {
		t.Fatalf("visitor: %v", err)
	}
	if want := []LangPercent{{Name: "Go", Percent: 100}}; !reflect.DeepEqual(visitor, want) {
		t.Errorf("visitor: got %+v, want %+v", visitor, want)
	}

	owner, err := svc.AggregateForOrg(ctx, org.ID, &ownerID, 5)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	if want := []LangPercent{{Name: "Go", Percent: 66}, {Name: "Python", Percent: 33}}; !reflect.DeepEqual(owner, want) {
		t.Errorf("owner: got %+v, want %+v", owner, want)
	}
}
