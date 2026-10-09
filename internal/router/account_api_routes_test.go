package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSSHKeys_RequireSignIn(t *testing.T) {
	e := newTransferEnv(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/user/keys/"},
		{http.MethodPost, "/api/user/keys/"},
		{http.MethodDelete, "/api/user/keys/1"},
	} {
		if rr := serve(e.h, browserRequest(tc.method, tc.path, "", nil)); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s %s signed out: got %d, want 401", tc.method, tc.path, rr.Code)
		}
	}
}

func TestSSHKeys_AddRefusals(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)
	good := sshPublicKey(t)
	stored := func() int { return countRows(t, e.db, `SELECT COUNT(*) FROM ssh_keys WHERE user_id = $1`, me.id) }

	cases := map[string]*http.Request{
		"malformed json":      importJSONRequest(http.MethodPost, "/api/user/keys/", me.session, `{`),
		"json without title":  importJSONRequest(http.MethodPost, "/api/user/keys/", me.session, `{"public_key":"`+good+`","password":"password1"}`),
		"json without key":    importJSONRequest(http.MethodPost, "/api/user/keys/", me.session, `{"title":"t","password":"password1"}`),
		"form without title":  browserRequest(http.MethodPost, "/api/user/keys/", me.session, url.Values{"public_key": {good}, "password": {"password1"}}),
		"unparseable key":     browserRequest(http.MethodPost, "/api/user/keys/", me.session, url.Values{"title": {"t"}, "public_key": {"ssh-ed25519 not-base64"}, "password": {"password1"}}),
		"private key pasted":  browserRequest(http.MethodPost, "/api/user/keys/", me.session, url.Values{"title": {"t"}, "public_key": {"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----"}, "password": {"password1"}}),
		"json without secret": importJSONRequest(http.MethodPost, "/api/user/keys/", me.session, `{"title":"t","public_key":"`+good+`"}`),
	}
	want := map[string]int{"json without secret": http.StatusForbidden}
	for name, req := range cases {
		code := http.StatusBadRequest
		if c, ok := want[name]; ok {
			code = c
		}
		if rr := serve(e.h, req); rr.Code != code {
			t.Errorf("%s: got %d %s, want %d", name, rr.Code, rr.Body, code)
		}
	}
	if stored() != 0 {
		t.Fatalf("refused requests stored %d keys", stored())
	}
}

