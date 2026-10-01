package service

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
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
	// repos is nil on purpose: only AggregateForUser and PrimaryLanguage's
	// write-back may touch it, and another method that starts to would panic here.
	svc := NewLanguageService(code, nil)

	comp, err := svc.Composition(context.Background(), &model.Repository{OwnerName: "alice", Name: "lang"}, "")
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
	svc := NewLanguageService(code, nil)

	pcts, err := svc.Percentages(context.Background(), &model.Repository{OwnerName: "bob", Name: "pct"}, "")
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
	svc := NewLanguageService(code, nil)
	repo := &model.Repository{ID: 7, OwnerName: "carol", Name: "cache"}

	first, err := svc.Composition(context.Background(), repo, "")
	if err != nil {
		t.Fatalf("first Composition: %v", err)
	}
	second, err := svc.Composition(context.Background(), repo, "")
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

	head, _, err := code.ResolveRef("carol", "cache", "")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if _, ok := svc.cache.Load(langCacheKey{repoID: 7, commit: head.Hash}); !ok {
		t.Errorf("expected cache entry under repo ID 7 and the head commit")
	}
}

// Web-UI commits (file edits, merges, profile READMEs) never run the push hook, so the cache must not rely on it.
func TestLanguageService_Composition_SeesWebCommitWithoutInvalidation(t *testing.T) {
	t.Parallel()
	code := newTestRepoWithFiles(t, "dave", "webedit", map[string]string{"index.js": "console.log('hi');\n"})
	svc := NewLanguageService(code, nil)
	repo := &model.Repository{ID: 8, OwnerName: "dave", Name: "webedit", DefaultBranch: "master"}

	if pcts, err := svc.Percentages(context.Background(), repo, repo.DefaultBranch); err != nil || langNames(pcts) != "JavaScript" {
		t.Fatalf("before commit: Percentages = %+v, %v; want JavaScript", pcts, err)
	}
	if err := code.CommitFile("dave", "webedit", "master", "main.go", []byte("package main\n\nfunc main() {}\n"), GitAuthor{Name: "Tester", Email: "tester@example.com"}, "add main.go"); err != nil {
		t.Fatalf("CommitFile: %v", err)
	}
	if pcts, err := svc.Percentages(context.Background(), repo, repo.DefaultBranch); err != nil || !strings.Contains(langNames(pcts), "Go") {
		t.Errorf("after commit: Percentages = %+v, %v; want Go included", pcts, err)
	}
}

func TestLanguageService_StoreEntry_SweepsExpiredCommits(t *testing.T) {
	t.Parallel()
	svc := NewLanguageService(nil, nil)
	old := langCacheKey{repoID: 1, commit: plumbing.NewHash("1111111111111111111111111111111111111111")}
	fresh := langCacheKey{repoID: 1, commit: plumbing.NewHash("2222222222222222222222222222222222222222")}
	svc.cache.Store(old, cacheEntry{cachedAt: time.Now().Add(-langCacheTTL)})
	svc.cacheInserts.Store(langCacheSweepEvery - 1)

	svc.storeEntry(fresh, cacheEntry{cachedAt: time.Now()})

	if _, ok := svc.cache.Load(old); ok {
		t.Error("expired entry for a superseded commit survived the sweep")
	}
	if _, ok := svc.cache.Load(fresh); !ok {
		t.Error("the entry just stored was swept")
	}
}

