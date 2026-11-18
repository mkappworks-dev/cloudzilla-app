package router_test

// PackageService reads, whole-package delete and re-link, over the same environment as the registry tests.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e *registryEnv) repo(t *testing.T, owner, name string) *model.Repository {
	t.Helper()
	r, err := e.svc.Repo.Get(context.Background(), owner, name)
	if err != nil {
		t.Fatalf("get repo %s/%s: %v", owner, name, err)
	}
	return r
}

func packageNames(list []model.PackageSummary) []string {
	names := make([]string, len(list))
	for i, p := range list {
		names[i] = p.Name
	}
	slices.Sort(names)
	return names
}

func withGenericFiles(c *config.Config) { c.Packages.MaxFileBytes = 1 << 20 }

func (e *registryEnv) uploadGeneric(t *testing.T, repo *model.Repository, name string) {
	t.Helper()
	if _, err := e.svc.Package.UploadGenericFile(context.Background(), repo, e.ownerID, name, "1.0.0", "a.bin", bytes.NewReader([]byte("abc"))); err != nil {
		t.Fatalf("upload generic %s: %v", name, err)
	}
}

func TestPackageList_OnlyReadablePackagesAreListedAndAPendingOneNever(t *testing.T) {
	e := newRegistryEnv(t, withGenericFiles)
	ctx := context.Background()
	e.uploadGeneric(t, e.repo(t, e.owner, "priv"), "secretfile")
	e.uploadGeneric(t, e.repo(t, e.owner, "pub"), "pubfile")
	e.startUpload(t, e.pushAuth(t, e.ownerPAT, e.newImage), e.newImage) // a pending package of the owner

	tests := []struct {
		name   string
		viewer *int64
		want   []string
	}{
		{"owner", &e.ownerID, []string{"privimg", "pubfile", "pubimg", "secretfile"}},
		{"reader of priv", &e.readerID, []string{"privimg", "pubfile", "pubimg", "secretfile"}},
		{"stranger", &e.strangerID, []string{"pubfile", "pubimg"}},
		{"anonymous", nil, []string{"pubfile", "pubimg"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.svc.Package.ListByOwner(ctx, e.owner, tc.viewer)
			if err != nil {
				t.Fatal(err)
			}
			if names := packageNames(got); !slices.Equal(names, tc.want) {
				t.Errorf("ListByOwner = %v, want %v", names, tc.want)
			}
		})
	}

	priv := e.repo(t, e.owner, "priv")
	if got, err := e.svc.Package.ListByRepo(ctx, priv, &e.strangerID); err != nil || len(got) != 0 {
		t.Errorf("ListByRepo(priv) for a stranger = %v, %v; want nothing", packageNames(got), err)
	}
	got, err := e.svc.Package.ListByRepo(ctx, priv, &e.readerID)
	if err != nil || !slices.Equal(packageNames(got), []string{"privimg", "secretfile"}) {
		t.Errorf("ListByRepo(priv) for a reader = %v, %v", packageNames(got), err)
	}
}

func TestPackageDetail_TagsCarryDigestSizePublisherAndPullsAndUntaggedLeavesReferrersOut(t *testing.T) {
	e := newRegistryEnv(t, nil)
	ctx := context.Background()
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	tagged := e.pushBlobs(t, auth, e.pubImage, nil)
	loose := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, tagged.manifest)
	e.putManifest(auth, e.pubImage, loose.digest, mtOCIManifest, loose.manifest)
	e.pushReferrer(t, auth, e.pubImage, tagged.digest, sigArtifactType, "")              // untagged referrer
	e.pushReferrer(t, auth, e.pubImage, tagged.digest, sigArtifactType, "sha256-tagged") // tagged referrer
	testutil.Exec(t, e.db, `UPDATE package_tags SET pull_count = 7 WHERE name = 'v1'`)

	d, err := e.svc.Package.ContainerDetail(ctx, e.owner, "pubimg", &e.strangerID)
	if err != nil {
		t.Fatal(err)
	}

	var v1 *model.TagRow
	for i := range d.Tags {
		if d.Tags[i].Name == "v1" {
			v1 = &d.Tags[i]
		}
	}
	if v1 == nil || len(d.Tags) != 2 {
		t.Fatalf("tags = %+v, want v1 and sha256-tagged", d.Tags)
	}
	if v1.Digest != tagged.digest || v1.PullCount != 7 || v1.Publisher != e.owner || v1.Size != int64(len(tagged.config)+len(tagged.layer)) {
		t.Errorf("v1 = %+v, want digest %s, 7 pulls, publisher %s, size %d", *v1, tagged.digest, e.owner, len(tagged.config)+len(tagged.layer))
	}
	if len(d.Untagged) != 1 || d.Untagged[0].Digest != loose.digest {
		t.Errorf("untagged = %+v, want only %s (an untagged referrer is not listed)", d.Untagged, loose.digest)
	}
	if d.Repo == nil || d.Repo.Name != "pub" {
		t.Errorf("repo = %+v, want pub", d.Repo)
	}
}

