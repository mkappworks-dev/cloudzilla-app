package router_test

// The package JSON endpoints under /api/repos/{owner}/{repo}/packages that the wire protocol does not give.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e *registryEnv) api(method, target, pat string, body any) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, rd)
	if pat != "" {
		req.Header.Set("Authorization", "Bearer "+pat)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return serve(e.h, req)
}

func TestPackageAPI_ListIncludesContainersAndHidesAPrivateRepositoryFromStrangers(t *testing.T) {
	e := newRegistryEnv(t, withGenericFiles)
	e.uploadGeneric(t, e.repo(t, e.owner, "pub"), "tool")
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)

	rr := e.api(http.MethodGet, "/api/repos/"+e.owner+"/pub/packages", "", nil)

	var got []struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		TagCount int    `json:"tag_count"`
		Size     int64  `json:"size"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || rr.Code != http.StatusOK {
		t.Fatalf("list: %d %s (%v)", rr.Code, rr.Body, err)
	}
	byName := map[string]int{}
	for i, p := range got {
		byName[p.Name] = i
	}
	img1, ok := byName["pubimg"]
	if !ok || got[img1].Type != model.PackageTypeContainer || got[img1].TagCount != 1 || got[img1].Size != int64(len(img.config)+len(img.layer)) {
		t.Errorf("list = %+v, want pubimg as a container with one tag and the image's size", got)
	}
	if _, ok := byName["tool"]; !ok {
		t.Errorf("list = %+v, want the generic package too", got)
	}
	if rr := e.api(http.MethodGet, "/api/repos/"+e.owner+"/priv/packages", e.strangerPAT, nil); rr.Code != http.StatusNotFound {
		t.Errorf("a stranger listing a private repository's packages: got %d, want 404", rr.Code)
	}
}

func TestPackageAPI_ContainerDetailListsTagsAndUntagged(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	tagged, loose := e.pushBlobs(t, auth, e.pubImage, nil), e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, tagged.manifest)
	e.putManifest(auth, e.pubImage, loose.digest, mtOCIManifest, loose.manifest)

	rr := e.api(http.MethodGet, "/api/repos/"+e.owner+"/pub/packages/container/pubimg", "", nil)

	var d struct {
		Name string `json:"name"`
		Tags []struct {
			Name, Digest, Publisher string
			PullCount               int64 `json:"pull_count"`
			Size                    int64
		} `json:"tags"`
		Untagged []struct{ Digest string } `json:"untagged"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil || rr.Code != http.StatusOK {
		t.Fatalf("detail: %d %s (%v)", rr.Code, rr.Body, err)
	}
	if len(d.Tags) != 1 || d.Tags[0].Name != "v1" || d.Tags[0].Digest != tagged.digest || d.Tags[0].Publisher != e.owner {
		t.Errorf("tags = %+v", d.Tags)
	}
	if len(d.Untagged) != 1 || d.Untagged[0].Digest != loose.digest {
		t.Errorf("untagged = %+v", d.Untagged)
	}
	if rr := e.api(http.MethodGet, "/api/repos/"+e.owner+"/pub/packages/container/nosuch", "", nil); rr.Code != http.StatusNotFound {
		t.Errorf("a missing package: got %d, want 404", rr.Code)
	}
}

func TestPackageAPI_DeleteWholePackageNeedsAManager(t *testing.T) {
	e := newRegistryEnv(t, withGenericFiles)
	pub := e.repo(t, e.owner, "pub")
	writerID := e.addCollaborator(t, pub, string(model.RoleWriter))
	writerPAT := e.pat(t, writerID, model.ScopePackagesWrite)
	e.uploadGeneric(t, pub, "tool")
	base := "/api/repos/" + e.owner + "/pub/packages"

	if rr := e.api(http.MethodDelete, base+"/container/pubimg", writerPAT, nil); rr.Code != http.StatusForbidden {
		t.Errorf("writer deleting a container package: got %d, want 403", rr.Code)
	}
	if rr := e.api(http.MethodDelete, base+"/generic/tool", writerPAT, nil); rr.Code != http.StatusForbidden {
		t.Errorf("writer deleting a generic package: got %d, want 403", rr.Code)
	}
	if rr := e.api(http.MethodDelete, base+"/container/pubimg", "", nil); rr.Code != http.StatusUnauthorized && rr.Code != http.StatusForbidden {
		t.Errorf("anonymous delete: got %d, want 401 or 403", rr.Code)
	}
	for _, target := range []string{"/container/pubimg", "/generic/tool"} {
		if rr := e.api(http.MethodDelete, base+target, e.ownerPAT, nil); rr.Code != http.StatusNoContent {
			t.Errorf("owner deleting %s: got %d %s, want 204", target, rr.Code, rr.Body)
		}
	}
	if rr := e.api(http.MethodDelete, base+"/container/pubimg", e.ownerPAT, nil); rr.Code != http.StatusNotFound {
		t.Errorf("deleting twice: got %d, want 404", rr.Code)
	}
}

func TestPackageAPI_RelinkMovesAPackageWithinThePathOwnersRepositories(t *testing.T) {
	e := newRegistryEnv(t, nil)
	theirs := testutil.SeedUser(t, e.db, "oth_"+testutil.UniqueSuffix(t))
	other, err := e.svc.Repo.Create(t.Context(), theirs, userName(t, e.db, theirs), "elsewhere", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Repo.Create(t.Context(), e.ownerID, e.owner, "pub2", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatal(err)
	}
	base := "/api/repos/" + e.owner + "/pub/packages/relink"

	if rr := e.api(http.MethodPost, base, e.ownerPAT, map[string]any{"type": "container", "name": "pubimg", "repo": other.OwnerName + "/" + other.Name}); rr.Code != http.StatusBadRequest {
		t.Errorf("to another owner's repository: got %d %s, want 400", rr.Code, rr.Body)
	}
	if rr := e.api(http.MethodPost, base, e.strangerPAT, map[string]any{"type": "container", "name": "pubimg", "repo": e.owner + "/pub2"}); rr.Code != http.StatusForbidden {
		t.Errorf("a stranger: got %d, want 403", rr.Code)
	}
	if rr := e.api(http.MethodPost, base, e.ownerPAT, map[string]any{"type": "container", "name": "pubimg", "repo": e.owner + "/priv"}); rr.Code != http.StatusNoContent {
		t.Fatalf("to the owner's own repository: got %d %s, want 204", rr.Code, rr.Body)
	}
	if rr := e.api(http.MethodGet, "/api/repos/"+e.owner+"/priv/packages/container/pubimg", e.ownerPAT, nil); rr.Code != http.StatusOK {
		t.Errorf("the package under its new repository: got %d, want 200", rr.Code)
	}
}
