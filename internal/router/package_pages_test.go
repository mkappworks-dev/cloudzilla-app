package router_test

// The Packages tab and the package pages, over the registry test environment.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// page GETs target as userID, or anonymously for 0.
func (e *registryEnv) page(t *testing.T, target string, userID int64) *httptest.ResponseRecorder {
	t.Helper()
	return e.browser(t, http.MethodGet, target, userID, nil)
}

func (e *registryEnv) browser(t *testing.T, method, target string, userID int64, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	session := ""
	if userID != 0 {
		session = makeJWT(t, userID, userName(t, e.db, userID))
	}
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, browserRequest(method, target, session, form))
	return rr
}

// pushTagged pushes one tag of name and returns its manifest digest.
func (e *registryEnv) pushTagged(t *testing.T, name, tag string) string {
	t.Helper()
	auth := e.pushAuth(t, e.ownerPAT, name)
	img := e.pushBlobs(t, auth, name, nil)
	if rr := e.putManifest(auth, name, tag, mtOCIManifest, img.manifest); rr.Code != http.StatusCreated {
		t.Fatalf("put manifest: %d %s", rr.Code, rr.Body)
	}
	return img.digest
}

func (e *registryEnv) tagCount(t *testing.T, name string) int {
	t.Helper()
	return e.count(t, `SELECT COUNT(*) FROM package_tags t JOIN packages p ON p.id = t.package_id WHERE p.name = $1`, name)
}

func TestPackagesTab_ListsReadablePackagesAndHidesAnUnreadableRepository(t *testing.T) {
	e := newRegistryEnv(t, withGenericFiles)
	e.uploadGeneric(t, e.repo(t, e.owner, "pub"), "pubfile")

	anon := e.page(t, "/"+e.owner+"/pub/packages", 0)
	if anon.Code != http.StatusOK {
		t.Fatalf("anonymous on a public repository: %d", anon.Code)
	}
	for _, want := range []string{"pubimg", "pubfile", `href="/` + e.owner + `/pub/packages/container/pubimg"`, `href="/` + e.owner + `/pub/packages/generic/pubfile"`} {
		if !strings.Contains(anon.Body.String(), want) {
			t.Errorf("list lacks %q", want)
		}
	}
	if strings.Contains(anon.Body.String(), "privimg") {
		t.Error("the list shows a package of another repository")
	}

	if got := e.page(t, "/"+e.owner+"/priv/packages", e.strangerID).Code; got != http.StatusNotFound {
		t.Errorf("stranger on a private repository: %d, want 404", got)
	}
	if got := e.page(t, "/"+e.owner+"/priv/packages", e.readerID).Code; got != http.StatusOK {
		t.Errorf("reader on a private repository: %d, want 200", got)
	}
}

func TestPackagesTab_FiltersByTypeAndName(t *testing.T) {
	e := newRegistryEnv(t, withGenericFiles)
	e.uploadGeneric(t, e.repo(t, e.owner, "pub"), "pubfile")

	body := e.page(t, "/"+e.owner+"/pub/packages?type=generic", 0).Body.String()
	if !strings.Contains(body, "pubfile") || strings.Contains(body, ">pubimg<") {
		t.Errorf("type=generic should list only the generic package:\n%s", body)
	}
	body = e.page(t, "/"+e.owner+"/pub/packages?q=IMG", 0).Body.String()
	if !strings.Contains(body, "pubimg") || strings.Contains(body, ">pubfile<") {
		t.Errorf("q=IMG should match case-insensitively and list only pubimg:\n%s", body)
	}
}

func TestPackagesTab_EmptyRepositoryShowsHowToPublish(t *testing.T) {
	e := newRegistryEnv(t, nil)
	testutil.Exec(t, e.db, `DELETE FROM packages WHERE repo_id = $1`, e.pubRepoID)

	body := e.page(t, "/"+e.owner+"/pub/packages", e.ownerID).Body.String()

	for _, want := range []string{"No packages published yet", "docker push", "curl -X PUT", "packages:write", "packages:read"} {
		if !strings.Contains(body, want) {
			t.Errorf("empty state lacks %q", want)
		}
	}
}