func TestSSHKeys_Lifecycle(t *testing.T) {
	e := newTransferEnv(t)
	me, other := e.account(t), e.account(t)
	suffix := testutil.UniqueSuffix(t)
	title := "laptop " + suffix

	empty := serve(e.h, importJSONRequest(http.MethodGet, "/api/user/keys/", me.session, ""))
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != "[]" {
		t.Fatalf("empty list: got %d %q, want 200 and an empty array", empty.Code, empty.Body)
	}

	pub := sshPublicKey(t)
	body, _ := json.Marshal(map[string]string{"title": title, "public_key": pub, "password": "password1"})
	rr := serve(e.h, importJSONRequest(http.MethodPost, "/api/user/keys/", me.session, string(body)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("add: got %d %s, want 201", rr.Code, rr.Body)
	}
	var key struct {
		ID          int64
		Fingerprint string
		Title       string
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &key); err != nil || key.ID == 0 || key.Fingerprint == "" || key.Title != title {
		t.Fatalf("add body = %s (%v), want the stored key with a fingerprint", rr.Body, err)
	}
	var fingerprint string
	if err := e.db.QueryRow(`SELECT fingerprint FROM ssh_keys WHERE id = $1 AND user_id = $2`, key.ID, me.id).Scan(&fingerprint); err != nil || fingerprint != key.Fingerprint {
		t.Errorf("stored fingerprint = %q (%v), want %q", fingerprint, err, key.Fingerprint)
	}

	list := serve(e.h, importJSONRequest(http.MethodGet, "/api/user/keys/", me.session, ""))
	if !strings.Contains(list.Body.String(), title) {
		t.Errorf("list lacks my key: %s", list.Body)
	}
	if theirs := serve(e.h, importJSONRequest(http.MethodGet, "/api/user/keys/", other.session, "")); strings.Contains(theirs.Body.String(), title) {
		t.Error("another user's list shows my key")
	}

	path := "/api/user/keys/" + strconv.FormatInt(key.ID, 10)
	if rr := serve(e.h, browserRequest(http.MethodDelete, "/api/user/keys/abc", me.session, nil)); rr.Code != http.StatusBadRequest {
		t.Errorf("non-numeric id: got %d, want 400", rr.Code)
	}
	serve(e.h, browserRequest(http.MethodDelete, path, other.session, nil))
	if countRows(t, e.db, `SELECT COUNT(*) FROM ssh_keys WHERE id = $1`, key.ID) != 1 {
		t.Fatal("another user deleted my key")
	}
	if rr := serve(e.h, browserRequest(http.MethodDelete, path, me.session, nil)); rr.Code != http.StatusNoContent {
		t.Errorf("delete: got %d, want 204", rr.Code)
	}
	if countRows(t, e.db, `SELECT COUNT(*) FROM ssh_keys WHERE id = $1`, key.ID) != 0 {
		t.Error("delete left the key behind")
	}
}

func TestSSHKeys_HTMXReturnsList(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)
	title := "htmx key " + testutil.UniqueSuffix(t)

	form := url.Values{"title": {title}, "public_key": {sshPublicKey(t)}, "password": {"password1"}}
	rr := serve(e.h, htmxRequest(browserRequest(http.MethodPost, "/api/user/keys/", me.session, form)))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), title) {
		t.Fatalf("add: got %d %.300s, want the list fragment with the key", rr.Code, rr.Body)
	}
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM ssh_keys WHERE user_id = $1`, me.id).Scan(&id); err != nil {
		t.Fatal(err)
	}
	rr = serve(e.h, htmxRequest(browserRequest(http.MethodDelete, "/api/user/keys/"+strconv.FormatInt(id, 10), me.session, nil)))
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), title) {
		t.Errorf("delete: got %d, want the list fragment without the key", rr.Code)
	}
	if countRows(t, e.db, `SELECT COUNT(*) FROM ssh_keys WHERE user_id = $1`, me.id) != 0 {
		t.Error("delete left the key behind")
	}
}

func createToken(e transferEnv, a transferAccount, form url.Values, htmx bool) *httptest.ResponseRecorder {
	req := browserRequest(http.MethodPost, "/api/user/tokens/", a.session, withPassword(form, "password1"))
	if htmx {
		htmxRequest(req)
	}
	return serve(e.h, req)
}

func TestTokens_RequireSignIn(t *testing.T) {
	e := newTransferEnv(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/user/tokens/"},
		{http.MethodDelete, "/api/user/tokens/1"},
	} {
		if rr := serve(e.h, browserRequest(tc.method, tc.path, "", nil)); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s %s signed out: got %d, want 401", tc.method, tc.path, rr.Code)
		}
	}
}

func TestTokens_CreateRefusals(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)
	stored := func() int { return countRows(t, e.db, `SELECT COUNT(*) FROM access_tokens WHERE user_id = $1`, me.id) }

	for name, form := range map[string]url.Values{
		"no name":       {"scopes": {"repo:read"}},
		"no scopes":     {"name": {"ci"}},
		"unknown scope": {"name": {"ci"}, "scopes": {"everything"}},
	} {
		if rr := createToken(e, me, form, false); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d %s, want 400", name, rr.Code, rr.Body)
		}
	}
	rr := createToken(e, me, url.Values{"scopes": {"repo:read"}}, true)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "name is required") {
		t.Errorf("htmx without a name: got %d %.200s, want the inline form error", rr.Code, rr.Body)
	}
	rr = createToken(e, me, url.Values{"name": {"ci"}}, true)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "at least one scope") {
		t.Errorf("htmx without scopes: got %d %.200s, want the inline form error", rr.Code, rr.Body)
	}

	for name, form := range map[string]url.Values{
		"admin without a signing key": {"name": {"ci"}, "scopes": {"repo:admin"}},
		"admin without an expiry":     {"name": {"ci"}, "scopes": {"repo:admin"}, "signing_key": {"ssh-ed25519 AAAA"}},
	} {
		rr := createToken(e, me, form, false)
		if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "profile_error=token_") {
			t.Errorf("%s: got %d to %q, want a token_* refusal redirect", name, rr.Code, rr.Header().Get("Location"))
		}
	}
	rr = createToken(e, me, url.Values{"name": {"ci"}, "scopes": {"repo:admin"}}, true)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "must expire within 90 days") {
		t.Errorf("htmx admin without a key: got %d %.200s, want the inline form error", rr.Code, rr.Body)
	}
	if stored() != 0 {
		t.Fatalf("refused requests stored %d tokens", stored())
	}
}

func TestTokens_CreateShowsSecretOnceAndAuthenticates(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)
	name := "ci " + testutil.UniqueSuffix(t)
	expires := time.Now().Add(48 * time.Hour).Format("2006-01-02")

	rr := createToken(e, me, url.Values{"name": {name}, "scopes": {"repo:read"}, "expires_at": {expires}}, false)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/settings#tokens" {
		t.Fatalf("create: got %d to %q, want a redirect to the tokens section", rr.Code, rr.Header().Get("Location"))
	}
	flash := responseCookie(t, rr, "cz_new_token")
	if !flash.HttpOnly || flash.Path != "/settings" {
		t.Errorf("flash cookie = %+v, want HttpOnly and scoped to /settings", flash)
	}
	if strings.Contains(rr.Header().Get("Location"), flash.Value) {
		t.Error("the secret is in the redirect URL")
	}
	var hash string
	var storedExpiry *time.Time
	if err := e.db.QueryRow(`SELECT token_hash, expires_at FROM access_tokens WHERE user_id = $1 AND name = $2`, me.id, name).Scan(&hash, &storedExpiry); err != nil {
		t.Fatalf("token not stored: %v", err)
	}
	if hash == flash.Value || strings.Contains(hash, flash.Value) {
		t.Error("the raw token is stored instead of its hash")
	}
	if storedExpiry == nil || storedExpiry.Format("2006-01-02") != expires {
		t.Errorf("expires_at = %v, want %s", storedExpiry, expires)
	}

	page := serve(e.h, browserRequest(http.MethodGet, "/settings", me.session, nil), flash)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), flash.Value) {
		t.Fatalf("settings with the flash: got %d, want the new token shown", page.Code)
	}
	expired := false
	for _, c := range page.Result().Cookies() {
		expired = expired || (c.Name == "cz_new_token" && c.Value == "" && c.MaxAge < 0)
	}
	if !expired {
		t.Error("the flash cookie was not expired after the first render")
	}
	again := serve(e.h, browserRequest(http.MethodGet, "/settings", me.session, nil))
	if strings.Contains(again.Body.String(), flash.Value) {
		t.Error("the token is shown without the flash cookie")
	}

	api := httptest.NewRequest(http.MethodGet, "/api/user", nil)
	api.Header.Set("Authorization", "Bearer "+flash.Value)
	if rr := serve(e.h, api); rr.Code != http.StatusOK {
		t.Errorf("the new token on /api/user: got %d, want 200", rr.Code)
	}

	var id int64
	if err := e.db.QueryRow(`SELECT id FROM access_tokens WHERE user_id = $1`, me.id).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if rr := serve(e.h, browserRequest(http.MethodDelete, "/api/user/tokens/"+strconv.FormatInt(id, 10), me.session, nil)); rr.Code != http.StatusNoContent {
		t.Fatalf("revoke: got %d, want 204", rr.Code)
	}
	revoked := httptest.NewRequest(http.MethodGet, "/api/user", nil)
	revoked.Header.Set("Authorization", "Bearer "+flash.Value)
	if rr := serve(e.h, revoked); rr.Code != http.StatusUnauthorized {
		t.Errorf("a revoked token on /api/user: got %d, want 401", rr.Code)
	}
}

func TestTokens_HTMXCreateRedirectsAndDeleteReturnsList(t *testing.T) {
	e := newTransferEnv(t)
	me, other := e.account(t), e.account(t)
	name := "htmx token " + testutil.UniqueSuffix(t)

	rr := createToken(e, me, url.Values{"name": {name}, "scopes": {"repo:read", "repo:write"}}, true)
	if rr.Code != http.StatusNoContent || rr.Header().Get("HX-Redirect") != "/settings#tokens" {
		t.Fatalf("create: got %d with HX-Redirect %q, want 204 to the tokens section", rr.Code, rr.Header().Get("HX-Redirect"))
	}
	if responseCookie(t, rr, "cz_new_token") == nil {
		t.Error("the redirect response lacks the flash cookie")
	}
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM access_tokens WHERE user_id = $1 AND name = $2`, me.id, name).Scan(&id); err != nil {
		t.Fatalf("token not stored: %v", err)
	}
	path := "/api/user/tokens/" + strconv.FormatInt(id, 10)

	if rr := serve(e.h, browserRequest(http.MethodDelete, "/api/user/tokens/abc", me.session, nil)); rr.Code != http.StatusBadRequest {
		t.Errorf("non-numeric id: got %d, want 400", rr.Code)
	}
	serve(e.h, browserRequest(http.MethodDelete, path, other.session, nil))
	if countRows(t, e.db, `SELECT COUNT(*) FROM access_tokens WHERE id = $1`, id) != 1 {
		t.Fatal("another user revoked my token")
	}
	rr = serve(e.h, htmxRequest(browserRequest(http.MethodDelete, path, me.session, nil)))
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), name) {
		t.Errorf("delete: got %d, want the list fragment without the token", rr.Code)
	}
	if countRows(t, e.db, `SELECT COUNT(*) FROM access_tokens WHERE id = $1`, id) != 0 {
		t.Error("delete left the token behind")
	}
}

