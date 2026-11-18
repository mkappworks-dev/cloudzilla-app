package router_test

// The owner Packages tab at /{owner}?tab=packages, its path-owner forms, and package storage in settings.

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// ownerOrg is an organization e.owner owns, with a public and a private repository and a container package in each.
type ownerOrg struct {
	name                 string
	id                   int64
	pubRepo, privRepo    *model.Repository
	pubPackage, privPack string
}

func (e *registryEnv) newOrg(t *testing.T) ownerOrg {
	t.Helper()
	ctx := context.Background()
	org, err := e.svc.Org.Create(ctx, e.ownerID, "testorg_"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, org.ID)
	o := ownerOrg{name: org.Name, id: org.ID, pubPackage: "orgpubimg", privPack: "orgprivimg"}
	if o.pubRepo, err = e.svc.Org.CreateRepo(ctx, org.ID, e.ownerID, "orgpub", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatal(err)
	}
	if o.privRepo, err = e.svc.Org.CreateRepo(ctx, org.ID, e.ownerID, "orgpriv", "", true, service.RepoInitOptions{}); err != nil {
		t.Fatal(err)
	}
	for name, repoID := range map[string]int64{o.pubPackage: o.pubRepo.ID, o.privPack: o.privRepo.ID} {
		testutil.Exec(t, e.db, `INSERT INTO packages (owner_org_id, type, name, repo_id, creator_id) VALUES ($1, 'container', $2, $3, $4)`,
			org.ID, name, repoID, e.ownerID)
	}
	return o
}

func hasRow(body, name string) bool { return strings.Contains(body, `data-pkg-row="`+name+`"`) }

func hasOwnerAction(body, action string) bool {
	return strings.Contains(body, `data-pkg-action="`+action+`"`)
}

func TestOwnerPackagesTab_ListsOnlyPackagesTheViewerCanRead(t *testing.T) {
	e := newRegistryEnv(t, nil)
	org := e.newOrg(t)
	if err := e.svc.Repo.AddCollaborator(context.Background(), org.privRepo, e.ownerID, e.reader, string(model.RoleReader)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		target       string
		viewer       int64
		want, absent []string
	}{
		{"anonymous on a user", "/" + e.owner + "?tab=packages", 0, []string{"pubimg"}, []string{"privimg"}},
		{"stranger on a user", "/" + e.owner + "?tab=packages", e.strangerID, []string{"pubimg"}, []string{"privimg"}},
		{"reader of the private repository", "/" + e.owner + "?tab=packages", e.readerID, []string{"pubimg", "privimg"}, nil},
		{"owner on a user", "/" + e.owner + "?tab=packages", e.ownerID, []string{"pubimg", "privimg"}, nil},
		{"anonymous on an organization", "/" + org.name + "?tab=packages", 0, []string{org.pubPackage}, []string{org.privPack}},
		{"stranger on an organization", "/" + org.name + "?tab=packages", e.strangerID, []string{org.pubPackage}, []string{org.privPack}},
		{"reader of the private org repository", "/" + org.name + "?tab=packages", e.readerID, []string{org.pubPackage, org.privPack}, nil},
		{"org owner", "/" + org.name + "?tab=packages", e.ownerID, []string{org.pubPackage, org.privPack}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := e.page(t, tc.target, tc.viewer)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d", rr.Code)
			}
			for _, n := range tc.want {
				if !hasRow(rr.Body.String(), n) {
					t.Errorf("no row for %s", n)
				}
			}
			for _, n := range tc.absent {
				if strings.Contains(rr.Body.String(), n) {
					t.Errorf("the page mentions %s", n)
				}
			}
		})
	}
}

func TestOwnerPackagesTab_FiltersByTypeAndRepository(t *testing.T) {
	e := newRegistryEnv(t, withGenericFiles)
	e.uploadGeneric(t, e.repo(t, e.owner, "pub"), "pubfile")
	base := "/" + e.owner + "?tab=packages"

	body := e.page(t, base+"&type=generic", e.ownerID).Body.String()
	if !hasRow(body, "pubfile") || hasRow(body, "pubimg") || hasRow(body, "privimg") {
		t.Errorf("type=generic should list only pubfile:\n%s", body)
	}
	body = e.page(t, base+"&repo=priv", e.ownerID).Body.String()
	if !hasRow(body, "privimg") || hasRow(body, "pubimg") || hasRow(body, "pubfile") {
		t.Errorf("repo=priv should list only privimg:\n%s", body)
	}
}

