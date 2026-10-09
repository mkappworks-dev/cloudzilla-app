package router_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestBranchProtectionRoutes_CreateListUpdateDelete(t *testing.T) {
	e := newR2Env(t)
	tok := e.admin.token

	rr := e.form(t, http.MethodPost, e.api("/branches/protections/"), tok, url.Values{
		"pattern":               {"  release/*  "},
		"require_review_count":  {"2"},
		"require_status_checks": {"ci/build, , ci/test ,"},
		"block_force_push":      {"true"},
		"require_pull_request":  {"true"},
	}, false)
	wantStatus(t, rr, http.StatusCreated)
	created := r2DecodeJSON[model.BranchProtection](t, rr)
	if created.ID == 0 || created.Pattern != "release/*" || created.RequireReviewCount != 2 ||
		!created.BlockForcePush || !created.RequirePullRequest {
		t.Fatalf("created = %+v", created)
	}
	if len(created.RequireStatusChecks) != 2 || created.RequireStatusChecks[0] != "ci/build" || created.RequireStatusChecks[1] != "ci/test" {
		t.Errorf("status checks = %v, want trimmed non-empty names", created.RequireStatusChecks)
	}

	rr = e.form(t, http.MethodGet, e.api("/branches/protections/"), tok, nil, false)
	wantStatus(t, rr, http.StatusOK)
	if rules := r2DecodeJSON[[]model.BranchProtection](t, rr); len(rules) != 1 || rules[0].ID != created.ID {
		t.Errorf("list = %+v", rules)
	}

	id := strconv.FormatInt(created.ID, 10)
	rr = e.form(t, http.MethodPatch, e.api("/branches/protections/"+id), tok, url.Values{
		"require_review_count": {"-5"},
		"block_force_push":     {"false"},
	}, false)
	wantStatus(t, rr, http.StatusNoContent)
	got, err := e.svc.BranchProtection.List(t.Context(), e.repo.ID)
	if err != nil || len(got) != 1 {
		t.Fatalf("List = %v, %v", got, err)
	}
	if got[0].RequireReviewCount != 0 || got[0].BlockForcePush || got[0].RequirePullRequest || len(got[0].RequireStatusChecks) != 0 {
		t.Errorf("after update = %+v; negative review count clamps to 0 and unset fields clear", got[0])
	}

	wantStatus(t, e.form(t, http.MethodDelete, e.api("/branches/protections/"+id), tok, nil, false), http.StatusNoContent)
	if got, _ := e.svc.BranchProtection.List(t.Context(), e.repo.ID); len(got) != 0 {
		t.Errorf("rule survived delete: %+v", got)
	}
}

func TestBranchProtectionRoutes_OnlyAdminsManageRules(t *testing.T) {
	e := newR2Env(t)
	form := url.Values{"pattern": {"main"}}
	for _, tok := range []string{e.writer.token, e.outsider.token} {
		wantStatus(t, e.form(t, http.MethodPost, e.api("/branches/protections/"), tok, form, false), http.StatusForbidden)
		wantStatus(t, e.form(t, http.MethodGet, e.api("/branches/protections/"), tok, nil, false), http.StatusForbidden)
	}
	wantStatus(t, e.form(t, http.MethodPost, e.api("/branches/protections/"), "", form, false), http.StatusUnauthorized)
	wantStatus(t, e.form(t, http.MethodGet, e.api("/branches/protections/"), "", nil, false), http.StatusUnauthorized)
	if rules, _ := e.svc.BranchProtection.List(t.Context(), e.repo.ID); len(rules) != 0 {
		t.Errorf("refused requests created rules: %+v", rules)
	}
}

