package router_test

// The /v2 challenge, the token endpoint and the middleware in front of every registry operation, driven the way
// docker drives them. Requires TEST_DATABASE_DSN and skips otherwise.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const testDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type registryAccess struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

// registryEnv has owner's public repo "pub" and private repo "priv", each with one linked image, and reader, a
// reader collaborator on priv. stranger has no access to either.
type registryEnv struct {
	h                                http.Handler
	svc                              *service.Services
	db                               *sql.DB
	clock                            *fakeClock
	owner, reader, stranger          string
	ownerID, readerID, strangerID    int64
	pubImage, privImage, newImage    string
	ownerPAT, readerPAT, strangerPAT string // packages:write
	ownerReadPAT                     string // packages:read
	ownerRepoWritePAT                string // repo:write only
	objects                          string // root of the local storage backend
	pubRepoID                        int64
}

func newRegistryEnv(t *testing.T, mutate func(*config.Config)) *registryEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: "cz_token"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
		CI:     config.DefaultCI(),
		Registry: config.RegistryConfig{
			Enabled: true, MaxBlobBytes: 5 << 30, MaxSessionsPerUser: 10, MaxInflightBytesPerUser: 10 << 30,
			MaxPendingPerUser: 20, UploadTTL: 24 * time.Hour, GCGrace: 24 * time.Hour,
		},
	}
	if mutate != nil {
		mutate(cfg)
	}
	objects := t.TempDir()
	backend, err := storage.NewLocal(objects)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	svc := service.New(store.New(db), cfg).WithStorage(backend)
	svc.Registry.WithSpoolDir(t.TempDir())
	clock := &fakeClock{now: time.Now()}
	svc.Registry.WithClock(clock.Now)
	h, err := router.New(svc, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	e := &registryEnv{h: h, svc: svc, db: db, clock: clock, objects: objects}
	suffix := testutil.UniqueSuffix(t)
	e.owner, e.reader, e.stranger = "own_"+suffix, "rdr_"+suffix, "str_"+suffix
	e.ownerID = testutil.SeedUser(t, db, "own_"+suffix)
	e.readerID = testutil.SeedUser(t, db, "rdr_"+suffix)
	e.strangerID = testutil.SeedUser(t, db, "str_"+suffix)
	e.owner, e.reader, e.stranger = userName(t, db, e.ownerID), userName(t, db, e.readerID), userName(t, db, e.strangerID)

	ctx := context.Background()
	pub, err := svc.Repo.Create(ctx, e.ownerID, e.owner, "pub", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("create pub: %v", err)
	}
	priv, err := svc.Repo.Create(ctx, e.ownerID, e.owner, "priv", "", true, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("create priv: %v", err)
	}
	if err := svc.Repo.AddCollaborator(ctx, priv, e.ownerID, e.reader, string(model.RoleReader)); err != nil {
		t.Fatalf("add reader: %v", err)
	}
	for name, repoID := range map[string]int64{"pubimg": pub.ID, "privimg": priv.ID} {
		testutil.Exec(t, db, `INSERT INTO packages (owner_user_id, type, name, repo_id, creator_id) VALUES ($1, 'container', $2, $3, $1)`,
			e.ownerID, name, repoID)
	}
	e.pubRepoID = pub.ID
	e.pubImage, e.privImage, e.newImage = e.owner+"/pubimg", e.owner+"/privimg", e.owner+"/newimg"

	e.ownerPAT = e.pat(t, e.ownerID, model.ScopePackagesWrite)
	e.readerPAT = e.pat(t, e.readerID, model.ScopePackagesWrite)
	e.strangerPAT = e.pat(t, e.strangerID, model.ScopePackagesWrite)
	e.ownerReadPAT = e.pat(t, e.ownerID, model.ScopePackagesRead)
	e.ownerRepoWritePAT = e.pat(t, e.ownerID, model.ScopeRepoWrite)
	return e
}

func userName(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var name string
	if err := db.QueryRowContext(context.Background(), `SELECT username FROM users WHERE id = $1`, id).Scan(&name); err != nil {
		t.Fatalf("read username: %v", err)
	}
	return name
}

