package router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var registrationTokenRe = regexp.MustCompile(`czreg_[0-9a-f]{64}`)

func registerRunnerWith(t *testing.T, e r2Env, regToken, label string) string {
	t.Helper()
	body := `{"name":"host-` + label + `","labels":["` + label + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/runner/register", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+regToken)
	req.Header.Set("Content-Type", "application/json")
	rr := serve(e.h, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Token
}

func runnerClaimCode(e r2Env, token string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/runner/claim", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return serve(e.h, req).Code
}

func TestAdminRunners_SuperadminCreatesTokenRegistersAndRevokes(t *testing.T) {
	e := newR2Env(t)
	adminID := testutil.SeedSuperadmin(t, e.db, testutil.UniqueSuffix(t))
	admin := superadminJWT(t, adminID, "root_"+strconv.FormatInt(adminID, 10))
	label := "adm" + testutil.UniqueSuffix(t)

	page := e.form(t, http.MethodGet, "/admin/runners", admin, nil, false)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Instance runners") {
		t.Fatalf("admin runners page: %d", page.Code)
	}
	if rr := e.form(t, http.MethodGet, "/admin/runners", e.owner.token, nil, false); rr.Code != http.StatusForbidden {
		t.Errorf("regular user page = %d, want 403", rr.Code)
	}
	if rr := e.form(t, http.MethodPost, "/admin/runners/tokens", e.owner.token, url.Values{"labels": {"linux"}}, true); rr.Code != http.StatusForbidden {
		t.Errorf("regular user token = %d, want 403", rr.Code)
	}
	if rr := e.form(t, http.MethodPost, "/admin/runners/tokens", admin, url.Values{"labels": {"linux; rm -rf /"}}, true); rr.Code != http.StatusBadRequest {
		t.Errorf("shell metacharacters in labels = %d, want 400", rr.Code)
	}

	rr := e.form(t, http.MethodPost, "/admin/runners/tokens", admin, url.Values{"labels": {label + ", docker"}}, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("token: %d %s", rr.Code, rr.Body.String())
	}
	reg := registrationTokenRe.FindString(rr.Body.String())
	if reg == "" || !strings.Contains(rr.Body.String(), "cz-runner register --server http://localhost --token "+reg+" --labels "+label+",docker") {
		t.Fatalf("token fragment lacks the token or the command: %s", rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("token response is cacheable")
	}

	runnerToken := registerRunnerWith(t, e, reg, label)
	if code := runnerClaimCode(e, runnerToken); code != http.StatusNoContent && code != http.StatusOK {
		t.Errorf("fresh runner claim = %d", code)
	}
	page = e.form(t, http.MethodGet, "/admin/runners", admin, nil, false)
	if !strings.Contains(page.Body.String(), "host-"+label) || !strings.Contains(page.Body.String(), "Online") {
		t.Errorf("registered runner is not listed online")
	}
	// The token page never shows the token again.
	if registrationTokenRe.MatchString(page.Body.String()) {
		t.Error("a registration token is on the page after creation")
	}

	var id int64
	if err := e.db.QueryRow(`SELECT id FROM ci_runners WHERE name = $1`, "host-"+label).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if rr := e.form(t, http.MethodPost, "/admin/runners/"+strconv.FormatInt(id, 10)+"/revoke", e.owner.token, nil, true); rr.Code != http.StatusForbidden {
		t.Errorf("regular user revoke = %d, want 403", rr.Code)
	}
	rr = e.form(t, http.MethodPost, "/admin/runners/"+strconv.FormatInt(id, 10)+"/revoke", admin, nil, true)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Revoked") {
		t.Fatalf("revoke: %d %s", rr.Code, rr.Body.String())
	}
	if code := runnerClaimCode(e, runnerToken); code != http.StatusUnauthorized {
		t.Errorf("revoked runner claim = %d, want 401", code)
	}
}