func TestOwnerPackagesTab_RowsNameTheirRepositoryAndMarkAPrivateOne(t *testing.T) {
	e := newRegistryEnv(t, nil)

	body := e.page(t, "/"+e.owner+"?tab=packages", e.ownerID).Body.String()

	for _, want := range []string{`href="/` + e.owner + `/priv/packages/container/privimg"`, `href="/` + e.owner + `/priv"`, "Private"} {
		if !strings.Contains(body, want) {
			t.Errorf("tab lacks %q", want)
		}
	}
}

func TestOwnerPackagesTab_IsInTheProfileNavOfAUser(t *testing.T) {
	e := newRegistryEnv(t, nil)

	body := e.page(t, "/"+e.owner, e.ownerID).Body.String()

	if !strings.Contains(body, `href="/`+e.owner+`?tab=packages"`) {
		t.Error("the profile has no Packages tab")
	}
}

func TestOwnerPackagesTab_ManageControlsAreThePathOwnersAlone(t *testing.T) {
	e := newRegistryEnv(t, nil)
	org := e.newOrg(t)
	member := e.addCollaborator(t, org.pubRepo, string(model.RoleAdmin))

	tests := []struct {
		name   string
		target string
		viewer int64
		want   bool
	}{
		{"anonymous", "/" + e.owner + "?tab=packages", 0, false},
		{"stranger", "/" + e.owner + "?tab=packages", e.strangerID, false},
		{"reader", "/" + e.owner + "?tab=packages", e.readerID, false},
		{"the user", "/" + e.owner + "?tab=packages", e.ownerID, true},
		{"anonymous on an org", "/" + org.name + "?tab=packages", 0, false},
		{"org repo manager who is not an org owner", "/" + org.name + "?tab=packages", member, false},
		{"org owner", "/" + org.name + "?tab=packages", e.ownerID, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := e.page(t, tc.target, tc.viewer).Body.String()
			for _, action := range []string{"owner-delete", "owner-relink"} {
				if got := hasOwnerAction(body, action); got != tc.want {
					t.Errorf("%s control present = %v, want %v", action, got, tc.want)
				}
			}
		})
	}
}

func TestOwnerPackagesTab_ThePathOwnerStillSeesAPackageInAPrivateRepositoryTransferredAway(t *testing.T) {
	e := newRegistryEnv(t, nil)
	to := e.transfer(t, e.repo(t, e.owner, "priv"))

	owner := e.page(t, "/"+e.owner+"?tab=packages", e.ownerID).Body.String()
	stranger := e.page(t, "/"+e.owner+"?tab=packages", e.strangerID).Body.String()

	if !hasRow(owner, "privimg") || !hasOwnerAction(owner, "owner-delete") || !hasOwnerAction(owner, "owner-relink") {
		t.Errorf("the path owner can't manage a package in a repository they can't read:\n%s", owner)
	}
	if strings.Contains(owner, "/"+userName(t, e.db, to)+"/priv") {
		t.Error("the row links to a repository the path owner can't read")
	}
	if hasRow(stranger, "privimg") {
		t.Error("a stranger sees the package")
	}
}

func TestOwnerPackageForms_DeleteNeedsThePathOwnerAndTheTypedName(t *testing.T) {
	e := newRegistryEnv(t, nil)
	org := e.newOrg(t)
	id := strconv.FormatInt(e.packageID(t, "privimg"), 10)
	target := "/" + e.owner + "/-/packages/delete"
	exists := func() int { return e.count(t, `SELECT COUNT(*) FROM packages WHERE name = 'privimg'`) }
	form := func(name string) url.Values { return url.Values{"package_id": {id}, "confirm_name": {name}} }

	for name, viewer := range map[string]int64{"stranger": e.strangerID, "reader": e.readerID} {
		if got := e.browser(t, http.MethodPost, target, viewer, form("privimg")).Code; got != http.StatusForbidden && got != http.StatusNotFound {
			t.Errorf("%s: %d, want 403 or 404", name, got)
		}
	}
	if got := e.browser(t, http.MethodPost, target, e.ownerID, form("priv")).Code; got != http.StatusUnprocessableEntity {
		t.Errorf("wrong name: %d, want 422", got)
	}
	if got := e.browser(t, http.MethodPost, "/"+org.name+"/-/packages/delete", e.ownerID, form("privimg")).Code; got != http.StatusNotFound {
		t.Errorf("a package of another path owner: %d, want 404", got)
	}
	if exists() != 1 {
		t.Fatal("a refused delete removed the package")
	}
	rr := e.browser(t, http.MethodPost, target, e.ownerID, form("privimg"))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/"+e.owner+"?tab=packages" {
		t.Fatalf("owner: %d to %q, want 303 to the tab", rr.Code, rr.Header().Get("Location"))
	}
	if exists() != 0 {
		t.Error("the package is still there")
	}
}