func TestLanguageService_TopLanguageFor(t *testing.T) {
	t.Parallel()

	bigGo := make([]byte, 500)
	for i := range bigGo {
		bigGo[i] = 'a'
	}
	files := map[string]string{
		"main.go": "package main\n" + string(bigGo),
		"app.py":  "print('hi')\n",
	}
	code := newTestRepoWithFiles(t, "alice", "demo", files)
	svc := NewLanguageService(code, nil)

	top, err := svc.TopLanguageFor(context.Background(), &model.Repository{OwnerName: "alice", Name: "demo"}, "")
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
	emptySvc := NewLanguageService(emptyCode, nil)
	top, err = emptySvc.TopLanguageFor(context.Background(), &model.Repository{OwnerName: "alice", Name: "empty"}, "")
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
	svc := NewLanguageService(code, repoSvc)

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
	testutil.DeleteOrgOnCleanup(t, db, org.ID)
	for _, r := range []struct {
		name, lang string
		private    bool
	}{{"web", "Go", false}, {"cli", "Go", false}, {"internal", "Python", true}} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO repositories (created_by, owner_name, org_id, name, description, private, default_branch, primary_language)
			 VALUES ($1, $2, $3, $4, '', $5, 'main', $6)`,
			ownerID, org.Name, org.ID, r.name, r.private, r.lang,
		); err != nil {
			t.Fatalf("insert org repo %s: %v", r.name, err)
		}
	}

	svc := NewLanguageService(nil, nil)

	for _, tc := range []struct {
		name   string
		viewer *int64
		want   []LangPercent
	}{
		{"visitor", &visitorID, []LangPercent{{Name: "Go", Percent: 100}}},
		{"owner", &ownerID, []LangPercent{{Name: "Go", Percent: 66}, {Name: "Python", Percent: 33}}},
	} {
		repos, err := orgSvc.ListReposVisibleTo(ctx, org.ID, tc.viewer)
		if err != nil {
			t.Fatalf("%s: ListReposVisibleTo: %v", tc.name, err)
		}
		if got := svc.AggregateForOrg(ctx, repos, 5); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestLanguageService_PrimaryLanguage_FillsEmptyColumn(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix

	code := newTestRepoWithFiles(t, owner, "app", map[string]string{"main.go": "package main\n"})
	newTestRepoWithFilesAt(t, code.cfg.ReposRoot, owner, "docs", map[string]string{"README.md": "# docs\n"})
	seedLangRepo(t, db, ownerID, owner, "app", false)
	seedLangRepo(t, db, ownerID, owner, "docs", false)
	repoSvc := NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, code, config.GitConfig{ReposRoot: code.cfg.ReposRoot})
	svc := NewLanguageService(code, repoSvc)

	null := sql.NullString{}
	text := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
	for _, tc := range []struct {
		name, repo  string
		column      sql.NullString
		staleColumn bool // the page loaded the row before a push wrote it
		want        string
		wantStored  sql.NullString
	}{
		{"nil column", "app", null, false, "Go", text("Go")},
		{"empty column", "app", text(""), false, "Go", text("")},
		{"set column", "app", text("Rust"), false, "Rust", text("Rust")},
		{"filled after load", "app", text("Rust"), true, "Go", text("Rust")},
		{"emptied after load", "app", text(""), true, "Go", text("")},
		{"no code", "docs", null, false, "", null},
	} {
		if _, err := db.ExecContext(ctx,
			`UPDATE repositories SET primary_language = $3 WHERE owner_name = $1 AND name = $2`,
			owner, tc.repo, tc.column,
		); err != nil {
			t.Fatalf("%s: set column: %v", tc.name, err)
		}
		byName, err := repoSvc.Get(ctx, owner, tc.repo)
		if err != nil {
			t.Fatalf("%s: get repo: %v", tc.name, err)
		}
		repo, err := repoSvc.GetByID(ctx, byName.ID)
		if err != nil {
			t.Fatalf("%s: get repo by id: %v", tc.name, err)
		}
		if tc.staleColumn {
			repo.PrimaryLanguage = nil
		}
		if got := svc.PrimaryLanguage(ctx, repo); got != tc.want {
			t.Errorf("%s: PrimaryLanguage = %q, want %q", tc.name, got, tc.want)
		}
		var stored sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT primary_language FROM repositories WHERE id = $1`, repo.ID).Scan(&stored); err != nil {
			t.Fatalf("%s: read column: %v", tc.name, err)
		}
		if stored != tc.wantStored {
			t.Errorf("%s: stored primary_language = %+v, want %+v", tc.name, stored, tc.wantStored)
		}
	}
}