func TestUserAPI_PublicProfileAndCurrentUser(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)

	rr := serve(e.h, browserRequest(http.MethodGet, "/api/users/"+me.name, "", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("profile: got %d, want 200", rr.Code)
	}
	var u map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &u); err != nil || u["username"] != me.name {
		t.Fatalf("profile body = %s (%v)", rr.Body, err)
	}
	if _, leaked := u["email"]; leaked || strings.Contains(rr.Body.String(), "@") {
		t.Errorf("public profile exposes the email: %s", rr.Body)
	}
	if rr := serve(e.h, browserRequest(http.MethodGet, "/api/users/nobody_"+testutil.UniqueSuffix(t), "", nil)); rr.Code != http.StatusNotFound {
		t.Errorf("unknown user: got %d, want 404", rr.Code)
	}

	if rr := serve(e.h, browserRequest(http.MethodGet, "/api/user", "", nil)); rr.Code != http.StatusUnauthorized {
		t.Errorf("/api/user signed out: got %d, want 401", rr.Code)
	}
	renamed := "renamed_" + testutil.UniqueSuffix(t)
	testutil.Exec(t, e.db, `UPDATE users SET username = $1 WHERE id = $2`, renamed, me.id)
	rr = serve(e.h, browserRequest(http.MethodGet, "/api/user", me.session, nil))
	var cur struct {
		ID       int64
		Username string
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &cur); err != nil || cur.ID != me.id || cur.Username != renamed {
		t.Errorf("/api/user = %d %s (%v), want the fresh username %q", rr.Code, rr.Body, err, renamed)
	}

	testutil.DeleteUsers(t, e.db, me.id)
	if rr := serve(e.h, browserRequest(http.MethodGet, "/api/user", me.session, nil)); rr.Code != http.StatusUnauthorized {
		t.Errorf("/api/user for a deleted account: got %d, want 401", rr.Code)
	}
}