func TestPackagesTab_IsInTheRepositorySubnav(t *testing.T) {
	e := newRegistryEnv(t, nil)

	body := e.page(t, "/"+e.owner+"/pub/packages", 0).Body.String()

	if !strings.Contains(body, `href="/`+e.owner+`/pub/packages"`) || !strings.Contains(body, `aria-current="page"`) {
		t.Errorf("subnav lacks an active Packages tab")
	}
	if strings.Index(body, "/pub/releases") > strings.Index(body, `/pub/packages"`) {
		t.Error("Packages should come after Releases")
	}
}

func TestPackageDetail_ContainerShowsPullCommandTagsAndUntagged(t *testing.T) {
	e := newRegistryEnv(t, nil)
	digest := e.pushTagged(t, e.pubImage, "v1")
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	loose := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, loose.digest, mtOCIManifest, loose.manifest)

	body := e.page(t, "/"+e.owner+"/pub/packages/container/pubimg", 0).Body.String()

	for _, want := range []string{"docker pull localhost/" + e.owner + "/pubimg:v1", digest[:19], loose.digest[:19], "untagged"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
}

func TestPackageDetail_GenericShowsFilesVersionsAndDownloadCommand(t *testing.T) {
	e := newRegistryEnv(t, withGenericFiles)
	e.uploadGeneric(t, e.repo(t, e.owner, "pub"), "pubfile")

	body := e.page(t, "/"+e.owner+"/pub/packages/generic/pubfile", 0).Body.String()

	for _, want := range []string{"curl -LO http://localhost/" + e.owner + "/pub/packages/generic/pubfile/1.0.0/a.bin", "a.bin", "1.0.0"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
}

func TestPackageDetail_AnUnreadableOrUnknownPackageIsNotFound(t *testing.T) {
	e := newRegistryEnv(t, nil)

	tests := []struct {
		name   string
		target string
		viewer int64
	}{
		{"stranger on a private package", "/" + e.owner + "/priv/packages/container/privimg", e.strangerID},
		{"anonymous on a private package", "/" + e.owner + "/priv/packages/container/privimg", 0},
		{"unknown name", "/" + e.owner + "/pub/packages/container/nothing", e.ownerID},
		{"package of another repository", "/" + e.owner + "/pub/packages/container/privimg", e.ownerID},
		{"unknown type", "/" + e.owner + "/pub/packages/maven/pubimg", e.ownerID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := e.page(t, tc.target, tc.viewer).Code; got != http.StatusNotFound {
				t.Errorf("status = %d, want 404", got)
			}
		})
	}
}

func TestPackageDetail_ControlsFollowTheViewersRole(t *testing.T) {
	e := newRegistryEnv(t, nil)
	pub := e.repo(t, e.owner, "pub")
	e.pushTagged(t, e.pubImage, "v1")
	writer, manager := e.addCollaborator(t, pub, string(model.RoleWriter)), e.addCollaborator(t, pub, string(model.RoleAdmin))
	e.addCollaborator(t, pub, string(model.RoleReader))
	reader := e.addCollaborator(t, pub, string(model.RoleReader))
	target := "/" + e.owner + "/pub/packages/container/pubimg"

	tests := []struct {
		name                  string
		viewer                int64
		tag, whole, relinkBox bool
	}{
		{"anonymous", 0, false, false, false},
		{"reader", reader, false, false, false},
		{"writer", writer, true, false, false},
		{"manager with no other repository to move it to", manager, true, true, false},
		{"path owner", e.ownerID, true, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := e.page(t, target, tc.viewer).Body.String()
			for action, want := range map[string]bool{"delete-tag": tc.tag, "delete-package": tc.whole, "relink": tc.relinkBox} {
				if got := strings.Contains(body, `data-pkg-action="`+action+`"`); got != want {
					t.Errorf("%s control present = %v, want %v", action, got, want)
				}
			}
		})
	}
}