// A transfer hands the old name to a new repo, which must not store the old tree's cached language.
func TestLanguageService_PrimaryLanguage_NameReusedAfterTransfer(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	aliceID := testutil.SeedUser(t, db, suffix)
	alice := "testuser_" + suffix
	testutil.SeedUser(t, db, suffix+"_b")
	bob := "testuser_" + suffix + "_b"

	root := t.TempDir()
	code := NewCodeService(config.GitConfig{ReposRoot: root})
	repoSvc := NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, code, config.GitConfig{ReposRoot: root}).
		WithTransferStore(store.NewRepoTransferStore(db))
	svc := NewLanguageService(code, repoSvc)
	repoSvc.WithLanguageService(svc)

	old, err := repoSvc.Create(ctx, aliceID, alice, "foo", "", false, RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create old repo: %v", err)
	}
	if err := code.CommitFile(alice, "foo", old.DefaultBranch, "main.go", []byte("package main\n"), GitAuthor{Name: "Tester", Email: "tester@example.com"}, "add main"); err != nil {
		t.Fatalf("commit main.go: %v", err)
	}
	if pcts, err := svc.AggregateForUser(ctx, alice, nil, 0); err != nil || langNames(pcts) != "Go" {
		t.Fatalf("profile languages before transfer = %+v, %v; want Go", pcts, err)
	}
	transfer, err := repoSvc.TransferRepo(ctx, old, aliceID, bob)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if _, err := repoSvc.AcceptTransfer(ctx, transfer.ID, transfer.RecipientID, transfer.FullName()); err != nil {
		t.Fatalf("accept transfer: %v", err)
	}

	fresh, err := repoSvc.Create(ctx, aliceID, alice, "foo", "", false, RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create repo under the freed name: %v", err)
	}
	if got := svc.PrimaryLanguage(ctx, fresh); got != "" {
		t.Errorf("PrimaryLanguage of README-only repo = %q, want none", got)
	}
	var stored sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT primary_language FROM repositories WHERE id = $1`, fresh.ID).Scan(&stored); err != nil {
		t.Fatalf("read column: %v", err)
	}
	if stored.Valid {
		t.Errorf("stored primary_language = %q, want NULL", stored.String)
	}
}

// A page view before the push caches the README-only tree; the push must not store that stale result.
func TestRepoService_OnPostReceive_PrimaryLanguageFromPushedTree(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix

	root := t.TempDir()
	bareDir := newTestRepoWithFilesAt(t, root, owner, "pushed", map[string]string{"README.md": "# pushed\n"})
	seedLangRepo(t, db, ownerID, owner, "pushed", false)

	code := NewCodeService(config.GitConfig{ReposRoot: root})
	users := store.NewUserStore(db)
	repoSvc := NewRepoService(store.NewRepoStore(db), users, store.NewOrgStore(db),
		NewContributorStatsService(store.NewContributorStatsStore(db), users), code, config.GitConfig{ReposRoot: root})
	langSvc := NewLanguageService(code, repoSvc)
	repoSvc.WithLanguageService(langSvc)
	repo, err := repoSvc.Get(ctx, owner, "pushed")
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}

	if pcts, err := langSvc.Percentages(ctx, repo, "master"); err != nil || len(pcts) != 0 {
		t.Fatalf("pre-push Percentages = %+v, %v; want none", pcts, err)
	}

	gitRepo, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("open bare: %v", err)
	}
	branch := plumbing.NewBranchReferenceName("master")
	before, err := gitRepo.Reference(branch, true)
	if err != nil {
		t.Fatalf("resolve master: %v", err)
	}
	if err := code.CommitFile(owner, "pushed", "master", "main.go", []byte("package main\n"), GitAuthor{Name: "Tester", Email: "tester@example.com"}, "add main"); err != nil {
		t.Fatalf("commit main.go: %v", err)
	}
	after, err := gitRepo.Reference(branch, true)
	if err != nil {
		t.Fatalf("resolve master after commit: %v", err)
	}

	cmds := []*packp.Command{{Name: branch, Old: before.Hash(), New: after.Hash()}}
	if err := repoSvc.OnPostReceive(ctx, repo, gitRepo, cmds); err != nil {
		t.Fatalf("OnPostReceive: %v", err)
	}

	var got sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT primary_language FROM repositories WHERE id = $1`, repo.ID).Scan(&got); err != nil {
		t.Fatalf("read primary_language: %v", err)
	}
	if got.String != "Go" {
		t.Errorf("primary_language after push = %q, want %q", got.String, "Go")
	}
	if pcts, err := langSvc.Percentages(ctx, repo, "master"); err != nil || langNames(pcts) != "Go" {
		t.Errorf("post-push Percentages = %+v, %v; want Go", pcts, err)
	}
}
