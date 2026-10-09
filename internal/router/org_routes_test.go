package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// orgEnv reuses metaEnv's users: owner owns the org, writer is a plain member and outsider is not in it.
type orgEnv struct {
	metaEnv
	name  string
	orgID int64
}

func newOrgEnv(t *testing.T) orgEnv {
	t.Helper()
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	user := func() metaUser {
		sfx := testutil.UniqueSuffix(t)
		id := testutil.SeedUser(t, db, sfx)
		name := "testuser_" + sfx
		return metaUser{id: id, name: name, token: makeJWT(t, id, name)}
	}
	e := orgEnv{metaEnv: metaEnv{h: h, svc: svc, db: db, owner: user(), writer: user(), outsider: user()}}
	e.givePassword(t)
	org, err := svc.Org.Create(context.Background(), e.owner.id, "testorg_"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, db, org.ID)
	e.name, e.orgID = org.Name, org.ID
	if err := svc.Org.AddMember(context.Background(), org.ID, e.owner.id, e.writer.id, model.OrgRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return e
}

func (e orgEnv) orgPath(format string, args ...any) string {
	return "/api/orgs/" + e.name + fmt.Sprintf(format, args...)
}

func (e orgEnv) roleOf(t *testing.T, userID int64) string {
	t.Helper()
	var role *string
	err := e.db.QueryRow(`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, e.orgID, userID).Scan(&role)
	if err != nil || role == nil {
		return ""
	}
	return *role
}

func (e orgEnv) orgCol(t *testing.T, col string) string {
	t.Helper()
	var v *string
	if err := e.db.QueryRow(`SELECT `+col+`::text FROM organizations WHERE id = $1`, e.orgID).Scan(&v); err != nil {
		return ""
	}
	if v == nil {
		return ""
	}
	return *v
}

func TestOrgs_Create(t *testing.T) {
	e := newOrgEnv(t)
	name := "testorg_" + testutil.UniqueSuffix(t)

	rr := e.do(t, metaReq{method: "POST", target: "/api/orgs", token: e.outsider.token,
		json: fmt.Sprintf(`{"name":%q,"display_name":"Shown","description":"about"}`, name)})
	wantStatus(t, rr, http.StatusCreated)
	var org struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &org); err != nil || org.ID == 0 {
		t.Fatalf("created = %s (%v)", rr.Body.String(), err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, org.ID)
	if n := e.count(t, `SELECT COUNT(*) FROM org_members WHERE org_id = $1 AND user_id = $2 AND role = 'owner'`, org.ID, e.outsider.id); n != 1 {
		t.Error("the creator must become the org owner")
	}

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: "/api/orgs", json: `{"name":"anon_org"}`}, http.StatusUnauthorized},
		{"bad json", metaReq{method: "POST", target: "/api/orgs", token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"invalid name", metaReq{method: "POST", target: "/api/orgs", token: e.owner.token, json: `{"name":"bad name!"}`}, http.StatusUnprocessableEntity},
		{"empty name", metaReq{method: "POST", target: "/api/orgs", token: e.owner.token, json: `{"name":""}`}, http.StatusUnprocessableEntity},
		{"name taken", metaReq{method: "POST", target: "/api/orgs", token: e.owner.token, json: fmt.Sprintf(`{"name":%q}`, name)}, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
}

func TestOrgs_GetAndListMembers(t *testing.T) {
	e := newOrgEnv(t)

	rr := e.do(t, metaReq{method: "GET", target: e.orgPath("")})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, e.name)
	rr = e.do(t, metaReq{method: "GET", target: e.orgPath("/members")})
	wantStatus(t, rr, http.StatusOK)
	var members []struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &members); err != nil || len(members) != 2 {
		t.Fatalf("members = %s (%v)", rr.Body.String(), err)
	}

	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/orgs/nope_org_zzz"}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/orgs/nope_org_zzz/members"}), http.StatusNotFound)
}

func TestOrgs_AddMember(t *testing.T) {
	e := newOrgEnv(t)
	target := e.orgPath("/members")

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, form: url.Values{"username": {e.outsider.name}}}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, form: url.Values{"username": {e.outsider.name}}}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, json: `{`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, form: url.Values{"username": {"nobody_zzz"}}}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: "/api/orgs/nope_org_zzz/members", token: e.owner.token, form: url.Values{"username": {e.outsider.name}}}), http.StatusNotFound)
	if e.roleOf(t, e.outsider.id) != "" {
		t.Fatal("refusals added the member")
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, form: url.Values{"username": {e.outsider.name}, "role": {"bogus"}}}), http.StatusUnprocessableEntity)
	if e.roleOf(t, e.outsider.id) != "" {
		t.Fatal("an unknown role added the member")
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, form: url.Values{"username": {e.outsider.name}}}), http.StatusCreated)
	if e.roleOf(t, e.outsider.id) != "member" {
		t.Errorf("default role = %q, want member", e.roleOf(t, e.outsider.id))
	}
}

func TestOrgs_AddOwnerNeedsConfirmation(t *testing.T) {
	e := newOrgEnv(t)
	target := e.orgPath("/members")

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, json: fmt.Sprintf(`{"username":%q,"role":"owner"}`, e.outsider.name)}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, json: fmt.Sprintf(`{"username":%q,"role":"owner","password":%q}`, e.outsider.name, ownerPassword)}), http.StatusForbidden)
	if e.roleOf(t, e.outsider.id) != "" {
		t.Fatal("an unconfirmed or non-owner request made an owner")
	}

	rr := e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, htmx: true,
		form: url.Values{"username": {e.outsider.name}, "role": {"owner"}, "password": {"wrong"}}})
	if rr.Header().Get("HX-Retarget") != "#invite-member-form-error" {
		t.Errorf("wrong password: HX-Retarget = %q (status %d)", rr.Header().Get("HX-Retarget"), rr.Code)
	}
	if e.roleOf(t, e.outsider.id) != "" {
		t.Fatal("a wrong password made an owner")
	}

	rr = e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, htmx: true,
		form: url.Values{"username": {e.outsider.name}, "role": {"owner"}, "password": {ownerPassword}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, e.outsider.name)
	if e.roleOf(t, e.outsider.id) != "owner" {
		t.Errorf("role = %q, want owner", e.roleOf(t, e.outsider.id))
	}
}

func TestOrgs_AddMemberHTMXErrors(t *testing.T) {
	e := newOrgEnv(t)
	target := e.orgPath("/members")

	rr := e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, htmx: true, form: url.Values{"username": {"nobody_zzz"}}})
	if rr.Header().Get("HX-Retarget") != "#invite-member-form-error" {
		t.Fatalf("HX-Retarget = %q (status %d)", rr.Header().Get("HX-Retarget"), rr.Code)
	}
	bodyHas(t, rr, "No user has that username.")

	rr = e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, htmx: true, form: url.Values{"username": {e.outsider.name}}})
	if rr.Header().Get("HX-Retarget") != "#invite-member-form-error" {
		t.Fatalf("non-owner HX-Retarget = %q (status %d)", rr.Header().Get("HX-Retarget"), rr.Code)
	}
	bodyHas(t, rr, "only org owners can add members")
}

func TestOrgs_RemoveMember(t *testing.T) {
	e := newOrgEnv(t)
	third := newOrgEnv(t)
	if err := e.svc.Org.AddMember(context.Background(), e.orgID, e.owner.id, third.writer.id, model.OrgRoleMember); err != nil {
		t.Fatal(err)
	}
	del := func(token, user string, htmx bool) *httpResult {
		rr := e.do(t, metaReq{method: "DELETE", target: e.orgPath("/members/%s", user), token: token, htmx: htmx})
		return &httpResult{rr.Code, "", rr.Body.String()}
	}

	if r := del("", e.writer.name, false); r.code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", r.code)
	}
	if r := del(e.writer.token, third.writer.name, false); r.code != http.StatusUnprocessableEntity {
		t.Errorf("member removing another = %d", r.code)
	}
	if r := del(e.outsider.token, e.writer.name, false); r.code != http.StatusUnprocessableEntity {
		t.Errorf("outsider removing a member = %d", r.code)
	}
	if r := del(e.owner.token, "nobody_zzz", false); r.code != http.StatusNotFound {
		t.Errorf("unknown user = %d", r.code)
	}
	if r := del(e.owner.token, e.outsider.name, false); r.code != http.StatusUnprocessableEntity {
		t.Errorf("non-member = %d", r.code)
	}
	if r := del(e.owner.token, e.owner.name, false); r.code != http.StatusUnprocessableEntity {
		t.Errorf("last owner removing themselves = %d", r.code)
	}
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: "/api/orgs/nope_org_zzz/members/" + e.writer.name, token: e.owner.token}), http.StatusNotFound)
	if e.roleOf(t, e.writer.id) == "" || e.roleOf(t, third.writer.id) == "" || e.roleOf(t, e.owner.id) != "owner" {
		t.Fatal("refusals changed the membership")
	}

	if r := del(e.owner.token, e.writer.name, false); r.code != http.StatusNoContent {
		t.Errorf("owner removing a member = %d", r.code)
	}
	if r := del(third.writer.token, third.writer.name, true); r.code != http.StatusOK {
		t.Errorf("member leaving = %d", r.code)
	}
	if e.roleOf(t, e.writer.id) != "" || e.roleOf(t, third.writer.id) != "" {
		t.Error("members were not removed")
	}
}

func TestOrgs_UpdateMemberRole(t *testing.T) {
	e := newOrgEnv(t)
	target := e.orgPath("/members/%s/role", e.writer.name)
	setRole := func(token string, role string, extra url.Values) *httpResult {
		form := url.Values{"role": {role}}
		for k, v := range extra {
			form[k] = v
		}
		rr := e.do(t, metaReq{method: "POST", target: target, token: token, form: form})
		return &httpResult{rr.Code, rr.Header().Get("HX-Retarget"), rr.Body.String()}
	}

	if r := setRole("", "member", nil); r.code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", r.code)
	}
	if r := setRole(e.writer.token, "owner", url.Values{"password": {"x"}}); r.code != http.StatusForbidden {
		t.Errorf("a member promoting themselves = %d, want 403", r.code)
	}
	if r := setRole(e.outsider.token, "member", nil); r.code != http.StatusUnprocessableEntity {
		t.Errorf("outsider = %d", r.code)
	}
	if r := setRole(e.owner.token, "admin", nil); r.code != http.StatusUnprocessableEntity {
		t.Errorf("unknown role = %d", r.code)
	}
	if r := setRole(e.owner.token, "owner", nil); r.code != http.StatusForbidden {
		t.Errorf("promotion without confirmation = %d", r.code)
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.orgPath("/members/nobody_zzz/role"), token: e.owner.token, form: url.Values{"role": {"member"}}}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: "/api/orgs/nope_org_zzz/members/" + e.writer.name + "/role", token: e.owner.token, form: url.Values{"role": {"member"}}}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.orgPath("/members/%s/role", e.outsider.name), token: e.owner.token, form: url.Values{"role": {"member"}}}), http.StatusUnprocessableEntity)
	if e.roleOf(t, e.writer.id) != "member" {
		t.Fatal("refusals changed the role")
	}

	if r := setRole(e.owner.token, "owner", url.Values{"password": {ownerPassword}}); r.code != http.StatusOK {
		t.Fatalf("confirmed promotion = %d %s", r.code, r.body)
	}
	if e.roleOf(t, e.writer.id) != "owner" {
		t.Error("member not promoted")
	}
	if r := setRole(e.owner.token, "owner", url.Values{"password": {ownerPassword}}); r.code != http.StatusOK {
		t.Errorf("promoting an owner again = %d", r.code)
	}
	if r := setRole(e.owner.token, "member", nil); r.code != http.StatusOK {
		t.Errorf("demotion = %d", r.code)
	}
	if e.roleOf(t, e.writer.id) != "member" {
		t.Error("owner not demoted")
	}

	soleTarget := e.orgPath("/members/%s/role", e.owner.name)
	rr := e.do(t, metaReq{method: "POST", target: soleTarget, token: e.owner.token, form: url.Values{"role": {"member"}}})
	wantStatus(t, rr, http.StatusUnprocessableEntity)
	bodyHas(t, rr, "last owner")
	if e.roleOf(t, e.owner.id) != "owner" {
		t.Error("the last owner was demoted")
	}

	rr = e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, htmx: true, form: url.Values{"role": {"owner"}, "password": {ownerPassword}}})
	wantStatus(t, rr, http.StatusOK)
	if !strings.Contains(rr.Header().Get("HX-Trigger"), "Member role updated") {
		t.Errorf("HX-Trigger = %q", rr.Header().Get("HX-Trigger"))
	}
}

func TestOrgs_RepoDefaults(t *testing.T) {
	e := newOrgEnv(t)
	target := e.orgPath("/repo-defaults")
	post := func(token string, htmx bool, form url.Values) *httpResult {
		rr := e.do(t, metaReq{method: "POST", target: target, token: token, htmx: htmx, form: form})
		return &httpResult{rr.Code, rr.Header().Get("Location") + rr.Header().Get("HX-Redirect"), rr.Header().Get("HX-Retarget")}
	}
	ok := url.Values{"default_repo_visibility": {"public"}, "default_branch_name": {"trunk"}}

	if r := post("", false, ok); r.code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", r.code)
	}
	if r := post(e.writer.token, false, ok); r.code != http.StatusUnprocessableEntity {
		t.Errorf("member = %d", r.code)
	}
	if r := post(e.owner.token, false, url.Values{"default_repo_visibility": {"secret"}, "default_branch_name": {"main"}}); r.code != http.StatusUnprocessableEntity {
		t.Errorf("bad visibility = %d", r.code)
	}
	if r := post(e.owner.token, true, url.Values{"default_repo_visibility": {"public"}, "default_branch_name": {"two words"}}); r.body != "#org-repo-defaults-form-error" {
		t.Errorf("bad branch name: retarget = %q (status %d)", r.body, r.code)
	}
	if r := post(e.owner.token, false, url.Values{"default_repo_visibility": {"public"}, "default_branch_name": {""}}); r.code != http.StatusUnprocessableEntity {
		t.Errorf("empty branch name = %d", r.code)
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: "/api/orgs/nope_org_zzz/repo-defaults", token: e.owner.token, form: ok}), http.StatusNotFound)
	if e.orgCol(t, "default_branch_name") == "trunk" {
		t.Fatal("refusals saved the defaults")
	}

	r := post(e.owner.token, false, ok)
	if r.code != http.StatusSeeOther || r.location != "/orgs/"+e.name+"/settings#repo-defaults" {
		t.Errorf("save = %+v", r)
	}
	if e.orgCol(t, "default_branch_name") != "trunk" || e.orgCol(t, "default_repo_visibility") != "public" {
		t.Error("defaults not stored")
	}
	if r := post(e.owner.token, true, ok); r.code != http.StatusNoContent || r.location != "/orgs/"+e.name+"/settings#repo-defaults" {
		t.Errorf("htmx save = %+v", r)
	}
}

func TestOrgs_Profile(t *testing.T) {
	e := newOrgEnv(t)
	target := e.orgPath("/profile")
	full := url.Values{"display_name": {"Shown Name"}, "description": {"what we do"}, "website": {"example.com"}, "location": {"Berlin"}, "contact_email": {"hi@example.com"}}
	post := func(token string, htmx bool, form url.Values) *httptestResult {
		rr := e.do(t, metaReq{method: "POST", target: target, token: token, htmx: htmx, form: form})
		return &httptestResult{rr.Code, rr.Header().Get("Location"), rr.Header().Get("HX-Redirect"), rr.Header().Get("HX-Retarget"), rr.Body.String()}
	}

	if r := post("", false, full); r.code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", r.code)
	}
	if r := post(e.writer.token, false, full); r.code != http.StatusUnprocessableEntity {
		t.Errorf("member = %d", r.code)
	}
	bad := url.Values{"display_name": {"x"}, "website": {"ftp://example.com"}}
	if r := post(e.owner.token, false, bad); r.code != http.StatusUnprocessableEntity || !strings.Contains(r.body, "http or https") {
		t.Errorf("bad website = %d %s", r.code, r.body)
	}
	if r := post(e.owner.token, true, bad); r.retarget != "#org-profile-form-error" {
		t.Errorf("bad website htmx retarget = %q (status %d)", r.retarget, r.code)
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: "/api/orgs/nope_org_zzz/profile", token: e.owner.token, form: full}), http.StatusNotFound)
	if e.orgCol(t, "display_name") == "Shown Name" {
		t.Fatal("refusals saved the profile")
	}

	r := post(e.owner.token, false, full)
	if r.code != http.StatusSeeOther || r.location != "/orgs/"+e.name+"/settings" {
		t.Errorf("save = %+v", r)
	}
	if e.orgCol(t, "display_name") != "Shown Name" || e.orgCol(t, "website") != "https://example.com" || e.orgCol(t, "location") != "Berlin" {
		t.Errorf("profile not stored: website=%q", e.orgCol(t, "website"))
	}
	if r := post(e.owner.token, true, full); r.code != http.StatusNoContent || r.hxRedirect != "/orgs/"+e.name+"/settings" {
		t.Errorf("htmx save = %+v", r)
	}
}

type httptestResult struct {
	code                                 int
	location, hxRedirect, retarget, body string
}

func TestOrgs_Delete(t *testing.T) {
	e := newOrgEnv(t)
	target := e.orgPath("/delete")
	post := func(token string, htmx bool, form url.Values) *httptestResult {
		rr := e.do(t, metaReq{method: "POST", target: target, token: token, htmx: htmx, form: form})
		return &httptestResult{rr.Code, rr.Header().Get("Location"), rr.Header().Get("HX-Redirect"), rr.Header().Get("HX-Retarget"), rr.Body.String()}
	}
	exists := func() bool { return e.count(t, `SELECT COUNT(*) FROM organizations WHERE id = $1`, e.orgID) == 1 }
	confirmed := url.Values{"confirm_name": {e.name}, "password": {ownerPassword}}

	if r := post("", false, confirmed); r.code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", r.code)
	}
	if r := post(e.owner.token, false, url.Values{"confirm_name": {"other"}, "password": {ownerPassword}}); r.code != http.StatusUnprocessableEntity {
		t.Errorf("wrong confirm name = %d", r.code)
	}
	if r := post(e.owner.token, true, url.Values{"confirm_name": {"other"}}); r.retarget != "#delete-org-form-error" {
		t.Errorf("wrong confirm name htmx retarget = %q", r.retarget)
	}
	if r := post(e.writer.token, false, confirmed); r.code != http.StatusForbidden {
		t.Errorf("member = %d", r.code)
	}
	if r := post(e.owner.token, false, url.Values{"confirm_name": {e.name}, "password": {"wrong"}}); r.code != http.StatusForbidden {
		t.Errorf("wrong password = %d", r.code)
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: "/api/orgs/nope_org_zzz/delete", token: e.owner.token, form: confirmed}), http.StatusNotFound)
	if !exists() {
		t.Fatal("refusals deleted the org")
	}

	r := post(e.owner.token, true, confirmed)
	if r.code != http.StatusNoContent || r.hxRedirect != "/organizations" {
		t.Fatalf("delete = %+v", r)
	}
	if exists() {
		t.Error("org not deleted")
	}
}

func TestOrgs_DeleteRedirect(t *testing.T) {
	e := newOrgEnv(t)
	rr := e.do(t, metaReq{method: "POST", target: e.orgPath("/delete"), token: e.owner.token, form: url.Values{"confirm_name": {e.name}, "password": {ownerPassword}}})
	wantStatus(t, rr, http.StatusSeeOther)
	if rr.Header().Get("Location") != "/organizations" {
		t.Errorf("Location = %q", rr.Header().Get("Location"))
	}
}

func TestOrgs_Transfer(t *testing.T) {
	e := newOrgEnv(t)
	target := e.orgPath("/transfer")
	post := func(token string, htmx bool, form url.Values) *httptestResult {
		rr := e.do(t, metaReq{method: "POST", target: target, token: token, htmx: htmx, form: form})
		return &httptestResult{rr.Code, rr.Header().Get("Location"), rr.Header().Get("HX-Redirect"), rr.Header().Get("HX-Retarget"), rr.Body.String()}
	}
	with := func(newOwner string) url.Values {
		return url.Values{"new_owner": {newOwner}, "confirm_name": {e.name}, "password": {ownerPassword}}
	}

	if r := post("", false, with(e.writer.name)); r.code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", r.code)
	}
	if r := post(e.owner.token, false, url.Values{"password": {ownerPassword}}); r.code != http.StatusBadRequest {
		t.Errorf("missing new_owner = %d", r.code)
	}
	if r := post(e.owner.token, true, url.Values{"password": {ownerPassword}}); r.retarget != "#transfer-org-form-error" {
		t.Errorf("missing new_owner htmx retarget = %q", r.retarget)
	}
	if r := post(e.owner.token, false, url.Values{"new_owner": {e.writer.name}, "confirm_name": {"other"}, "password": {ownerPassword}}); r.code != http.StatusUnprocessableEntity {
		t.Errorf("wrong confirm name = %d", r.code)
	}
	if r := post(e.writer.token, false, with(e.writer.name)); r.code != http.StatusForbidden {
		t.Errorf("member = %d", r.code)
	}
	if r := post(e.owner.token, false, url.Values{"new_owner": {e.writer.name}, "confirm_name": {e.name}, "password": {"wrong"}}); r.code != http.StatusForbidden {
		t.Errorf("wrong password = %d", r.code)
	}
	if r := post(e.owner.token, false, with("nobody_zzz")); r.code != http.StatusUnprocessableEntity {
		t.Errorf("unknown new owner = %d", r.code)
	}
	if r := post(e.owner.token, false, with(e.owner.name)); r.code != http.StatusUnprocessableEntity {
		t.Errorf("transfer to self = %d", r.code)
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: "/api/orgs/nope_org_zzz/transfer", token: e.owner.token, form: with(e.writer.name)}), http.StatusNotFound)
	if e.roleOf(t, e.owner.id) != "owner" || e.roleOf(t, e.writer.id) != "member" {
		t.Fatal("refusals changed ownership")
	}

	r := post(e.owner.token, false, with(e.outsider.name))
	if r.code != http.StatusSeeOther || r.location != "/"+e.name {
		t.Fatalf("transfer = %+v", r)
	}
	if e.roleOf(t, e.outsider.id) != "owner" || e.roleOf(t, e.owner.id) != "member" {
		t.Errorf("roles after transfer: new=%q old=%q", e.roleOf(t, e.outsider.id), e.roleOf(t, e.owner.id))
	}
}

func TestOrgs_TransferHTMX(t *testing.T) {
	e := newOrgEnv(t)
	rr := e.do(t, metaReq{method: "POST", target: e.orgPath("/transfer"), token: e.owner.token, htmx: true,
		form: url.Values{"new_owner": {e.writer.name}, "password": {ownerPassword}}})
	wantStatus(t, rr, http.StatusNoContent)
	if rr.Header().Get("HX-Redirect") != "/"+e.name {
		t.Errorf("HX-Redirect = %q", rr.Header().Get("HX-Redirect"))
	}
	if e.roleOf(t, e.writer.id) != "owner" || e.roleOf(t, e.owner.id) != "member" {
		t.Error("ownership not transferred when confirm_name is omitted")
	}
}

func TestOrgs_CreateRepo(t *testing.T) {
	e := newOrgEnv(t)
	target := e.orgPath("/repos")

	rr := e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, json: `{"name":"first","description":"d","add_readme":true}`})
	wantStatus(t, rr, http.StatusCreated)
	var priv bool
	var org int64
	if err := e.db.QueryRow(`SELECT private, org_id FROM repositories WHERE owner_name = $1 AND name = 'first'`, e.name).Scan(&priv, &org); err != nil {
		t.Fatalf("stored repo: %v", err)
	}
	if !priv || org != e.orgID {
		t.Errorf("private = %v, org_id = %d; an org repo defaults to the org's visibility", priv, org)
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, json: `{"name":"open","private":false}`}), http.StatusCreated)
	if n := e.count(t, `SELECT COUNT(*) FROM repositories WHERE owner_name = $1 AND name = 'open' AND NOT private`, e.name); n != 1 {
		t.Error("explicit private=false ignored")
	}

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: target, json: `{"name":"x"}`}, http.StatusUnauthorized},
		{"bad json", metaReq{method: "POST", target: target, token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"unknown org", metaReq{method: "POST", target: "/api/orgs/nope_org_zzz/repos", token: e.owner.token, json: `{"name":"x"}`}, http.StatusNotFound},
		{"member", metaReq{method: "POST", target: target, token: e.writer.token, json: `{"name":"byMember"}`}, http.StatusUnprocessableEntity},
		{"invalid name", metaReq{method: "POST", target: target, token: e.owner.token, json: `{"name":"bad name"}`}, http.StatusUnprocessableEntity},
		{"duplicate", metaReq{method: "POST", target: target, token: e.owner.token, json: `{"name":"first"}`}, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if n := e.count(t, `SELECT COUNT(*) FROM repositories WHERE owner_name = $1`, e.name); n != 2 {
		t.Errorf("org repos = %d, want 2", n)
	}
}
