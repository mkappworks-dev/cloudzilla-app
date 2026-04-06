package handler

// White-box tests for resolveGitUser (unexported method) and GitInfoRefs access control.
// Tests that require TEST_DATABASE_DSN are skipped when that env var is not set.

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/store"
)

// newBrokenGitHandler builds a Handler backed by a broken DB.
// All store queries will fail, making it suitable for unit tests
// that only exercise logic that runs before any DB call.
func newBrokenGitHandler() *Handler {
	db, _ := sql.Open("pgx", "postgres://localhost:1/nonexistent?connect_timeout=1")
	stores := &store.Stores{
		Repo:        store.NewRepoStore(db),
		User:        store.NewUserStore(db),
		Org:         store.NewOrgStore(db),
		AccessToken: store.NewAccessTokenStore(db),
	}
	cfg := &config.Config{}
	svc := &service.Services{
		Repo:        service.NewRepoService(stores.Repo, stores.User, stores.Org, cfg.Git),
		AccessToken: service.NewAccessTokenService(stores.AccessToken, stores.User),
	}
	return New(svc, cfg)
}

// newRealGitHandler builds a Handler backed by the test database.
// Skips t if TEST_DATABASE_DSN is not set.
func newRealGitHandler(t *testing.T) (*Handler, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set; skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	authCfg := config.AuthConfig{
		JWTSecret:  "test-git-secret-32bytes-minimum!",
		JWTExpiry:  24 * time.Hour,
		CookieName: "cz_token_test",
	}
	cfg := &config.Config{Auth: authCfg}
	stores := &store.Stores{
		Repo:        store.NewRepoStore(db),
		User:        store.NewUserStore(db),
		Org:         store.NewOrgStore(db),
		AccessToken: store.NewAccessTokenStore(db),
		SiteSetting: store.NewSiteSettingStore(db),
		AuditLog:    store.NewAuditLogStore(db),
	}
	svc := &service.Services{
		Repo:        service.NewRepoService(stores.Repo, stores.User, stores.Org, cfg.Git),
		AccessToken: service.NewAccessTokenService(stores.AccessToken, stores.User),
		User:        service.NewUserService(stores.User, authCfg),
		SiteSetting: service.NewSiteSettingService(stores.SiteSetting, stores.User),
		AuditLog:    service.NewAuditService(stores.AuditLog),
	}
	return New(svc, cfg), db
}

// gitInfoRefsRouter returns a chi router that dispatches GitInfoRefs.
func gitInfoRefsRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/{owner}/{repo}/info/refs", h.GitInfoRefs)
	return r
}

