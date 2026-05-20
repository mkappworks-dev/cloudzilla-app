package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	gitobj "github.com/go-git/go-git/v5/plumbing/object"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
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
	svc := NewLanguageService(code, nil)

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
	svc := NewLanguageService(code, nil)

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
	svc := NewLanguageService(code, nil)

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
	svc := NewLanguageService(code, nil)

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
	emptySvc := NewLanguageService(emptyCode, nil)
	top, err = emptySvc.TopLanguageFor(context.Background(), "alice", "empty", "")
	if err != nil {
		t.Fatalf("TopLanguageFor empty: %v", err)
	}
	if top != "" {
		t.Errorf("top language for empty repo: want %q, got %q", "", top)
	}
}

// addBareRepoWithFiles seeds an additional bare repo under an existing repos
// root (so multiple repos share the same ReposRoot — required for the
// AggregateForUser test which queries by owner_id).
func addBareRepoWithFiles(t *testing.T, root, owner, name string, files map[string]string) {
	t.Helper()
	bareDir := filepath.Join(root, owner, name+".git")
	if err := os.MkdirAll(filepath.Dir(bareDir), 0o755); err != nil {
		t.Fatalf("mkdir owner: %v", err)
	}
	if _, err := gogit.PlainInit(bareDir, true); err != nil {
		t.Fatalf("plain init bare: %v", err)
	}

	workDir := t.TempDir()
	work, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("plain init work: %v", err)
	}
	wt, err := work.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	for relPath, content := range files {
		full := filepath.Join(workDir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir parent for %s: %v", relPath, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
		if _, err := wt.Add(relPath); err != nil {
			t.Fatalf("add %s: %v", relPath, err)
		}
	}
	sig := &gitobj.Signature{Name: "Tester", Email: "tester@example.com", When: time.Now().UTC()}
	if _, err := wt.Commit("seed", &gogit.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{
		Name: "bare",
		URLs: []string{bareDir},
	}); err != nil {
		t.Fatalf("create remote: %v", err)
	}
	if err := work.Push(&gogit.PushOptions{
		RemoteName: "bare",
		RefSpecs:   []gitconfig.RefSpec{"refs/heads/master:refs/heads/master"},
	}); err != nil {
		t.Fatalf("push: %v", err)
	}
	bare, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("open bare: %v", err)
	}
	headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("master"))
	if err := bare.Storer.SetReference(headRef); err != nil {
		t.Fatalf("set HEAD: %v", err)
	}
}

func TestLanguageService_AggregateForUser(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set; skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test db: %v", err)
	}

	ctx := context.Background()
	suffix := fmt.Sprintf("%d", os.Getpid())
	username := "langagg_" + suffix

	var userID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		username, username+"@test.invalid",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	defer func() {
		db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	}()

	// Build repo #1 (Go-heavy) — this also creates the shared tempdir root.
	bigGo := make([]byte, 800)
	for i := range bigGo {
		bigGo[i] = 'a'
	}
	repo1Files := map[string]string{
		"main.go": "package main\n" + string(bigGo),
	}
	code := newTestRepoWithFiles(t, username, "repo1", repo1Files)
	root := code.cfg.ReposRoot

	// Build repo #2 (Python) under the same root.
	repo2Files := map[string]string{
		"app.py": "print('hello world')\n",
	}
	addBareRepoWithFiles(t, root, username, "repo2", repo2Files)

	// Insert matching repository rows.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'master')`,
		userID, username, "repo1",
	); err != nil {
		t.Fatalf("insert repo1: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'master')`,
		userID, username, "repo2",
	); err != nil {
		t.Fatalf("insert repo2: %v", err)
	}

	repoStore := store.NewRepoStore(db)
	// Reuse the CodeService returned from newTestRepoWithFiles — same root.
	svc := NewLanguageService(code, repoStore)

	pcts, err := svc.AggregateForUser(ctx, userID, 5)
	if err != nil {
		t.Fatalf("AggregateForUser: %v", err)
	}
	if len(pcts) == 0 {
		t.Fatal("expected at least one language in aggregate")
	}

	got := make(map[string]int, len(pcts))
	var sum int
	for _, p := range pcts {
		got[p.Name] = p.Percent
		sum += p.Percent
	}
	if got["Go"] == 0 {
		t.Errorf("expected Go > 0 in aggregate, got %+v", got)
	}
	if got["Python"] == 0 {
		t.Errorf("expected Python > 0 in aggregate, got %+v", got)
	}
	if sum > 100 {
		t.Errorf("percent sum > 100: %d (%+v)", sum, got)
	}
}
