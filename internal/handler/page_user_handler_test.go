package handler_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newProfileTestRouter(t *testing.T, db *sql.DB) (http.Handler, *service.Services) {
	t.Helper()
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg)
	r := chi.NewRouter()
	r.Get("/{owner}", handler.New(svc, cfg).PageUser)
	return middleware.OptionalAuth(testJWTSecret, testCookieName, nil, nil)(r), svc
}

func getProfile(t *testing.T, router http.Handler, path, token string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d", path, rr.Code)
	}
	return rr.Body.String()
}

// Matches the legend of components.LanguagesBar only; repo cards render languages differently.
func langBarLists(body, lang string) bool {
	return strings.Contains(body, `<span class="text-foreground">`+lang+`</span>`)
}

func TestPageUser_PrivatePinHiddenFromVisitors(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	visitorID := testutil.SeedUser(t, db, suffix+"_visitor")
	secret := testutil.SeedRepo(t, db, ownerID, owner, suffix+"_secret")
	if _, err := db.ExecContext(ctx, `UPDATE repositories SET private = TRUE WHERE id = $1`, secret); err != nil {
		t.Fatalf("make private: %v", err)
	}
	router, svc := newProfileTestRouter(t, db)

	if err := svc.User.PinRepo(ctx, ownerID, secret); err != nil {
		t.Fatalf("pin: %v", err)
	}

	pinnedCard := `href="/` + owner + `/testrepo_` + suffix + `_secret"`
	for _, tc := range []struct {
		name  string
		token string
		want  bool
	}{
		{"anonymous", "", false},
		{"visitor", makeIssueJWT(t, visitorID, owner+"_visitor"), false},
		{"owner", makeIssueJWT(t, ownerID, owner), true},
	} {
		body := getProfile(t, router, "/"+owner, tc.token)
		if got := strings.Contains(body, pinnedCard); got != tc.want {
			t.Errorf("%s: private pinned card rendered = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPageUser_LanguagesCountOnlyReposTheViewerCanRead(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	visitorID := testutil.SeedUser(t, db, suffix+"_visitor")
	router, svc := newProfileTestRouter(t, db)

	for _, r := range []struct {
		name, file string
		private    bool
	}{{"web", "main.go", false}, {"engine", "lib.rs", true}} {
		if _, err := svc.Repo.Create(ctx, owner, r.name, "", r.private, service.RepoInitOptions{}); err != nil {
			t.Fatalf("create %s: %v", r.name, err)
		}
		if err := svc.Code.CommitFile(owner, r.name, "main", r.file, []byte("code\n"), owner, owner+"@test.invalid", "seed"); err != nil {
			t.Fatalf("commit to %s: %v", r.name, err)
		}
	}

	for _, tc := range []struct {
		name     string
		token    string
		wantRust bool
	}{
		{"anonymous", "", false},
		{"visitor", makeIssueJWT(t, visitorID, owner+"_visitor"), false},
		{"owner", makeIssueJWT(t, ownerID, owner), true},
	} {
		body := getProfile(t, router, "/"+owner, tc.token)
		if !langBarLists(body, "Go") {
			t.Errorf("%s: top languages miss the public repo's Go", tc.name)
		}
		if got := langBarLists(body, "Rust"); got != tc.wantRust {
			t.Errorf("%s: top languages list the private repo's Rust = %v, want %v", tc.name, got, tc.wantRust)
		}
	}
}

func seedProfileOrg(t *testing.T, db *sql.DB, svc *service.Services, ownerID int64, name string) *model.Organization {
	t.Helper()
	org, err := svc.Org.Create(context.Background(), ownerID, name, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), `DELETE FROM organizations WHERE id = $1`, org.ID) })
	return org
}

func TestPageOrg_LanguagesCountOnlyReposTheViewerCanRead(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	visitorID := testutil.SeedUser(t, db, suffix+"_visitor")
	router, svc := newProfileTestRouter(t, db)
	org := seedProfileOrg(t, db, svc, ownerID, "testorg_"+suffix)

	for _, r := range []struct {
		name, lang string
		private    bool
	}{{"web", "Go", false}, {"engine", "Rust", true}} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO repositories (owner_id, owner_name, org_id, name, description, private, default_branch, primary_language)
			 VALUES ($1, $2, $3, $4, '', $5, 'main', $6)`,
			ownerID, org.Name, org.ID, r.name, r.private, r.lang,
		); err != nil {
			t.Fatalf("insert org repo %s: %v", r.name, err)
		}
	}

	for _, tc := range []struct {
		name     string
		token    string
		wantRust bool
	}{
		{"anonymous", "", false},
		{"visitor", makeIssueJWT(t, visitorID, "testuser_"+suffix+"_visitor"), false},
		{"owner", makeIssueJWT(t, ownerID, "testuser_"+suffix), true},
	} {
		body := getProfile(t, router, "/"+org.Name, tc.token)
		if !langBarLists(body, "Go") {
			t.Errorf("%s: top languages miss the public repo's Go", tc.name)
		}
		if got := langBarLists(body, "Rust"); got != tc.wantRust {
			t.Errorf("%s: top languages list the private repo's Rust = %v, want %v", tc.name, got, tc.wantRust)
		}
	}
}

func TestPageOrg_PeopleTabListsEveryMemberWithoutReadme(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	router, svc := newProfileTestRouter(t, db)
	org := seedProfileOrg(t, db, svc, ownerID, "testorg_"+suffix)

	members := []string{"testuser_" + suffix}
	for i := range 13 {
		memberSuffix := fmt.Sprintf("%s_m%d", suffix, i)
		id := testutil.SeedUser(t, db, memberSuffix)
		if err := svc.Org.AddMember(ctx, org.ID, ownerID, id, model.OrgRoleMember); err != nil {
			t.Fatalf("add member %d: %v", i, err)
		}
		members = append(members, "testuser_"+memberSuffix)
	}
	if _, err := svc.Org.CreateRepo(ctx, org.ID, ownerID, org.Name, "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create profile repo: %v", err)
	}

	overview := getProfile(t, router, "/"+org.Name, "")
	if !strings.Contains(overview, `href="/`+org.Name+`?tab=people"`) {
		t.Error("overview has no View all link to the people tab")
	}
	if !strings.Contains(overview, `id="org-readme-heading"`) {
		t.Error("overview does not render the org README")
	}

	people := getProfile(t, router, "/"+org.Name+"?tab=people", "")
	start := strings.Index(people, `id="all-people-heading"`)
	if start < 0 {
		t.Fatal("people tab has no all-people list")
	}
	list := people[start:]
	list = list[:strings.Index(list, "</section>")]
	for _, m := range members {
		if !strings.Contains(list, `href="/`+m+`"`) {
			t.Errorf("people list misses %s", m)
		}
	}
	if strings.Contains(people, `id="org-readme-heading"`) {
		t.Error("people tab still renders the org README")
	}
}