func TestRepoRunners_ManagerManagesOwnRunnersOnly(t *testing.T) {
	e := newR2Env(t)
	base := "/" + e.owner.name + "/proj/settings/runners"
	label := "rep" + testutil.UniqueSuffix(t)

	page := e.form(t, http.MethodGet, "/"+e.owner.name+"/proj/settings", e.owner.token, nil, false)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `id="runners"`) || !strings.Contains(page.Body.String(), "No runners registered") {
		t.Fatalf("settings page lacks the runners section: %d", page.Code)
	}
	for name, tok := range map[string]string{"writer": e.writer.token, "outsider": e.outsider.token} {
		if rr := e.form(t, http.MethodPost, base+"/tokens", tok, url.Values{"labels": {"linux"}}, true); rr.Code != http.StatusForbidden {
			t.Errorf("%s token = %d, want 403", name, rr.Code)
		}
	}

	rr := e.form(t, http.MethodPost, base+"/tokens", e.owner.token, url.Values{"labels": {label}}, true)
	reg := registrationTokenRe.FindString(rr.Body.String())
	if rr.Code != http.StatusOK || reg == "" {
		t.Fatalf("token: %d %s", rr.Code, rr.Body.String())
	}
	runnerToken := registerRunnerWith(t, e, reg, label)

	var id int64
	var scope string
	var repoID int64
	if err := e.db.QueryRow(`SELECT id, scope, repo_id FROM ci_runners WHERE name = $1`, "host-"+label).Scan(&id, &scope, &repoID); err != nil {
		t.Fatal(err)
	}
	if scope != "repository" || repoID != e.repo.ID {
		t.Errorf("runner scope = %s repo %d, want repository %d", scope, repoID, e.repo.ID)
	}
	if page = e.form(t, http.MethodGet, "/"+e.owner.name+"/proj/settings", e.owner.token, nil, false); !strings.Contains(page.Body.String(), "host-"+label) {
		t.Error("the repo runner is not listed in settings")
	}
	// An instance admin page does not list it, and cannot revoke it from there.
	adminID := testutil.SeedSuperadmin(t, e.db, testutil.UniqueSuffix(t))
	admin := superadminJWT(t, adminID, "root_"+strconv.FormatInt(adminID, 10))
	if rr := e.form(t, http.MethodPost, "/admin/runners/"+strconv.FormatInt(id, 10)+"/revoke", admin, nil, true); rr.Code != http.StatusNotFound {
		t.Errorf("admin revoke of a repo runner = %d, want 404", rr.Code)
	}
	if rr := e.form(t, http.MethodPost, base+"/"+strconv.FormatInt(id, 10)+"/revoke", e.writer.token, nil, true); rr.Code != http.StatusForbidden {
		t.Errorf("writer revoke = %d, want 403", rr.Code)
	}
	rr = e.form(t, http.MethodPost, base+"/"+strconv.FormatInt(id, 10)+"/revoke", e.owner.token, nil, true)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Revoked") {
		t.Fatalf("revoke: %d %s", rr.Code, rr.Body.String())
	}
	if code := runnerClaimCode(e, runnerToken); code != http.StatusUnauthorized {
		t.Errorf("revoked runner claim = %d, want 401", code)
	}
}

func TestAdminRunners_AbsentWhileCIIsOff(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		ci := config.DefaultCI()
		ci.Enabled = enabled
		h, _, db := newRouterWithCI(t, config.SMTPConfig{}, ci)
		admin := superadminJWT(t, testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t)), "root")

		tabs := serve(h, browserRequest(http.MethodGet, "/admin/settings", admin, nil))
		if tabs.Code != http.StatusOK {
			t.Fatalf("enabled=%v: admin settings = %d", enabled, tabs.Code)
		}
		if got := strings.Contains(tabs.Body.String(), `href="/admin/runners"`); got != enabled {
			t.Errorf("enabled=%v: Runners tab shown = %v", enabled, got)
		}
		for _, c := range []struct{ method, path string }{
			{http.MethodGet, "/admin/runners"},
			{http.MethodPost, "/admin/runners/tokens"},
			{http.MethodPost, "/admin/runners/1/revoke"},
		} {
			code := serve(h, browserRequest(c.method, c.path, admin, url.Values{"labels": {"linux"}})).Code
			if enabled && code == http.StatusNotFound && c.method == http.MethodGet || !enabled && code != http.StatusNotFound {
				t.Errorf("enabled=%v: %s %s = %d", enabled, c.method, c.path, code)
			}
		}
	}
}