func TestUserAPI_PinnedRepos(t *testing.T) {
	e := newTransferEnv(t)
	me, other := e.account(t), e.account(t)
	mine := func() string { return "/api/users/" + strconv.FormatInt(me.id, 10) + "/pinned-repos/" }
	newRepo := func(a transferAccount, private bool) int64 {
		sfx := testutil.UniqueSuffix(t)
		repo, err := e.svc.Repo.Create(context.Background(), a.id, a.name, "pin_"+sfx, "", private, service.RepoInitOptions{})
		if err != nil {
			t.Fatalf("create repo: %v", err)
		}
		return repo.ID
	}
	pinned := func() string {
		var s string
		if err := e.db.QueryRow(`SELECT pinned_repo_ids::text FROM users WHERE id = $1`, me.id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	repo := newRepo(me, false)
	foreignPrivate := newRepo(other, true)

	for _, m := range []string{http.MethodPost, http.MethodDelete} {
		if rr := serve(e.h, browserRequest(m, mine()+id(repo), "", nil)); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s signed out: got %d, want 401", m, rr.Code)
		}
		if rr := serve(e.h, browserRequest(m, "/api/users/x/pinned-repos/"+id(repo), me.session, nil)); rr.Code != http.StatusBadRequest {
			t.Errorf("%s bad user id: got %d, want 400", m, rr.Code)
		}
		if rr := serve(e.h, browserRequest(m, "/api/users/"+id(other.id)+"/pinned-repos/"+id(repo), me.session, nil)); rr.Code != http.StatusForbidden {
			t.Errorf("%s another user's pins: got %d, want 403", m, rr.Code)
		}
		if rr := serve(e.h, browserRequest(m, mine()+"x", me.session, nil)); rr.Code != http.StatusBadRequest {
			t.Errorf("%s bad repo id: got %d, want 400", m, rr.Code)
		}
	}
	if rr := serve(e.h, browserRequest(http.MethodPost, mine()+"999999999", me.session, nil)); rr.Code != http.StatusNotFound {
		t.Errorf("pin a missing repo: got %d, want 404", rr.Code)
	}
	if rr := serve(e.h, browserRequest(http.MethodPost, mine()+id(foreignPrivate), me.session, nil)); rr.Code != http.StatusNotFound {
		t.Errorf("pin someone else's private repo: got %d, want 404", rr.Code)
	}
	if got := pinned(); got != "{}" {
		t.Fatalf("refused requests pinned %s", got)
	}

	if rr := serve(e.h, browserRequest(http.MethodPost, mine()+id(repo), me.session, nil)); rr.Code != http.StatusOK {
		t.Fatalf("pin: got %d %s, want 200", rr.Code, rr.Body)
	}
	if got := pinned(); got != "{"+id(repo)+"}" {
		t.Errorf("pinned = %s, want {%d}", got, repo)
	}
	if rr := serve(e.h, browserRequest(http.MethodDelete, mine()+id(repo), me.session, nil)); rr.Code != http.StatusOK {
		t.Fatalf("unpin: got %d, want 200", rr.Code)
	}
	if got := pinned(); got != "{}" {
		t.Errorf("pinned = %s after unpinning", got)
	}

	for range 6 {
		if rr := serve(e.h, browserRequest(http.MethodPost, mine()+id(newRepo(me, false)), me.session, nil)); rr.Code != http.StatusOK {
			t.Fatalf("pin within the limit: got %d %s", rr.Code, rr.Body)
		}
	}
	if rr := serve(e.h, browserRequest(http.MethodPost, mine()+id(newRepo(me, false)), me.session, nil)); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("pin past the limit: got %d, want 422", rr.Code)
	}
}
