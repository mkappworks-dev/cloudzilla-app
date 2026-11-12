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

	issues := serve(e.h, browserRequest(http.MethodGet, "/issues?q=author:@me+sort:oldest", me.session, nil))
	if issues.Code != http.StatusOK || !strings.Contains(issues.Body.String(), title) {
		t.Errorf("created issues list = %d, want it to list %q", issues.Code, title)
	}
	if rr := serve(e.h, browserRequest(http.MethodGet, "/issues?q=assignee:@me", me.session, nil)); rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), title) {
		t.Errorf("assigned issues = %d, want an issue I only created to be absent", rr.Code)
	}
	for _, q := range []string{"?q=is:closed", "?q=mentions:@me+sort:updated", "?q=sort:comments", "?q=reviewer:@me+review:approved", "?q=repo:no/such"} {
		if rr := serve(e.h, browserRequest(http.MethodGet, "/issues"+q, me.session, nil)); rr.Code != http.StatusOK {
			t.Errorf("GET /issues%s = %d", q, rr.Code)
		}
	}
	for _, q := range []string{"?q=", "?q=assignee:@me", "?q=reviewer:@me+is:closed", "?q=mentions:@me+sort:oldest", "?q=is:merged", "?q=is:draft+review:required", "?q=review:none+repo:a/b"} {
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

func TestAccountLists_LegacyURLsRedirectToTheQualifierBar(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)
	for _, tc := range []struct{ from, to string }{
		{"/issues", "/issues?q=assignee%3A%40me"},
		{"/pulls", "/pulls?q=author%3A%40me"},
		{"/issues?filter=created&state=closed&sort=oldest", "/issues?q=is%3Aclosed+author%3A%40me+sort%3Aoldest"},
		{"/issues?filter=mentioned", "/issues?q=mentions%3A%40me"},
		{"/issues?state=closed", "/issues?q=is%3Aclosed+assignee%3A%40me"},
		{"/pulls?filter=review_requested&state=closed", "/pulls?q=is%3Aclosed+reviewer%3A%40me"},
		{"/pulls?filter=assigned&sort=recently-updated", "/pulls?q=assignee%3A%40me+sort%3Aupdated"},
		{"/pulls?filter=bogus&state=bogus&sort=bogus", "/pulls?q=author%3A%40me"},
		{"/issues?q=label:bug&state=closed", "/issues?q=is%3Aclosed+label%3Abug"},
		{"/issues?f=&v=author:@me&q=crash", "/issues?q=author%3A%40me+crash"},
		{"/issues?f=author:@me&v=is:closed", "/issues?q=is%3Aclosed+author%3A%40me"},
	} {
		rr := serve(e.h, browserRequest(http.MethodGet, tc.from, me.session, nil))
		if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != tc.to {
			t.Errorf("GET %s = %d to %q, want a redirect to %q", tc.from, rr.Code, rr.Header().Get("Location"), tc.to)
		}
	}
}

func TestAccountLists_QueryIsAppliedAndShownAsChips(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)
	ctx := context.Background()
	repo, err := e.svc.Repo.Create(ctx, me.id, me.name, "qbar_"+testutil.UniqueSuffix(t), "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatal(err)
	}
	title := "qualifier-bar-" + testutil.UniqueSuffix(t)
	if _, err := e.svc.Issue.Create(ctx, me.name, repo.Name, me.id, title, "body", ""); err != nil {
		t.Fatal(err)
	}
	get := func(q string) string {
		t.Helper()
		rr := serve(e.h, browserRequest(http.MethodGet, "/issues?q="+q, me.session, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /issues?q=%s = %d", q, rr.Code)
		}
		return rr.Body.String()
	}
	fullName := me.name + "/" + repo.Name
	for _, tc := range []struct {
		q    string
		want bool
	}{
		{"author:@me", true},
		{"", true},
		{"repo:" + fullName, true},
		{"repo:" + strings.ToUpper(fullName), true},
		{"qualifier-bar", true},
		{"author:@me+is:closed", false},
		{"assignee:@me", false},
		{"repo:" + me.name + "/other", false},
		{"nothing-matches-this", false},
	} {
		if got := strings.Contains(get(tc.q), title); got != tc.want {
			t.Errorf("q=%s lists the issue: %v, want %v", tc.q, got, tc.want)
		}
	}
	body := get("author:@me+repo:" + fullName)
	for _, chip := range []string{`aria-label="Remove filter author:@me"`, `aria-label="Remove filter repo:` + fullName + `"`} {
		if !strings.Contains(body, chip) {
			t.Errorf("no chip %s", chip)
		}
	}
	if body := get("review:approved"); !strings.Contains(body, "apply to pull requests") {
		t.Error("a pull request qualifier on the issue list should say it was ignored")
	}
}
