package router_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestAccountPages_RequireSignIn(t *testing.T) {
	h, _, _ := newVerificationRouter(t, config.SMTPConfig{})
	for _, path := range []string{"/repos", "/pulls", "/issues", "/attention", "/stars", "/repos/transfers", "/repos/import"} {
		rr := serve(h, browserRequest(http.MethodGet, path, "", nil))
		if (rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound) || !strings.HasPrefix(rr.Header().Get("Location"), "/login") {
			t.Errorf("GET %s signed out: got %d to %q, want a redirect to /login", path, rr.Code, rr.Header().Get("Location"))
		}
	}
}

func TestAccountRepos_FilterSortAndPaging(t *testing.T) {
	e := newTransferEnv(t)
	me, other := e.account(t), e.account(t)
	ctx := context.Background()
	mk := func(a transferAccount, name string) {
		t.Helper()
		if _, err := e.svc.Repo.Create(ctx, a.id, a.name, name, "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	suffix := testutil.UniqueSuffix(t)
	zeta, alpha, theirs := "zeta_"+suffix, "alpha_"+suffix, "theirs_"+suffix
	mk(me, zeta)
	mk(me, alpha)
	mk(other, theirs)
	if err := e.svc.Star.Star(ctx, me.name, zeta, other.id); err != nil {
		t.Fatalf("star: %v", err)
	}

	get := func(query string) string {
		t.Helper()
		rr := serve(e.h, browserRequest(http.MethodGet, "/repos"+query, me.session, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /repos%s = %d", query, rr.Code)
		}
		return rr.Body.String()
	}

	body := get("")
	if !strings.Contains(body, zeta) || !strings.Contains(body, alpha) || strings.Contains(body, theirs) {
		t.Errorf("default list should show my repos only")
	}
	if body := get("?sort=name"); strings.Index(body, alpha) > strings.Index(body, zeta) {
		t.Error("sort=name should put alpha before zeta")
	}
	if body := get("?sort=stars"); strings.Index(body, zeta) > strings.Index(body, alpha) {
		t.Error("sort=stars should put the starred repo first")
	}
	if body := get("?sort=created"); !strings.Contains(body, alpha) {
		t.Error("sort=created lost a repo")
	}
	if body := get("?filter=forks"); strings.Contains(body, zeta) {
		t.Error("filter=forks listed a non-fork")
	}
	if body := get("?filter=collaborator"); strings.Contains(body, zeta) {
		t.Error("filter=collaborator listed an owned repo")
	}
	if body := get("?filter=owned&language=NoSuchLanguage"); strings.Contains(body, zeta) {
		t.Error("an unmatched language filter still listed repos")
	}
	if body := get("?page=999&sort=bogus&filter=bogus"); !strings.Contains(body, zeta) {
		t.Error("an out-of-range page with unknown filter/sort should fall back to defaults")
	}
}

func TestAccountIssuesPullsAttention_FilterVariants(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)
	ctx := context.Background()
	repo, err := e.svc.Repo.Create(ctx, me.id, me.name, "issues_"+testutil.UniqueSuffix(t), "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatal(err)
	}
	title := "needs-attention-" + testutil.UniqueSuffix(t)
	if _, err := e.svc.Issue.Create(ctx, me.name, repo.Name, me.id, title, "body", ""); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	issues := serve(e.h, browserRequest(http.MethodGet, "/issues?filter=created&sort=oldest", me.session, nil))
	if issues.Code != http.StatusOK || !strings.Contains(issues.Body.String(), title) {
		t.Errorf("created issues list = %d, want it to list %q", issues.Code, title)
	}
	if rr := serve(e.h, browserRequest(http.MethodGet, "/issues?filter=assigned", me.session, nil)); rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), title) {
		t.Errorf("assigned issues = %d, want an issue I only created to be absent", rr.Code)
	}
	for _, q := range []string{"?state=closed", "?filter=mentioned&sort=updated", "?sort=comments", "?filter=bogus&state=bogus&sort=bogus"} {
		if rr := serve(e.h, browserRequest(http.MethodGet, "/issues"+q, me.session, nil)); rr.Code != http.StatusOK {
			t.Errorf("GET /issues%s = %d", q, rr.Code)
		}
	}
	for _, q := range []string{"", "?filter=assigned", "?filter=review_requested&state=closed", "?filter=mentioned&sort=oldest", "?sort=updated", "?sort=comments", "?filter=bogus&sort=bogus"} {
		if rr := serve(e.h, browserRequest(http.MethodGet, "/pulls"+q, me.session, nil)); rr.Code != http.StatusOK {
			t.Errorf("GET /pulls%s = %d", q, rr.Code)
		}
	}
	for _, q := range []string{"", "?kind=mentions", "?kind=reviews&sort=newest", "?kind=assigned&sort=oldest", "?kind=bogus&sort=bogus"} {
		if rr := serve(e.h, browserRequest(http.MethodGet, "/attention"+q, me.session, nil)); rr.Code != http.StatusOK {
			t.Errorf("GET /attention%s = %d", q, rr.Code)
		}
	}
}

func TestAccountStars_ListsAndFiltersStarredRepos(t *testing.T) {
	e := newTransferEnv(t)
	me, owner := e.account(t), e.account(t)
	ctx := context.Background()
	name := "starred_" + testutil.UniqueSuffix(t)
	if _, err := e.svc.Repo.Create(ctx, owner.id, owner.name, name, "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatal(err)
	}

	empty := serve(e.h, browserRequest(http.MethodGet, "/stars", me.session, nil))
	if empty.Code != http.StatusOK || strings.Contains(empty.Body.String(), name) {
		t.Fatalf("before starring: %d, repo listed = %v", empty.Code, strings.Contains(empty.Body.String(), name))
	}
	if err := e.svc.Star.Star(ctx, owner.name, name, me.id); err != nil {
		t.Fatal(err)
	}
	if rr := serve(e.h, browserRequest(http.MethodGet, "/stars", me.session, nil)); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), name) {
		t.Errorf("after starring: %d, want the repo listed", rr.Code)
	}
	if rr := serve(e.h, browserRequest(http.MethodGet, "/stars?language=NoSuchLanguage", me.session, nil)); rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), name) {
		t.Errorf("language filter: %d, want the repo filtered out", rr.Code)
	}
}