func TestPackageDetail_AfterATransferTheMismatchNoteShowsAndThePathOwnerStillManagesIt(t *testing.T) {
	e := newRegistryEnv(t, nil)
	e.pushTagged(t, e.pubImage, "v1")
	to := e.transfer(t, e.repo(t, e.owner, "pub"))
	target := "/" + userName(t, e.db, to) + "/pub/packages/container/pubimg"

	body := e.page(t, target, e.ownerID).Body.String()

	if !strings.Contains(body, `data-pkg-note="mismatch"`) {
		t.Error("no mismatch note after a transfer")
	}
	if !strings.Contains(body, "docker pull localhost/"+e.owner+"/pubimg:v1") {
		t.Error("the pull command should keep the path owner's name")
	}
	for _, action := range []string{"delete-package", "relink"} {
		if !strings.Contains(body, `data-pkg-action="`+action+`"`) {
			t.Errorf("path owner lacks %s", action)
		}
	}
	if strings.Contains(e.page(t, "/"+e.owner+"/pub/packages/container/pubimg", e.ownerID).Body.String(), "data-pkg-note") {
		t.Error("a package at its own owner shows a mismatch note")
	}
}

func TestPackageDetail_ReadmeCardIsTheLinkedRepositorysDefaultBranchReadmeOrAbsent(t *testing.T) {
	e := newRegistryEnv(t, nil)
	ctx := context.Background()
	docs, err := e.svc.Repo.Create(ctx, e.ownerID, e.owner, "docs", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, e.db, `INSERT INTO packages (owner_user_id, type, name, repo_id, creator_id) VALUES ($1, 'container', 'docsimg', $2, $1)`, e.ownerID, docs.ID)

	with := e.page(t, "/"+e.owner+"/docs/packages/container/docsimg", 0).Body.String()
	without := e.page(t, "/"+e.owner+"/pub/packages/container/pubimg", 0).Body.String()

	if !strings.Contains(with, `data-pkg-card="readme"`) || !strings.Contains(with, "README.md") {
		t.Error("no README card for a repository with a README")
	}
	if strings.Contains(without, `data-pkg-card="readme"`) {
		t.Error("README card shown for a repository without one")
	}
}

func TestPackageDetail_StorageShowsUsageAgainstThePackageQuota(t *testing.T) {
	e := newRegistryEnv(t, func(c *config.Config) { c.Quota.User.PackageBytes = 10 << 20 })
	e.pushTagged(t, e.pubImage, "v1")

	body := e.page(t, "/"+e.owner+"/pub/packages/container/pubimg", e.ownerID).Body.String()

	if !strings.Contains(body, "10 MiB package quota") {
		t.Errorf("storage card lacks the quota line:\n%s", body)
	}
}

func TestPackageForms_DeleteTagNeedsWriteAndKeepsTheOtherTags(t *testing.T) {
	e := newRegistryEnv(t, nil)
	pub := e.repo(t, e.owner, "pub")
	e.pushTagged(t, e.pubImage, "v1")
	e.pushTagged(t, e.pubImage, "v2")
	reader := e.addCollaborator(t, pub, string(model.RoleReader))
	id := strconv.FormatInt(e.packageID(t, "pubimg"), 10)
	form := url.Values{"package_id": {id}, "ref": {"v1"}}
	target := "/" + e.owner + "/pub/packages/tags/delete"

	if got := e.browser(t, http.MethodPost, target, reader, form).Code; got != http.StatusForbidden {
		t.Fatalf("reader: %d, want 403", got)
	}
	if got := e.tagCount(t, "pubimg"); got != 2 {
		t.Fatalf("a refused delete removed a tag: %d left", got)
	}
	rr := e.browser(t, http.MethodPost, target, e.ownerID, form)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("owner: %d %s, want 303", rr.Code, rr.Body)
	}
	if got := e.tagCount(t, "pubimg"); got != 1 {
		t.Errorf("tags left = %d, want 1", got)
	}
}