func TestOwnerPackageForms_AnOrgOwnerDeletesButANonOwnerManagerCannot(t *testing.T) {
	e := newRegistryEnv(t, nil)
	org := e.newOrg(t)
	member := e.addCollaborator(t, org.pubRepo, string(model.RoleAdmin))
	var id int64
	if err := e.db.QueryRowContext(context.Background(), `SELECT id FROM packages WHERE name = $1`, org.pubPackage).Scan(&id); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"package_id": {strconv.FormatInt(id, 10)}, "confirm_name": {org.pubPackage}}
	target := "/" + org.name + "/-/packages/delete"

	if got := e.browser(t, http.MethodPost, target, member, form).Code; got != http.StatusForbidden {
		t.Errorf("non-owner manager: %d, want 403", got)
	}
	if rr := e.browser(t, http.MethodPost, target, e.ownerID, form); rr.Code != http.StatusSeeOther {
		t.Fatalf("org owner: %d %s, want 303", rr.Code, rr.Body)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM packages WHERE id = $1`, id); got != 0 {
		t.Error("the package is still there")
	}
}

func TestOwnerPackageForms_RelinkMovesAPackageOutOfARepositoryThePathOwnerCannotRead(t *testing.T) {
	e := newRegistryEnv(t, nil)
	e.transfer(t, e.repo(t, e.owner, "priv"))
	home, err := e.svc.Repo.Create(context.Background(), e.ownerID, e.owner, "home", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(e.packageID(t, "privimg"), 10)
	target := "/" + e.owner + "/-/packages/relink"

	if got := e.browser(t, http.MethodPost, target, e.strangerID, url.Values{"package_id": {id}, "repo": {"home"}}).Code; got != http.StatusForbidden && got != http.StatusNotFound {
		t.Errorf("stranger: %d, want 403 or 404", got)
	}
	if got := e.browser(t, http.MethodPost, target, e.ownerID, url.Values{"package_id": {id}, "repo": {"nothere"}}).Code; got != http.StatusUnprocessableEntity {
		t.Errorf("unknown target: %d, want 422", got)
	}
	rr := e.browser(t, http.MethodPost, target, e.ownerID, url.Values{"package_id": {id}, "repo": {"home"}})
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/"+e.owner+"?tab=packages" {
		t.Fatalf("relink: %d to %q", rr.Code, rr.Header().Get("Location"))
	}
	if got := e.count(t, `SELECT COUNT(*) FROM packages WHERE name = 'privimg' AND repo_id = $1`, home.ID); got != 1 {
		t.Error("package not linked to the new repository")
	}
}

func TestOwnerPackageForms_RequireSignIn(t *testing.T) {
	e := newRegistryEnv(t, nil)

	rr := e.browser(t, http.MethodPost, "/"+e.owner+"/-/packages/delete", 0, url.Values{"package_id": {"1"}})

	if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusSeeOther {
		t.Errorf("anonymous: %d, want 401 or a redirect to sign in", rr.Code)
	}
}

func TestSettings_ShowPackageStorageWithTheQuotaOnlyWhenOneIsSet(t *testing.T) {
	tests := []struct {
		name     string
		quota    int64
		wantLine string
	}{
		{"no quota shows usage alone", 0, "Packages 0 B"},
		{"a quota shows usage against it", 10 << 20, "Packages 0 B of 10 MiB"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newRegistryEnv(t, func(c *config.Config) { c.Quota.User.PackageBytes, c.Quota.Org.PackageBytes = tc.quota, tc.quota })
			org := e.newOrg(t)
			for name, target := range map[string]string{"account": "/settings", "organization": "/orgs/" + org.name + "/settings"} {
				body := e.page(t, target, e.ownerID).Body.String()
				i := strings.Index(body, `id="package-usage"`)
				if i < 0 {
					t.Fatalf("%s settings have no package storage line", name)
				}
				line := body[i:min(len(body), i+200)]
				if !strings.Contains(line, tc.wantLine+"<") {
					t.Errorf("%s settings line = %q, want %q", name, line, tc.wantLine)
				}
			}
		})
	}
}