func TestPackageDetail_AnUnreadablePackageIsNotFound(t *testing.T) {
	e := newRegistryEnv(t, nil)

	_, err := e.svc.Package.ContainerDetail(context.Background(), e.owner, "privimg", &e.strangerID)

	if !errors.Is(err, service.ErrPackageNotFound) {
		t.Errorf("err = %v, want ErrPackageNotFound", err)
	}
}

func (e *registryEnv) packageID(t *testing.T, name string) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRowContext(context.Background(), `SELECT id FROM packages WHERE name = $1 AND owner_user_id = $2 AND repo_id IS NOT NULL`, name, e.ownerID).Scan(&id); err != nil {
		t.Fatalf("package %s: %v", name, err)
	}
	return id
}

func (e *registryEnv) addCollaborator(t *testing.T, repo *model.Repository, role string) int64 {
	t.Helper()
	id := testutil.SeedUser(t, e.db, "col_"+testutil.UniqueSuffix(t))
	if err := e.svc.Repo.AddCollaborator(context.Background(), repo, e.ownerID, userName(t, e.db, id), role); err != nil {
		t.Fatal(err)
	}
	return id
}

// transfer moves repo to a new user and returns them.
func (e *registryEnv) transfer(t *testing.T, repo *model.Repository) int64 {
	t.Helper()
	ctx := context.Background()
	to := testutil.SeedUser(t, e.db, "xfer_"+testutil.UniqueSuffix(t))
	tr, err := e.svc.Repo.TransferRepo(ctx, repo, e.ownerID, userName(t, e.db, to))
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if _, err := e.svc.Repo.AcceptTransfer(ctx, tr.ID, to, tr.FullName()); err != nil {
		t.Fatalf("accept transfer: %v", err)
	}
	return to
}

func TestPackageDelete_AWriterIsRefusedAManagerAndThePathOwnerSucceed(t *testing.T) {
	e := newRegistryEnv(t, nil)
	ctx := context.Background()
	pub := e.repo(t, e.owner, "pub")
	writer, manager := e.addCollaborator(t, pub, string(model.RoleWriter)), e.addCollaborator(t, pub, string(model.RoleAdmin))
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)
	id := e.packageID(t, "pubimg")

	if err := e.svc.Package.Delete(ctx, writer, id); !errors.Is(err, service.ErrPackageForbidden) {
		t.Fatalf("writer: err = %v, want ErrPackageForbidden", err)
	}
	if err := e.svc.Package.Delete(ctx, manager, id); err != nil {
		t.Fatalf("manager: %v", err)
	}

	for _, table := range []string{"package_blobs", "package_manifests", "package_tags"} {
		if n := e.count(t, `SELECT COUNT(*) FROM `+table+` WHERE package_id = $1`, id); n != 0 {
			t.Errorf("%s still has %d rows of the deleted package", table, n)
		}
	}
	if n := e.count(t, `SELECT COUNT(*) FROM registry_blobs WHERE digest = $1`, digestOf(img.layer)); n != 1 {
		t.Errorf("blob row count = %d, want it kept for the sweep", n)
	}
	if err := e.svc.Package.Delete(ctx, manager, id); !errors.Is(err, service.ErrPackageNotFound) {
		t.Errorf("second delete: err = %v, want ErrPackageNotFound", err)
	}
}