func TestBranchProtectionRoutes_ValidationAndDuplicates(t *testing.T) {
	e := newR2Env(t)
	tok := e.owner.token
	wantStatus(t, e.form(t, http.MethodPost, e.api("/branches/protections/"), tok, url.Values{"pattern": {"   "}}, false), http.StatusBadRequest)

	wantStatus(t, e.form(t, http.MethodPost, e.api("/branches/protections/"), tok, url.Values{"pattern": {"main"}}, false), http.StatusCreated)
	rr := e.form(t, http.MethodPost, e.api("/branches/protections/"), tok, url.Values{"pattern": {"main"}}, false)
	wantStatus(t, rr, http.StatusUnprocessableEntity)
	if r2ErrorMessage(t, rr) != "failed to create branch protection" {
		t.Errorf("error = %q", r2ErrorMessage(t, rr))
	}

	wantStatus(t, e.form(t, http.MethodPatch, e.api("/branches/protections/abc"), tok, url.Values{}, false), http.StatusBadRequest)
	wantStatus(t, e.form(t, http.MethodDelete, e.api("/branches/protections/abc"), tok, nil, false), http.StatusBadRequest)
	wantStatus(t, e.form(t, http.MethodPatch, e.api("/branches/protections/999999999"), tok, url.Values{}, false), http.StatusUnprocessableEntity)
}

func TestBranchProtectionRoutes_CreateRejectsMalformedGlob(t *testing.T) {
	e := newR2Env(t)
	for _, pattern := range []string{"[", "a[", `\`} {
		rr := e.form(t, http.MethodPost, e.api("/branches/protections/"), e.owner.token, url.Values{"pattern": {pattern}}, false)
		wantStatus(t, rr, http.StatusUnprocessableEntity)
		if msg := r2ErrorMessage(t, rr); !strings.Contains(msg, "pattern") {
			t.Errorf("pattern %q: error = %q, want it to name the pattern", pattern, msg)
		}
	}
	if rules, _ := e.svc.BranchProtection.List(t.Context(), e.repo.ID); len(rules) != 0 {
		t.Errorf("refused requests stored rules: %+v", rules)
	}

	wantStatus(t, e.form(t, http.MethodPost, e.api("/branches/protections/"), e.owner.token, url.Values{"pattern": {"release/*"}}, false), http.StatusCreated)
}

func TestBranchProtectionRoutes_RulesOfOtherReposAreUntouchable(t *testing.T) {
	e := newR2Env(t)
	other := newR2Env(t)
	rr := other.form(t, http.MethodPost, other.api("/branches/protections/"), other.owner.token, url.Values{"pattern": {"main"}, "require_review_count": {"3"}}, false)
	wantStatus(t, rr, http.StatusCreated)
	foreign := strconv.FormatInt(r2DecodeJSON[model.BranchProtection](t, rr).ID, 10)

	wantStatus(t, e.form(t, http.MethodPatch, e.api("/branches/protections/"+foreign), e.owner.token, url.Values{"require_review_count": {"0"}}, false), http.StatusUnprocessableEntity)
	wantStatus(t, e.form(t, http.MethodDelete, e.api("/branches/protections/"+foreign), e.owner.token, nil, false), http.StatusNoContent)

	rules, _ := other.svc.BranchProtection.List(t.Context(), other.repo.ID)
	if len(rules) != 1 || rules[0].RequireReviewCount != 3 {
		t.Errorf("other repo's rule was changed through this repo's URL: %+v", rules)
	}
}

func TestBranchProtectionRoutes_HTMXRendersRuleList(t *testing.T) {
	e := newR2Env(t)
	tok := e.owner.token
	rr := e.form(t, http.MethodPost, e.api("/branches/protections/"), tok, url.Values{"pattern": {"hx-pattern"}}, true)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "hx-pattern")

	rules, _ := e.svc.BranchProtection.List(t.Context(), e.repo.ID)
	id := strconv.FormatInt(rules[0].ID, 10)
	rr = e.form(t, http.MethodPatch, e.api("/branches/protections/"+id), tok, url.Values{"require_review_count": {"1"}}, true)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "hx-pattern")

	rr = e.form(t, http.MethodDelete, e.api("/branches/protections/"+id), tok, nil, true)
	wantStatus(t, rr, http.StatusOK)
}
