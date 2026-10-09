package router_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSearchPage_GroupsLinkToTheirItems(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	word := "srpg" + strings.ReplaceAll(suffix, "_", "x")

	ownerName := "srpgowner_" + suffix
	ownerID := testutil.SeedUser(t, db, ownerName)
	t.Cleanup(func() { testutil.DeleteUsers(t, db, ownerID) })
	owner := "testuser_" + ownerName

	var repoID, orgID int64
	for name, private := range map[string]bool{word + "pub": false, word + "priv": true} {
		var id int64
		if err := db.QueryRowContext(t.Context(),
			`INSERT INTO repositories (owner_id, owner_name, name, private) VALUES ($1, $2, $3, $4) RETURNING id`,
			ownerID, owner, name, private,
		).Scan(&id); err != nil {
			t.Fatalf("insert repo: %v", err)
		}
		if !private {
			repoID = id
		}
	}
	if err := db.QueryRowContext(t.Context(),
		`INSERT INTO organizations (name, display_name) VALUES ($1, $2) RETURNING id`, word+"org", "Org "+word,
	).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, orgID) })
	testutil.Exec(t, db,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility) VALUES ($1, 3, $2, $3, '', 'open', 'public')`,
		repoID, ownerID, word+" issue")
	testutil.Exec(t, db,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, body, head_branch, base_branch) VALUES ($1, 9, $2, $3, '', 'f', 'main')`,
		repoID, ownerID, word+" pull")

	page := func(q, typ string) string {
		t.Helper()
		rr := serve(h, browserRequest(http.MethodGet, "/search?q="+url.QueryEscape(q)+"&type="+typ, "", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d", rr.Code)
		}
		return rr.Body.String()
	}

	body := page(word, "all")
	for name, want := range map[string]string{
		"repo":  `href="/` + owner + `/` + word + `pub"`,
		"org":   `href="/` + word + `org"`,
		"issue": `href="/` + owner + `/` + word + `pub/issues/3"`,
		"pull":  `href="/` + owner + `/` + word + `pub/pulls/9"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s: missing %s", name, want)
		}
	}
	if strings.Contains(body, word+"priv") {
		t.Error("anonymous viewer was shown the private repo")
	}
	if !strings.Contains(body, "Organizations") {
		t.Error("missing the Organizations group")
	}

	orgsOnly := page(word, "orgs")
	if !strings.Contains(orgsOnly, `href="/`+word+`org"`) || strings.Contains(orgsOnly, `/issues/3"`) {
		t.Errorf("type=orgs should show only organizations:\n%s", orgsOnly)
	}

	if body := page(word+"nomatch", "all"); !strings.Contains(body, "No results found") {
		t.Error("expected the empty state")
	}
}

func TestSearchPage_EmptyQueryRenders(t *testing.T) {
	h, _, _ := newVerificationRouter(t, config.SMTPConfig{})
	rr := serve(h, browserRequest(http.MethodGet, "/search", "", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Enter a search query") {
		t.Errorf("status %d, body:\n%s", rr.Code, rr.Body.String())
	}
}