func (e *registryEnv) pat(t *testing.T, userID int64, scopes ...string) string {
	t.Helper()
	raw, _, err := e.svc.AccessToken.Create(context.Background(), userID, service.NewToken{Name: "reg", Scopes: scopes})
	if err != nil {
		t.Fatalf("create token %v: %v", scopes, err)
	}
	return raw
}

func (e *registryEnv) do(method, target, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return serve(e.h, req)
}

// token asks /v2/token as docker does: Basic with the PAT as the password, or no credentials when pat is "".
func (e *registryEnv) token(pat string, scopes ...string) *httptest.ResponseRecorder {
	target := "/v2/token?service=localhost"
	for _, s := range scopes {
		target += "&scope=" + s
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if pat != "" {
		req.SetBasicAuth("docker", pat)
	}
	return serve(e.h, req)
}

// bearer returns a registry token for pat and scopes, failing the test when the endpoint refuses.
func (e *registryEnv) bearer(t *testing.T, pat string, scopes ...string) string {
	t.Helper()
	rr := e.token(pat, scopes...)
	if rr.Code != http.StatusOK {
		t.Fatalf("token for %v: got %d %s", scopes, rr.Code, rr.Body)
	}
	return "Bearer " + tokenFrom(t, rr)
}

func tokenFrom(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		IssuedAt    string `json:"issued_at"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode token response %s: %v", rr.Body, err)
	}
	if body.Token == "" || body.Token != body.AccessToken {
		t.Fatalf("token %q and access_token %q must both be set and equal", body.Token, body.AccessToken)
	}
	if body.ExpiresIn != 300 {
		t.Errorf("expires_in = %d, want 300", body.ExpiresIn)
	}
	if _, err := time.Parse(time.RFC3339, body.IssuedAt); err != nil {
		t.Errorf("issued_at %q is not RFC 3339: %v", body.IssuedAt, err)
	}
	return body.Token
}

// grantedAccess reads the access list out of a registry token without verifying it.
func grantedAccess(t *testing.T, token string) []registryAccess {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token %q is not a JWT", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var claims struct {
		Access []registryAccess `json:"access"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("decode claims %s: %v", payload, err)
	}
	return claims.Access
}

// actionsFor returns the actions the token grants on name, nil when it grants none.
func actionsFor(t *testing.T, rr *httptest.ResponseRecorder, name string) []string {
	t.Helper()
	for _, a := range grantedAccess(t, tokenFrom(t, rr)) {
		if a.Name == name {
			return a.Actions
		}
	}
	return nil
}

func ociErrorCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Errors []struct{ Code, Message string } `json:"errors"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 {
		t.Fatalf("body %q is not one OCI error: %v", rr.Body, err)
	}
	return body.Errors[0].Code
}

func TestRegistryBase_ChallengesWithoutAToken(t *testing.T) {
	e := newRegistryEnv(t, nil)

	rr := e.do(http.MethodGet, "/v2/", "")

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
	if got, want := rr.Header().Get("WWW-Authenticate"), `Bearer realm="http://localhost/v2/token",service="localhost"`; got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
	if got := rr.Header().Get("Docker-Distribution-API-Version"); got != "registry/2.0" {
		t.Errorf("Docker-Distribution-API-Version = %q, want registry/2.0", got)
	}
	if code := ociErrorCode(t, rr); code != "UNAUTHORIZED" {
		t.Errorf("error code = %q, want UNAUTHORIZED", code)
	}
}

func TestRegistryBase_AcceptsATokenAndSendsTheVersionHeader(t *testing.T) {
	e := newRegistryEnv(t, nil)

	rr := e.do(http.MethodGet, "/v2/", e.bearer(t, e.ownerPAT))

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200", rr.Code, rr.Body)
	}
	if got := rr.Header().Get("Docker-Distribution-API-Version"); got != "registry/2.0" {
		t.Errorf("Docker-Distribution-API-Version = %q, want registry/2.0", got)
	}
}

func TestRegistryBase_AnonymousTokenWithNoScopeIsAcceptedToo(t *testing.T) {
	e := newRegistryEnv(t, nil)

	rr := e.do(http.MethodGet, "/v2/", e.bearer(t, ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
}

func TestRegistry_DisabledAnswers404ForTheWholeTree(t *testing.T) {
	e := newRegistryEnv(t, func(c *config.Config) { c.Registry.Enabled = false })

	for _, target := range []string{"/v2/", "/v2", "/v2/token", "/v2/" + "x/y/tags/list", "/v2/_catalog"} {
		if rr := e.do(http.MethodGet, target, ""); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, rr.Code)
		}
	}
}

func TestRegistryToken_Credentials(t *testing.T) {
	e := newRegistryEnv(t, nil)
	accountPassword := "an-account-password"
	testutil.SetPassword(t, e.db, e.ownerID, accountPassword)
	_, signingKey := testutil.NewSigningKey(t)
	keyBound, _, err := e.svc.AccessToken.Create(context.Background(), e.ownerID,
		service.NewToken{Name: "bound", Scopes: []string{model.ScopePackagesWrite}, SigningKey: signingKey})
	if err != nil {
		t.Fatalf("create key-bound token: %v", err)
	}
	suspended := e.pat(t, e.strangerID, model.ScopePackagesRead)
	testutil.Exec(t, e.db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, e.strangerID)

	tests := []struct {
		name, password string
		want           int
		challenge      string
	}{
		{"a packages:write PAT", e.ownerPAT, http.StatusOK, ""},
		{"a packages:read PAT", e.ownerReadPAT, http.StatusOK, ""},
		{"a PAT that doesn't exist", "czp_" + strings.Repeat("0", 64), http.StatusUnauthorized, `Basic realm="registry"`},
		{"the account password", accountPassword, http.StatusUnauthorized, `Basic realm="registry"`},
		{"a PAT with only repo:write", e.ownerRepoWritePAT, http.StatusForbidden, ""},
		{"a key-bound PAT", keyBound, http.StatusForbidden, ""},
		{"a suspended user's PAT", suspended, http.StatusForbidden, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := e.token(tc.password, "repository:"+e.pubImage+":pull")
			if rr.Code != tc.want {
				t.Fatalf("got %d %s, want %d", rr.Code, rr.Body, tc.want)
			}
			if got := rr.Header().Get("WWW-Authenticate"); got != tc.challenge {
				t.Errorf("WWW-Authenticate = %q, want %q", got, tc.challenge)
			}
			if tc.want == http.StatusForbidden && !strings.Contains(rr.Body.String(), "packages") && !strings.Contains(rr.Body.String(), "suspended") && !strings.Contains(rr.Body.String(), "key") {
				t.Errorf("403 body %q names no reason", rr.Body)
			}
		})
	}
}

func TestRegistryToken_AccessIsTheIntersectionOfTheRequestAndWhatTheUserMay(t *testing.T) {
	e := newRegistryEnv(t, nil)
	both := []string{"pull", "push"}

	tests := []struct {
		name  string
		pat   string
		image string
		ask   string
		want  []string
	}{
		{"a writer gets pull and push", e.ownerPAT, e.privImage, "pull,push", both},
		{"a reader asking for both gets pull", e.readerPAT, e.privImage, "pull,push", []string{"pull"}},
		{"a stranger on a private image gets nothing", e.strangerPAT, e.privImage, "pull,push", nil},
		{"a stranger on a public image gets pull", e.strangerPAT, e.pubImage, "pull,push", []string{"pull"}},
		{"a writer gets only what it asked for", e.ownerPAT, e.privImage, "pull", []string{"pull"}},
		{"packages:write admits pull on its own", e.ownerPAT, e.pubImage, "pull", []string{"pull"}},
		{"packages:read cannot push", e.ownerReadPAT, e.privImage, "pull,push", []string{"pull"}},
		{"a new name grants push to a writer of any of the owner's repositories", e.ownerPAT, e.newImage, "pull,push", both},
		{"a new name lets a writer delete the blobs it uploaded", e.ownerPAT, e.newImage, "pull,push,delete", []string{"delete", "pull", "push"}},
		{"a new name grants nothing to a stranger", e.strangerPAT, e.newImage, "pull,push", nil},
		{"a name that is not a valid path grants nothing", e.ownerPAT, e.owner + "/Bad_Name", "pull,push", nil},
		{"a name with no image part grants nothing", e.ownerPAT, e.owner, "pull,push", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := e.token(tc.pat, "repository:"+tc.image+":"+tc.ask)
			if rr.Code != http.StatusOK {
				t.Fatalf("got %d %s, want 200 even when nothing is granted", rr.Code, rr.Body)
			}
			got := actionsFor(t, rr, tc.image)
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("granted %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRegistryToken_RepeatedScopesAreEachIntersected(t *testing.T) {
	e := newRegistryEnv(t, nil)

	rr := e.token(e.readerPAT, "repository:"+e.privImage+":pull,push", "repository:"+e.pubImage+":pull")

	for image, want := range map[string][]string{e.privImage: {"pull"}, e.pubImage: {"pull"}} {
		if got := actionsFor(t, rr, image); !slices.Equal(got, want) {
			t.Errorf("%s: granted %v, want %v", image, got, want)
		}
	}
}

func TestRegistryToken_AnonymousGetsPullOnPublicImagesOnly(t *testing.T) {
	e := newRegistryEnv(t, nil)

	if rr := e.token("", "repository:"+e.pubImage+":pull"); rr.Code != http.StatusOK || !slices.Equal(actionsFor(t, rr, e.pubImage), []string{"pull"}) {
		t.Errorf("public pull: got %d, actions %v, want 200 and [pull]", rr.Code, grantedAccess(t, tokenFromOrEmpty(rr)))
	}
	for name, scope := range map[string]string{
		"a private image":                   "repository:" + e.privImage + ":pull",
		"an image that does not exist":      "repository:" + e.owner + "/nothere:pull",
		"a push to a public image":          "repository:" + e.pubImage + ":push",
		"a pull and a push to a public one": "repository:" + e.pubImage + ":pull,push",
	} {
		rr := e.token("", scope)
		if rr.Code != http.StatusUnauthorized || rr.Header().Get("WWW-Authenticate") != `Basic realm="registry"` {
			t.Errorf("%s: got %d with %q, want 401 and a Basic challenge", name, rr.Code, rr.Header().Get("WWW-Authenticate"))
		}
	}
}

func tokenFromOrEmpty(rr *httptest.ResponseRecorder) string {
	var body struct{ Token string }
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	return body.Token
}

func TestRegistryToken_ASoftDeletedRepositoryGrantsNothing(t *testing.T) {
	e := newRegistryEnv(t, nil)
	testutil.Exec(t, e.db, `UPDATE repositories SET deleted_at = NOW() WHERE owner_id = $1 AND name = 'pub'`, e.ownerID)

	rr := e.token(e.ownerPAT, "repository:"+e.pubImage+":pull,push")

	if got := actionsFor(t, rr, e.pubImage); len(got) != 0 {
		t.Errorf("granted %v on an image of a deleted repository, want nothing", got)
	}
}

func TestRegistryAuth_TheTokenExpiresAfterFiveMinutes(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.bearer(t, e.ownerPAT, "repository:"+e.privImage+":pull")
	target := "/v2/" + e.privImage + "/tags/list"

	if rr := e.do(http.MethodGet, target, auth); rr.Code == http.StatusUnauthorized {
		t.Fatalf("a fresh token was refused: %s", rr.Body)
	}
	e.clock.now = e.clock.now.Add(5*time.Minute + time.Second)

	rr := e.do(http.MethodGet, target, auth)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 for an expired token", rr.Code)
	}
	want := `Bearer realm="http://localhost/v2/token",service="localhost",scope="repository:` + e.privImage + `:pull",error="invalid_token"`
	if got := rr.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestRegistryAuth_ATamperedTokenIsRefused(t *testing.T) {
	e := newRegistryEnv(t, nil)
	token := strings.TrimPrefix(e.bearer(t, e.strangerPAT, "repository:"+e.privImage+":pull"), "Bearer ")
	parts := strings.Split(token, ".")
	forged, _ := json.Marshal(map[string]any{"access": []registryAccess{{Type: "repository", Name: e.privImage, Actions: []string{"pull", "push"}}}})
	parts[1] = base64.RawURLEncoding.EncodeToString(forged)

	rr := e.do(http.MethodGet, "/v2/"+e.privImage+"/tags/list", "Bearer "+strings.Join(parts, "."))

	if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Header().Get("WWW-Authenticate"), `scope="repository:`+e.privImage+`:pull"`) {
		t.Errorf("got %d with %q, want 401 naming the scope", rr.Code, rr.Header().Get("WWW-Authenticate"))
	}
}

func TestRegistryAuth_AnActionTheTokenLacksIsAnInsufficientScopeChallenge(t *testing.T) {
	e := newRegistryEnv(t, nil)
	reader := e.bearer(t, e.readerPAT, "repository:"+e.privImage+":pull")

	rr := e.do(http.MethodPost, "/v2/"+e.privImage+"/blobs/uploads/", reader)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 (docker shows a reader pushing as unauthorized)", rr.Code)
	}
	want := `Bearer realm="http://localhost/v2/token",service="localhost",scope="repository:` + e.privImage + `:push",error="insufficient_scope"`
	if got := rr.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestRegistryAuth_ATokenForAnotherNameDoesNotAdmit(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.bearer(t, e.ownerPAT, "repository:"+e.pubImage+":pull")

	rr := e.do(http.MethodGet, "/v2/"+e.privImage+"/tags/list", auth)

	if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Header().Get("WWW-Authenticate"), "insufficient_scope") {
		t.Errorf("got %d with %q, want an insufficient_scope challenge", rr.Code, rr.Header().Get("WWW-Authenticate"))
	}
}

func TestRegistryAuth_NoTokenOnANamedRouteChallengesWithItsScope(t *testing.T) {
	e := newRegistryEnv(t, nil)
	tests := []struct{ method, path, name, action string }{
		{http.MethodGet, "/v2/" + e.pubImage + "/tags/list", e.pubImage, "pull"},
		{http.MethodHead, "/v2/" + e.pubImage + "/manifests/latest", e.pubImage, "pull"},
		{http.MethodPut, "/v2/" + e.pubImage + "/manifests/latest", e.pubImage, "push"},
		{http.MethodPatch, "/v2/" + e.pubImage + "/blobs/uploads/abc", e.pubImage, "push"},
		{http.MethodPost, "/v2/" + e.pubImage + "/blobs/uploads/", e.pubImage, "push"},
		{http.MethodDelete, "/v2/" + e.pubImage + "/manifests/" + testDigest, e.pubImage, "delete"},
		// A name containing a route word as a segment: the route is read from the right.
		{http.MethodGet, "/v2/" + e.owner + "/blobs/img/blobs/" + testDigest, e.owner + "/blobs/img", "pull"},
		{http.MethodGet, "/v2/" + e.owner + "/manifests/tags/list/tags/list", e.owner + "/manifests/tags/list", "pull"},
		{http.MethodGet, "/v2/" + e.owner + "/referrers/x/referrers/" + testDigest, e.owner + "/referrers/x", "pull"},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := e.do(tc.method, tc.path, "")
			want := `scope="repository:` + tc.name + `:` + tc.action + `"`
			if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Header().Get("WWW-Authenticate"), want) {
				t.Errorf("got %d with %q, want 401 containing %s", rr.Code, rr.Header().Get("WWW-Authenticate"), want)
			}
		})
	}
}

func TestRegistryAuth_APostIsNotStoppedByCSRFOrFormLimits(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.bearer(t, e.ownerPAT, "repository:"+e.privImage+":pull,push")

	req := httptest.NewRequest(http.MethodPost, "/v2/"+e.privImage+"/blobs/uploads/", strings.NewReader("a=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", auth)
	rr := serve(e.h, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("got %d %s, want 202, not a CSRF refusal", rr.Code, rr.Body)
	}
}

func TestRegistryAuth_CatalogIs404(t *testing.T) {
	e := newRegistryEnv(t, nil)

	for _, auth := range []string{"", e.bearer(t, e.ownerPAT)} {
		if rr := e.do(http.MethodGet, "/v2/_catalog", auth); rr.Code != http.StatusNotFound {
			t.Errorf("GET /v2/_catalog with %q = %d, want 404", auth, rr.Code)
		}
	}
}

func TestRegistryAuth_AnInvalidNameIsNAME_INVALID(t *testing.T) {
	e := newRegistryEnv(t, nil)

	rr := e.do(http.MethodGet, "/v2/"+e.owner+"/Bad_Name/tags/list", "")

	if rr.Code != http.StatusBadRequest || ociErrorCode(t, rr) != "NAME_INVALID" {
		t.Errorf("got %d %s, want 400 NAME_INVALID", rr.Code, rr.Body)
	}
}

func TestRegistryAuth_ARegistryTokenIsNotASession(t *testing.T) {
	e := newRegistryEnv(t, nil)
	token := strings.TrimPrefix(e.bearer(t, e.ownerPAT, "repository:"+e.privImage+":pull"), "Bearer ")

	asBearer := httptest.NewRequest(http.MethodGet, "/api/user", nil)
	asBearer.Header.Set("Authorization", "Bearer "+token)
	if rr := serve(e.h, asBearer); rr.Code != http.StatusUnauthorized {
		t.Errorf("as a Bearer on /api/user: got %d, want 401", rr.Code)
	}

	asCookie := httptest.NewRequest(http.MethodGet, "/api/user", nil)
	asCookie.AddCookie(&http.Cookie{Name: "cz_token", Value: token})
	if rr := serve(e.h, asCookie); rr.Code != http.StatusUnauthorized {
		t.Errorf("as the session cookie on /api/user: got %d, want 401", rr.Code)
	}

	page := httptest.NewRequest(http.MethodGet, "/settings", nil)
	page.AddCookie(&http.Cookie{Name: "cz_token", Value: token})
	if rr := serve(e.h, page); rr.Code == http.StatusOK {
		t.Errorf("as the session cookie on /settings: got 200, want a redirect to sign in")
	}
}

func TestRegistryAuth_ASessionIsNotARegistryToken(t *testing.T) {
	e := newRegistryEnv(t, nil)

	rr := e.do(http.MethodGet, "/v2/", "Bearer "+makeJWT(t, e.ownerID, e.owner))

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401: a session JWT must not open /v2", rr.Code)
	}
}

func TestRegistryRateLimit_CountsPerUserInItsOwnBucket(t *testing.T) {
	e := newRegistryEnv(t, func(c *config.Config) {
		c.RateLimit = config.RateLimitConfig{Enabled: true, Window: time.Hour,
			Core:     config.RateBudget{Authenticated: 1000, Anonymous: 1000},
			Registry: config.RateBudget{Authenticated: 3, Anonymous: 2},
		}
	})
	owner := e.bearer(t, e.ownerPAT, "repository:"+e.privImage+":pull")
	reader := e.bearer(t, e.readerPAT, "repository:"+e.privImage+":pull")

	var got []int
	for range 4 {
		got = append(got, e.do(http.MethodGet, "/v2/", owner).Code)
	}
	if want := []int{200, 200, 200, http.StatusTooManyRequests}; !slices.Equal(got, want) {
		t.Errorf("owner's /v2/ statuses = %v, want %v", got, want)
	}
	if rr := e.do(http.MethodGet, "/v2/", reader); rr.Code != http.StatusOK || rr.Header().Get("X-RateLimit-Resource") != "registry" {
		t.Errorf("another user shares the owner's bucket: got %d, resource %q", rr.Code, rr.Header().Get("X-RateLimit-Resource"))
	}
	// Token requests are core: the registry bucket the owner just emptied doesn't stop them.
	if rr := e.token(e.ownerPAT, "repository:"+e.privImage+":pull"); rr.Code != http.StatusOK || rr.Header().Get("X-RateLimit-Resource") != "core" {
		t.Errorf("token request: got %d, resource %q, want 200 in core", rr.Code, rr.Header().Get("X-RateLimit-Resource"))
	}
}
