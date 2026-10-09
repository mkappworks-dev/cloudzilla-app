package router_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func suggest(h http.Handler, q, session string) *httptest.ResponseRecorder {
	return serve(h, browserRequest(http.MethodGet, "/search/suggest?q="+url.QueryEscape(q), session, nil))
}

// A suggestion links to a page; showing a private repo's name to someone who
// would get a 404 there leaks that it exists.
func TestSearchSuggest_PrivateRepoOnlyForThoseWhoCanRead(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	word := "sgrt" + strings.ReplaceAll(suffix, "_", "x")

	ownerName := "sgrtowner_" + suffix
	ownerID := testutil.SeedUser(t, db, ownerName)
	t.Cleanup(func() { testutil.DeleteUsers(t, db, ownerID) })
	owner := "testuser_" + ownerName
	for name, private := range map[string]bool{word + "pub": false, word + "priv": true} {
		testutil.Exec(t, db,
			`INSERT INTO repositories (owner_id, owner_name, name, private) VALUES ($1, $2, $3, $4)`,
			ownerID, owner, name, private)
	}
	session := superadminJWT(t, ownerID, owner)

	anon := suggest(h, word, "")
	if anon.Code != http.StatusOK {
		t.Fatalf("anonymous: status %d", anon.Code)
	}
	if body := anon.Body.String(); !strings.Contains(body, word+"pub") || strings.Contains(body, word+"priv") {
		t.Errorf("anonymous should see only the public repo:\n%s", body)
	}

	own := suggest(h, word, session)
	body := own.Body.String()
	if !strings.Contains(body, word+"pub") || !strings.Contains(body, word+"priv") {
		t.Errorf("owner should see both repos:\n%s", body)
	}
	if !strings.Contains(body, `href="/`+owner+`/`+word+`priv"`) {
		t.Errorf("repo row should link to /owner/name:\n%s", body)
	}
}

func TestSearchSuggest_ResponseShape(t *testing.T) {
	h, _, _ := newVerificationRouter(t, config.SMTPConfig{})

	t.Run("under two characters is empty", func(t *testing.T) {
		rr := suggest(h, "a", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d", rr.Code)
		}
		if body := rr.Body.String(); strings.Contains(body, `role="option"`) || strings.Contains(body, "Search for") {
			t.Errorf("want an empty fragment, got:\n%s", body)
		}
	})

	t.Run("varies by viewer, so never cached", func(t *testing.T) {
		rr := suggest(h, "ab", "")
		if got := rr.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Errorf("Cache-Control = %q", got)
		}
	})

	t.Run("search row escapes the query", func(t *testing.T) {
		rr := suggest(h, `<script>alert(1)</script>"`, "")
		body := rr.Body.String()
		if strings.Contains(body, "<script>alert") {
			t.Errorf("query reached the page unescaped:\n%s", body)
		}
		if !strings.Contains(body, `href="/search?q=%3Cscript%3Ealert%281%29%3C%2Fscript%3E%22"`) {
			t.Errorf("Search-for row should link to the URL-encoded query:\n%s", body)
		}
	})
}
