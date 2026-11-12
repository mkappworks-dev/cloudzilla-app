package service_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type exportEnv struct {
	db      *sql.DB
	svc     *service.ExportService
	backend storage.Backend
}

func newExportEnv(t *testing.T) exportEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local backend: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	stores := store.New(db)
	svc := service.NewExportService(stores.Export, stores.User, nil, "http://localhost").WithBackend(backend)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return exportEnv{db: db, svc: svc, backend: backend}
}

func (e exportEnv) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	testutil.Exec(t, e.db, query, args...)
}

func (e exportEnv) scalar(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(query, args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return id
}

// seedContent gives userID one of everything an export holds, and every secret it must not, each marked with tag.
func (e exportEnv) seedContent(t *testing.T, userID int64, username, tag string) {
	t.Helper()
	repo := testutil.SeedRepo(t, e.db, userID, username, tag)
	issue := e.scalar(t, `INSERT INTO issues (repo_id, number, author_id, title, body) VALUES ($1, 1, $2, $3, 'issue body '||$3) RETURNING id`, repo, userID, "issue-"+tag)
	pull := e.scalar(t, `INSERT INTO pull_requests (repo_id, number, author_id, title, head_branch) VALUES ($1, 2, $2, $3, 'topic') RETURNING id`, repo, userID, "pull-"+tag)
	e.exec(t, `INSERT INTO comments (repo_id, issue_id, author_id, body) VALUES ($1, $2, $3, $4)`, repo, issue, userID, "comment-"+tag)
	e.exec(t, `INSERT INTO pull_reviews (pull_id, repo_id, author_id, state, body) VALUES ($1, $2, $3, 'commented', $4)`, pull, repo, userID, "review-"+tag)
	e.exec(t, `INSERT INTO pull_line_comments (pull_id, repo_id, author_id, path, line, body) VALUES ($1, $2, $3, 'a.go', 3, $4)`, pull, repo, userID, "line-"+tag)
	e.exec(t, `INSERT INTO ssh_keys (user_id, title, public_key, fingerprint) VALUES ($1, $2, 'ssh-ed25519 AAAA'||$2, 'SHA256:'||$2)`, userID, "key-"+tag)
	e.exec(t, `INSERT INTO gists (id, owner_id, owner_name, description) VALUES ($1, $2, $3, $4)`, "g"+tag, userID, username, "gist-"+tag)
	e.exec(t, `INSERT INTO gist_files (gist_id, filename, content) VALUES ($1, 'f.txt', $2)`, "g"+tag, "gistfile-"+tag)
	e.exec(t, `INSERT INTO stars (user_id, repo_id) VALUES ($1, $2)`, userID, repo)
	e.exec(t, `INSERT INTO watches (user_id, repo_id) VALUES ($1, $2)`, userID, repo)
	e.exec(t, `UPDATE users SET password_hash = $2, totp_secret = $3, totp_enabled = true WHERE id = $1`, userID, "PWHASH-"+tag, "TOTPSECRET-"+tag)
	e.exec(t, `INSERT INTO access_tokens (user_id, name, token_hash, last_eight) VALUES ($1, 'ci', $2, 'abcd1234')`, userID, "TOKENHASH-"+tag)
}

func (e exportEnv) waitFor(t *testing.T, userID int64, status string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var got string
		err := e.db.QueryRow(`SELECT status FROM account_exports WHERE user_id = $1 ORDER BY id DESC LIMIT 1`, userID).Scan(&got)
		if err == nil && got == status {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("export of user %d never became %s", userID, status)
}

func (e exportEnv) unzip(t *testing.T, userID, exportID int64) map[string]string {
	t.Helper()
	rc, err := e.svc.Open(context.Background(), userID, exportID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, _ := io.ReadAll(r)
		_ = r.Close()
		files[f.Name] = string(b)
	}
	return files
}

func (e exportEnv) request(t *testing.T, userID int64) int64 {
	t.Helper()
	x, err := e.svc.Request(context.Background(), userID)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	return x.ID
}

func TestExportService_ZipHoldsOnlyTheListedFilesOfTheUser(t *testing.T) {
	env := newExportEnv(t)
	suffix := testutil.UniqueSuffix(t)
	alice := testutil.SeedUser(t, env.db, "a"+suffix)
	bob := testutil.SeedUser(t, env.db, "b"+suffix)
	aliceTag, bobTag := "alicemark"+suffix, "bobmark"+suffix
	env.seedContent(t, alice, "testuser_a"+suffix, aliceTag)
	env.seedContent(t, bob, "testuser_b"+suffix, bobTag)

	id := env.request(t, alice)
	env.waitFor(t, alice, "ready")
	files := env.unzip(t, alice, id)

	want := []string{"comments.json", "gists.json", "issues.json", "profile.json", "pull_line_comments.json", "pull_requests.json",
		"pull_reviews.json", "ssh_keys.json", "stars.json", "watches.json"}
	got := make([]string, 0, len(files))
	for name := range files {
		got = append(got, name)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("zip files = %v, want %v", got, want)
	}
	all := strings.Join(slices.Collect(func(yield func(string) bool) {
		for _, c := range files {
			if !yield(c) {
				return
			}
		}
	}), "\n")
	for _, marker := range []string{"issue-", "pull-", "comment-", "review-", "line-", "key-", "gist-", "gistfile-"} {
		if !strings.Contains(all, marker+aliceTag) {
			t.Errorf("the export lacks %s%s", marker, aliceTag)
		}
		if strings.Contains(all, marker+bobTag) {
			t.Errorf("the export holds another user's %s%s", marker, bobTag)
		}
	}
	if !strings.Contains(files["stars.json"], "testrepo_"+aliceTag) || !strings.Contains(files["watches.json"], "testrepo_"+aliceTag) {
		t.Errorf("stars or watches miss the user's repository: %s / %s", files["stars.json"], files["watches.json"])
	}
}

func TestExportService_ZipHoldsNoSecrets(t *testing.T) {
	env := newExportEnv(t)
	suffix := testutil.UniqueSuffix(t)
	user := testutil.SeedUser(t, env.db, suffix)
	env.seedContent(t, user, "testuser_"+suffix, "mark"+suffix)

	id := env.request(t, user)
	env.waitFor(t, user, "ready")
	for name, content := range env.unzip(t, user, id) {
		for _, secret := range []string{"PWHASH-", "TOTPSECRET-", "TOKENHASH-", "abcd1234", "password_hash", "totp"} {
			if strings.Contains(content, secret) {
				t.Errorf("%s contains %q", name, secret)
			}
		}
	}
}

func TestExportService_AnotherUserCannotOpenIt(t *testing.T) {
	env := newExportEnv(t)
	suffix := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, env.db, "o"+suffix)
	other := testutil.SeedUser(t, env.db, "x"+suffix)
	id := env.request(t, owner)
	env.waitFor(t, owner, "ready")

	if _, err := env.svc.Open(context.Background(), other, id); !errors.Is(err, service.ErrExportNotFound) {
		t.Fatalf("Open as another user = %v, want ErrExportNotFound", err)
	}
}

func TestExportService_OneInFlightAndOnePerDay(t *testing.T) {
	db := testutil.OpenTestDB(t)
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	stores := store.New(db)
	// No Run loop: the first request stays queued.
	svc := service.NewExportService(stores.Export, stores.User, nil, "http://localhost").WithBackend(backend)
	user := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	ctx := context.Background()

	first, err := svc.Request(ctx, user)
	if err != nil {
		t.Fatalf("first Request: %v", err)
	}
	if _, err := svc.Request(ctx, user); !errors.Is(err, service.ErrExportInProgress) {
		t.Fatalf("Request while queued = %v, want ErrExportInProgress", err)
	}
	testutil.Exec(t, db, `UPDATE account_exports SET status = 'ready', storage_key = 'exports/x/y.zip' WHERE id = $1`, first.ID)
	if _, err := svc.Request(ctx, user); !errors.Is(err, service.ErrExportTooSoon) {
		t.Fatalf("second Request in a day = %v, want ErrExportTooSoon", err)
	}
	testutil.Exec(t, db, `UPDATE account_exports SET status = 'failed' WHERE id = $1`, first.ID)
	if _, err := svc.Request(ctx, user); err != nil {
		t.Fatalf("Request after a failure = %v, want to be allowed", err)
	}
	testutil.Exec(t, db, `UPDATE account_exports SET requested_at = NOW() - INTERVAL '25 hours', status = 'ready' WHERE user_id = $1`, user)
	if _, err := svc.Request(ctx, user); err != nil {
		t.Fatalf("Request after a day = %v", err)
	}
}

func TestExportService_SweepDeletesExpiredObjectAndRow(t *testing.T) {
	env := newExportEnv(t)
	user := testutil.SeedUser(t, env.db, testutil.UniqueSuffix(t))
	id := env.request(t, user)
	env.waitFor(t, user, "ready")
	key := env.keyOf(t, id)
	if ok, _ := env.backend.Exists(context.Background(), key); !ok {
		t.Fatal("the export object was never stored")
	}

	env.exec(t, `UPDATE account_exports SET expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, id)
	env.svc.Wake()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && env.scalar(t, `SELECT COUNT(*) FROM account_exports WHERE id = $1`, id) != 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := env.scalar(t, `SELECT COUNT(*) FROM account_exports WHERE id = $1`, id); n != 0 {
		t.Fatal("the expired row survived the sweep")
	}
	if ok, _ := env.backend.Exists(context.Background(), key); ok {
		t.Error("the expired object survived the sweep")
	}
}

func TestExportService_OpenRefusesAnExpiredExport(t *testing.T) {
	env := newExportEnv(t)
	user := testutil.SeedUser(t, env.db, testutil.UniqueSuffix(t))
	id := env.request(t, user)
	env.waitFor(t, user, "ready")
	// Past expiry but before the next sweep tick; the sweep may win the race, which is also a refusal.
	env.exec(t, `UPDATE account_exports SET expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, id)
	if _, err := env.svc.Open(context.Background(), user, id); !errors.Is(err, service.ErrExportNotFound) {
		t.Fatalf("Open after expiry = %v, want ErrExportNotFound", err)
	}
}

func TestUserService_DeleteUser_RemovesExports(t *testing.T) {
	env := newExportEnv(t)
	repoEnv := newRepoDirsEnv(t)
	userID, _ := repoEnv.seedUser(t)
	id := env.request(t, userID)
	env.waitFor(t, userID, "ready")
	key := env.keyOf(t, id)

	users := service.NewUserService(store.NewUserStore(repoEnv.db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}).
		WithRepoService(repoEnv.repos).WithExports(env.svc)
	if err := users.DeleteUser(context.Background(), userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if ok, _ := env.backend.Exists(context.Background(), key); ok {
		t.Error("the export object outlived the account")
	}
	if n := env.scalar(t, `SELECT COUNT(*) FROM account_exports WHERE user_id = $1`, userID); n != 0 {
		t.Error("the export row outlived the account")
	}
}

func (e exportEnv) keyOf(t *testing.T, id int64) string {
	t.Helper()
	var key string
	if err := e.db.QueryRow(`SELECT storage_key FROM account_exports WHERE id = $1`, id).Scan(&key); err != nil {
		t.Fatalf("storage key: %v", err)
	}
	return key
}