func TestPackageDelete_ThePathOwnerMayDeleteAfterTransferringTheRepository(t *testing.T) {
	e := newRegistryEnv(t, nil)
	ctx := context.Background()
	to := e.transfer(t, e.repo(t, e.owner, "pub"))
	id := e.packageID(t, "pubimg")
	if err := e.svc.Package.Delete(ctx, e.strangerID, id); !errors.Is(err, service.ErrPackageForbidden) {
		t.Fatalf("an unrelated user: err = %v, want ErrPackageForbidden", err)
	}

	if err := e.svc.Package.Delete(ctx, e.ownerID, id); err != nil {
		t.Errorf("path owner: %v", err)
	}
	_ = to
}

func TestPackageDelete_AGenericPackageTakesItsFilesWithIt(t *testing.T) {
	e := newRegistryEnv(t, withGenericFiles)
	e.uploadGeneric(t, e.repo(t, e.owner, "pub"), "tool")
	id := e.packageID(t, "tool")
	before := e.objectCount(t)

	if err := e.svc.Package.Delete(context.Background(), e.ownerID, id); err != nil {
		t.Fatal(err)
	}

	if after := e.objectCount(t); after != before-1 {
		t.Errorf("objects = %d, want %d: the file's object goes with the package", after, before-1)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM package_files`); n != 0 {
		t.Errorf("file rows = %d, want 0", n)
	}
}

func TestPackageRelink_StaysWithinThePathOwnersRepositories(t *testing.T) {
	e := newRegistryEnv(t, nil)
	ctx := context.Background()
	priv := e.repo(t, e.owner, "priv")
	id := e.packageID(t, "pubimg")
	otherOwnersRepo, err := e.svc.Repo.Create(ctx, e.strangerID, e.stranger, "theirs", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := e.svc.Package.Relink(ctx, e.ownerID, id, otherOwnersRepo.ID); !errors.Is(err, service.ErrRelinkTarget) {
		t.Errorf("to another owner's repository: err = %v, want ErrRelinkTarget", err)
	}
	if err := e.svc.Package.Relink(ctx, e.ownerID, id, priv.ID); err != nil {
		t.Fatalf("to the path owner's own repository: %v", err)
	}

	list, err := e.svc.Package.ListByRepo(ctx, priv, &e.ownerID)
	if err != nil || !slices.Contains(packageNames(list), "pubimg") {
		t.Errorf("priv packages = %v, %v; want pubimg re-linked there", packageNames(list), err)
	}
}

func TestPackageRelink_AfterATransferThePathOwnerCanButTheNewOwnersManagerCannot(t *testing.T) {
	e := newRegistryEnv(t, nil)
	ctx := context.Background()
	to := e.transfer(t, e.repo(t, e.owner, "pub"))
	id := e.packageID(t, "pubimg")
	priv := e.repo(t, e.owner, "priv")

	if err := e.svc.Package.Relink(ctx, to, id, priv.ID); !errors.Is(err, service.ErrPackageForbidden) {
		t.Errorf("the new repository owner: err = %v, want ErrPackageForbidden", err)
	}
	if err := e.svc.Package.Relink(ctx, e.ownerID, id, priv.ID); err != nil {
		t.Errorf("the path owner: %v", err)
	}
}

func TestPackageRelink_ToAPrivateRepositoryHidesThePackageFromAnonymousPulls(t *testing.T) {
	e := newRegistryEnv(t, nil)
	if err := e.svc.Package.Relink(context.Background(), e.ownerID, e.packageID(t, "pubimg"), e.repo(t, e.owner, "priv").ID); err != nil {
		t.Fatal(err)
	}

	rr := e.token("", "repository:"+e.pubImage+":pull")

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("anonymous pull token after re-linking to a private repository: got %d, want 401", rr.Code)
	}
}