// seedGitUser inserts a test user and returns its ID and username.
func seedGitUser(t *testing.T, db *sql.DB, suffix string) (int64, string) {
	t.Helper()
	username := "gitu_" + suffix
	var id int64
	err := db.QueryRowContext(context.Background(),
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		username, username+"@test.invalid",
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedGitUser: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id, username
}

// seedRepo inserts a repository and returns its ID and name.
func seedGitRepo(t *testing.T, db *sql.DB, ownerID int64, ownerName, suffix string, private bool) (int64, string) {
	t.Helper()
	repoName := "gitr_" + suffix
	var repoID int64
	err := db.QueryRowContext(context.Background(),
		`INSERT INTO repositories (owner_id, owner_name, name, description, private)
		 VALUES ($1, $2, $3, '', $4) RETURNING id`,
		ownerID, ownerName, repoName, private,
	).Scan(&repoID)
	if err != nil {
		t.Fatalf("seedGitRepo: %v", err)
	}
	db.ExecContext(context.Background(),
		`INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'owner')`,
		ownerID, repoID,
	)
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM permissions WHERE repo_id = $1`, repoID)
		db.ExecContext(context.Background(), `DELETE FROM repositories WHERE id = $1`, repoID)
	})
	return repoID, repoName
}

// --- resolveGitUser unit tests (no DB required) ---

// TestResolveGitUser_NoAuth_ReturnsNil verifies that resolveGitUser returns nil
// when the request has neither JWT claims in context nor Basic Auth credentials.
func TestResolveGitUser_NoAuth_ReturnsNil(t *testing.T) {
	h := newBrokenGitHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if gu := h.resolveGitUser(req); gu != nil {
		t.Errorf("expected nil without any auth, got %+v", gu)
	}
}

// TestResolveGitUser_BasicAuth_NoCZPPrefix_ReturnsNil verifies that Basic Auth with
// a plain password (not prefixed "czp_") is ignored and resolveGitUser returns nil.
func TestResolveGitUser_BasicAuth_NoCZPPrefix_ReturnsNil(t *testing.T) {
	h := newBrokenGitHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("alice", "plainpassword")
	if gu := h.resolveGitUser(req); gu != nil {
		t.Errorf("expected nil for non-czp_ password, got %+v", gu)
	}
}

// TestResolveGitUser_BasicAuth_CZPPrefix_InvalidToken_ReturnsNil verifies that a
// czp_-prefixed password that fails token validation results in a nil gitUser.
// The broken DB causes AccessToken.Validate to return an error, simulating an unknown token.
func TestResolveGitUser_BasicAuth_CZPPrefix_InvalidToken_ReturnsNil(t *testing.T) {
	h := newBrokenGitHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("alice", "czp_invalidtoken0000000000000000000000000000000000000000000000000")
	if gu := h.resolveGitUser(req); gu != nil {
		t.Errorf("expected nil for invalid PAT with broken DB, got %+v", gu)
	}
}

// --- GitInfoRefs access control integration tests ---

// TestGitInfoRefs_UnknownRepo_404 verifies that requesting info/refs for a repository
// that does not exist returns HTTP 404.
func TestGitInfoRefs_UnknownRepo_404(t *testing.T) {
	h, _ := newRealGitHandler(t)
	router := gitInfoRefsRouter(h)

	req := httptest.NewRequest(http.MethodGet,
		"/nobody/nonexistentrepo/info/refs?service=git-upload-pack", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestGitInfoRefs_PrivateRepo_UploadPack_NoAuth_401 verifies that an anonymous request
// to clone (upload-pack) a private repository is rejected with 401 and a WWW-Authenticate header
// (prompts the git CLI to ask for credentials).
func TestGitInfoRefs_PrivateRepo_UploadPack_NoAuth_401(t *testing.T) {
	h, db := newRealGitHandler(t)
	suffix := fmt.Sprintf("%d_priv", os.Getpid())
	ownerID, ownerName := seedGitUser(t, db, suffix)
	_, repoName := seedGitRepo(t, db, ownerID, ownerName, suffix, true)

	router := gitInfoRefsRouter(h)
	url := fmt.Sprintf("/%s/%s/info/refs?service=git-upload-pack", ownerName, repoName)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401 for private repo without auth, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header on 401 response")
	}
}

// TestGitInfoRefs_PublicRepo_ReceivePack_NoAuth_401 verifies that even a public repository
// requires authentication for push (receive-pack) — read-public does not imply write-public.
func TestGitInfoRefs_PublicRepo_ReceivePack_NoAuth_401(t *testing.T) {
	h, db := newRealGitHandler(t)
	suffix := fmt.Sprintf("%d_pub", os.Getpid())
	ownerID, ownerName := seedGitUser(t, db, suffix)
	_, repoName := seedGitRepo(t, db, ownerID, ownerName, suffix, false)

	router := gitInfoRefsRouter(h)
	url := fmt.Sprintf("/%s/%s/info/refs?service=git-receive-pack", ownerName, repoName)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401 for receive-pack without auth, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestGitInfoRefs_InvalidService_400 verifies that an unrecognised "service" query parameter
// is rejected with HTTP 400 before any git protocol processing occurs.
// A public repo is used so the request can reach the service-param check.
func TestGitInfoRefs_InvalidService_400(t *testing.T) {
	h, db := newRealGitHandler(t)
	suffix := fmt.Sprintf("%d_svc", os.Getpid())
	ownerID, ownerName := seedGitUser(t, db, suffix)
	_, repoName := seedGitRepo(t, db, ownerID, ownerName, suffix, false)

	router := gitInfoRefsRouter(h)
	url := fmt.Sprintf("/%s/%s/info/refs?service=git-fake-pack", ownerName, repoName)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 for invalid service, got %d: %s", rr.Code, rr.Body.String())
	}
}