func TestPackageForms_DeleteVersionOfAGenericPackage(t *testing.T) {
	e := newRegistryEnv(t, withGenericFiles)
	pub := e.repo(t, e.owner, "pub")
	e.uploadGeneric(t, pub, "pubfile")
	reader := e.addCollaborator(t, pub, string(model.RoleReader))
	id := strconv.FormatInt(e.packageID(t, "pubfile"), 10)
	form := url.Values{"package_id": {id}, "version": {"1.0.0"}}
	target := "/" + e.owner + "/pub/packages/versions/delete"

	if got := e.browser(t, http.MethodPost, target, reader, form).Code; got != http.StatusForbidden {
		t.Fatalf("reader: %d, want 403", got)
	}
	if rr := e.browser(t, http.MethodPost, target, e.ownerID, form); rr.Code != http.StatusSeeOther {
		t.Fatalf("owner: %d %s, want 303", rr.Code, rr.Body)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM packages WHERE name = 'pubfile'`); got != 0 {
		t.Error("deleting the last version should free the package")
	}
}

func TestPackageForms_DeletePackageNeedsManageAndTheTypedName(t *testing.T) {
	e := newRegistryEnv(t, nil)
	pub := e.repo(t, e.owner, "pub")
	e.pushTagged(t, e.pubImage, "v1")
	writer := e.addCollaborator(t, pub, string(model.RoleWriter))
	id := strconv.FormatInt(e.packageID(t, "pubimg"), 10)
	target := "/" + e.owner + "/pub/packages/delete"
	exists := func() int { return e.count(t, `SELECT COUNT(*) FROM packages WHERE name = 'pubimg'`) }

	if got := e.browser(t, http.MethodPost, target, writer, url.Values{"package_id": {id}, "confirm_name": {"pubimg"}}).Code; got != http.StatusForbidden {
		t.Errorf("writer: %d, want 403", got)
	}
	if got := e.browser(t, http.MethodPost, target, e.ownerID, url.Values{"package_id": {id}, "confirm_name": {"pub"}}).Code; got != http.StatusUnprocessableEntity {
		t.Errorf("wrong name: %d, want 422", got)
	}
	if exists() != 1 {
		t.Fatal("a refused delete removed the package")
	}
	rr := e.browser(t, http.MethodPost, target, e.ownerID, url.Values{"package_id": {id}, "confirm_name": {"pubimg"}})
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/"+e.owner+"/pub/packages" {
		t.Fatalf("owner: %d to %q, want 303 to the list", rr.Code, rr.Header().Get("Location"))
	}
	if exists() != 0 {
		t.Error("the package is still there")
	}
}

func TestPackageForms_PackageOfAnotherRepositoryIsNotFoundHere(t *testing.T) {
	e := newRegistryEnv(t, nil)
	id := strconv.FormatInt(e.packageID(t, "privimg"), 10)

	rr := e.browser(t, http.MethodPost, "/"+e.owner+"/pub/packages/delete", e.ownerID, url.Values{"package_id": {id}, "confirm_name": {"privimg"}})

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestPackageForms_RelinkMovesThePackageToAnotherRepositoryOfThePathOwner(t *testing.T) {
	e := newRegistryEnv(t, nil)
	e.pushTagged(t, e.pubImage, "v1")
	to := e.transfer(t, e.repo(t, e.owner, "pub"))
	mine, err := e.svc.Repo.Create(context.Background(), e.ownerID, e.owner, "home", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(e.packageID(t, "pubimg"), 10)
	target := "/" + userName(t, e.db, to) + "/pub/packages/relink"

	if got := e.browser(t, http.MethodPost, target, e.ownerID, url.Values{"package_id": {id}, "repo": {"nothere"}}).Code; got != http.StatusUnprocessableEntity {
		t.Errorf("unknown target: %d, want 422", got)
	}
	rr := e.browser(t, http.MethodPost, target, e.ownerID, url.Values{"package_id": {id}, "repo": {"home"}})
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/"+e.owner+"/home/packages/container/pubimg" {
		t.Fatalf("relink: %d to %q", rr.Code, rr.Header().Get("Location"))
	}
	if got := e.count(t, `SELECT COUNT(*) FROM packages WHERE name = 'pubimg' AND repo_id = $1`, mine.ID); got != 1 {
		t.Error("package not linked to the new repository")
	}
}
