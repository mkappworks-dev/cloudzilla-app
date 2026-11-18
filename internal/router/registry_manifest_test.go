package router_test

// Manifests, tags and linking over the /v2 wire protocol. Requires TEST_DATABASE_DSN and skips otherwise.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const (
	mtOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	mtOCIIndex       = "application/vnd.oci.image.index.v1+json"
	mtDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	mtDockerList     = "application/vnd.docker.distribution.manifest.list.v2+json"
)

// image is a manifest pushed by pushImage, with the blobs it names.
type image struct {
	manifest []byte
	digest   string
	config   []byte
	layer    []byte
}

// pushBlobs uploads a config and one layer to name and returns the manifest naming them.
func (e *registryEnv) pushBlobs(t *testing.T, auth, name string, labels map[string]string) image {
	t.Helper()
	cfg := map[string]any{"architecture": "amd64", "os": "linux", "config": map[string]any{"Labels": labels}, "nonce": string(payload(t, "cfg"))}
	config, _ := json.Marshal(cfg)
	layer := payload(t, "layer")
	for _, b := range [][]byte{config, layer} {
		if rr := e.pushMonolithic(t, auth, name, b); rr.Code != http.StatusCreated {
			t.Fatalf("push blob to %s: got %d %s", name, rr.Code, rr.Body)
		}
	}
	m, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": mtOCIManifest,
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digestOf(config), "size": len(config)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": digestOf(layer), "size": len(layer)}},
	})
	return image{manifest: m, digest: digestOf(m), config: config, layer: layer}
}

// deleteAuth is a token that may also delete, which docker never asks for.
func (e *registryEnv) deleteAuth(t *testing.T, pat, name string) string {
	t.Helper()
	return e.bearer(t, pat, "repository:"+name+":pull,push,delete")
}

func (e *registryEnv) putManifest(auth, name, ref, mediaType string, body []byte) *httptest.ResponseRecorder {
	return e.req(http.MethodPut, "/v2/"+name+"/manifests/"+ref, auth, bytes.NewReader(body), "Content-Type", mediaType)
}

func TestManifest_PushThenPullByTagAndDigestReturnsTheExactBytes(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)

	put := e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)

	if put.Code != http.StatusCreated {
		t.Fatalf("push: got %d %s, want 201", put.Code, put.Body)
	}
	if got := put.Header().Get("Docker-Content-Digest"); got != img.digest {
		t.Errorf("Docker-Content-Digest = %q, want %q", got, img.digest)
	}
	if got, want := put.Header().Get("Location"), "/v2/"+e.pubImage+"/manifests/"+img.digest; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	for _, ref := range []string{"v1", img.digest} {
		get := e.req(http.MethodGet, "/v2/"+e.pubImage+"/manifests/"+ref, auth, nil, "Accept", mtOCIManifest)
		if get.Code != http.StatusOK || !bytes.Equal(get.Body.Bytes(), img.manifest) {
			t.Fatalf("GET %s: got %d %q", ref, get.Code, get.Body)
		}
		if got := get.Header().Get("Content-Type"); got != mtOCIManifest {
			t.Errorf("GET %s Content-Type = %q", ref, got)
		}
		if got := get.Header().Get("Docker-Content-Digest"); got != img.digest {
			t.Errorf("GET %s Docker-Content-Digest = %q", ref, got)
		}
		head := e.req(http.MethodHead, "/v2/"+e.pubImage+"/manifests/"+ref, auth, nil)
		if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
			t.Errorf("HEAD %s: got %d body %d length %q", ref, head.Code, head.Body.Len(), head.Header().Get("Content-Length"))
		}
	}
}

func TestManifest_RepushingATagMovesItAndLeavesTheOldManifestUntagged(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	first := e.pushBlobs(t, auth, e.pubImage, nil)
	second := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "latest", mtOCIManifest, first.manifest)

	if rr := e.putManifest(auth, e.pubImage, "latest", mtOCIManifest, second.manifest); rr.Code != http.StatusCreated {
		t.Fatalf("re-push: got %d %s", rr.Code, rr.Body)
	}

	get := e.req(http.MethodGet, "/v2/"+e.pubImage+"/manifests/latest", auth, nil)
	if !bytes.Equal(get.Body.Bytes(), second.manifest) {
		t.Errorf("latest serves the old manifest")
	}
	old := e.req(http.MethodGet, "/v2/"+e.pubImage+"/manifests/"+first.digest, auth, nil)
	if old.Code != http.StatusOK || !bytes.Equal(old.Body.Bytes(), first.manifest) {
		t.Errorf("old manifest by digest: got %d, want it kept untagged", old.Code)
	}
}

func TestManifest_AMissingManifestIsMANIFEST_UNKNOWN(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)

	for _, ref := range []string{"nope", testDigest} {
		rr := e.req(http.MethodGet, "/v2/"+e.pubImage+"/manifests/"+ref, auth, nil)
		if rr.Code != http.StatusNotFound || ociErrorCode(t, rr) != "MANIFEST_UNKNOWN" {
			t.Errorf("GET %s: got %d %s, want 404 MANIFEST_UNKNOWN", ref, rr.Code, rr.Body)
		}
	}
}

func TestManifest_RefusesWhatItCannotStore(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	missing := bytes.Replace(img.manifest, []byte(digestOf(img.layer)), []byte(testDigest), 1)

	cases := []struct {
		name, ref, mediaType string
		body                 []byte
		status               int
		code                 string
	}{
		{"a layer not linked to the package", "v1", mtOCIManifest, missing, http.StatusBadRequest, "MANIFEST_BLOB_UNKNOWN"},
		{"not JSON", "v1", mtOCIManifest, []byte("nope"), http.StatusBadRequest, "MANIFEST_INVALID"},
		{"an unsupported media type", "v1", "text/plain", img.manifest, http.StatusBadRequest, "MANIFEST_INVALID"},
		{"a media type that disagrees with the body", "v1", mtDockerManifest, img.manifest, http.StatusBadRequest, "MANIFEST_INVALID"},
		{"a digest reference that is not the content's", testDigest, mtOCIManifest, img.manifest, http.StatusBadRequest, "DIGEST_INVALID"},
		{"a tag that breaks the grammar", "-bad", mtOCIManifest, img.manifest, http.StatusBadRequest, "MANIFEST_INVALID"},
		{"a body over 4 MiB", "v1", mtOCIManifest, bytes.Repeat([]byte(" "), 4<<20+1), http.StatusRequestEntityTooLarge, "SIZE_INVALID"},
	}
	for _, c := range cases {
		rr := e.putManifest(auth, e.pubImage, c.ref, c.mediaType, c.body)
		if rr.Code != c.status || ociErrorCode(t, rr) != c.code {
			t.Errorf("%s: got %d %s, want %d %s", c.name, rr.Code, strings.TrimSpace(rr.Body.String()), c.status, c.code)
		}
	}
}

// pushIndex pushes an index naming the given images' manifests, each already pushed to name.
func (e *registryEnv) pushIndex(t *testing.T, auth, name, tag string, children ...image) image {
	t.Helper()
	var descs []any
	for _, c := range children {
		descs = append(descs, map[string]any{"mediaType": mtOCIManifest, "digest": c.digest, "size": len(c.manifest)})
	}
	body, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": mtOCIIndex, "manifests": descs})
	rr := e.putManifest(auth, name, tag, mtOCIIndex, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("push index: got %d %s", rr.Code, rr.Body)
	}
	return image{manifest: body, digest: digestOf(body)}
}

func TestManifest_AnIndexOfTwoChildrenPushesAndPullsAndItsChildrenCannotBeDeleted(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	a, b := e.pushBlobs(t, auth, e.pubImage, nil), e.pushBlobs(t, auth, e.pubImage, nil)
	for _, c := range []image{a, b} {
		if rr := e.putManifest(auth, e.pubImage, c.digest, mtOCIManifest, c.manifest); rr.Code != http.StatusCreated {
			t.Fatalf("push child by digest: got %d %s", rr.Code, rr.Body)
		}
	}

	idx := e.pushIndex(t, auth, e.pubImage, "multi", a, b)

	get := e.req(http.MethodGet, "/v2/"+e.pubImage+"/manifests/multi", auth, nil, "Accept", mtOCIIndex)
	if get.Code != http.StatusOK || !bytes.Equal(get.Body.Bytes(), idx.manifest) || get.Header().Get("Content-Type") != mtOCIIndex {
		t.Fatalf("pull index: got %d type %q", get.Code, get.Header().Get("Content-Type"))
	}
	del := e.req(http.MethodDelete, "/v2/"+e.pubImage+"/manifests/"+a.digest, e.deleteAuth(t, e.ownerPAT, e.pubImage), nil)
	if del.Code != http.StatusConflict || ociErrorCode(t, del) != "DENIED" {
		t.Errorf("delete referenced child: got %d %s, want 409 DENIED", del.Code, del.Body)
	}
	if rr := e.req(http.MethodGet, "/v2/"+e.pubImage+"/manifests/"+a.digest, auth, nil); rr.Code != http.StatusOK {
		t.Errorf("child after refused delete: got %d", rr.Code)
	}
}

func TestManifest_AnIndexNamingAnUnpushedChildIsRefused(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	body, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": mtOCIIndex,
		"manifests": []any{map[string]any{"mediaType": mtOCIManifest, "digest": testDigest, "size": 1}}})

	rr := e.putManifest(auth, e.pubImage, "multi", mtOCIIndex, body)

	if rr.Code != http.StatusBadRequest || ociErrorCode(t, rr) != "MANIFEST_BLOB_UNKNOWN" {
		t.Errorf("got %d %s, want 400 MANIFEST_BLOB_UNKNOWN", rr.Code, rr.Body)
	}
}

func TestManifestDelete_ATagDeleteKeepsTheManifestAndAManifestDeleteRemovesItsTags(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	for _, tag := range []string{"a", "b"} {
		e.putManifest(auth, e.pubImage, tag, mtOCIManifest, img.manifest)
	}
	url := func(ref string) string { return "/v2/" + e.pubImage + "/manifests/" + ref }
	auth = e.deleteAuth(t, e.ownerPAT, e.pubImage)

	if rr := e.req(http.MethodDelete, url("a"), auth, nil); rr.Code != http.StatusAccepted {
		t.Fatalf("delete tag: got %d %s, want 202", rr.Code, rr.Body)
	}
	if rr := e.req(http.MethodGet, url("a"), auth, nil); rr.Code != http.StatusNotFound {
		t.Errorf("deleted tag still pulls: %d", rr.Code)
	}
	if rr := e.req(http.MethodGet, url(img.digest), auth, nil); rr.Code != http.StatusOK {
		t.Errorf("manifest after tag delete: got %d, want it kept", rr.Code)
	}
	if rr := e.req(http.MethodDelete, url(img.digest), auth, nil); rr.Code != http.StatusAccepted {
		t.Fatalf("delete manifest: got %d %s, want 202", rr.Code, rr.Body)
	}
	for _, ref := range []string{"b", img.digest} {
		if rr := e.req(http.MethodGet, url(ref), auth, nil); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s after manifest delete: got %d, want 404", ref, rr.Code)
		}
	}
	if rr := e.req(http.MethodDelete, url("never"), auth, nil); rr.Code != http.StatusNotFound || ociErrorCode(t, rr) != "MANIFEST_UNKNOWN" {
		t.Errorf("delete unknown tag: got %d %s", rr.Code, rr.Body)
	}
}

func TestBlobDelete_IsRefusedWhileAManifestReferencesTheBlob(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	spare := payload(t, "spare")
	e.pushMonolithic(t, auth, e.pubImage, spare)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)
	blob := func(d string) string { return "/v2/" + e.pubImage + "/blobs/" + d }
	auth = e.deleteAuth(t, e.ownerPAT, e.pubImage)

	held := e.req(http.MethodDelete, blob(digestOf(img.layer)), auth, nil)
	if held.Code != http.StatusConflict || ociErrorCode(t, held) != "DENIED" {
		t.Errorf("delete referenced blob: got %d %s, want 409 DENIED", held.Code, held.Body)
	}
	if rr := e.req(http.MethodDelete, blob(digestOf(spare)), auth, nil); rr.Code != http.StatusAccepted {
		t.Fatalf("delete unreferenced blob: got %d %s, want 202", rr.Code, rr.Body)
	}
	if rr := e.req(http.MethodHead, blob(digestOf(spare)), auth, nil); rr.Code != http.StatusNotFound {
		t.Errorf("deleted blob still found: %d", rr.Code)
	}
	if rr := e.req(http.MethodDelete, blob(digestOf(spare)), auth, nil); rr.Code != http.StatusNotFound {
		t.Errorf("delete of a blob the package no longer links: got %d, want 404", rr.Code)
	}
}

func TestManifest_AReaderCannotPushOrDelete(t *testing.T) {
	e := newRegistryEnv(t, nil)
	owner := e.pushAuth(t, e.ownerPAT, e.privImage)
	img := e.pushBlobs(t, owner, e.privImage, nil)
	e.putManifest(owner, e.privImage, "v1", mtOCIManifest, img.manifest)
	reader := e.bearer(t, e.readerPAT, "repository:"+e.privImage+":pull,push,delete")

	put := e.putManifest(reader, e.privImage, "v2", mtOCIManifest, img.manifest)
	del := e.req(http.MethodDelete, "/v2/"+e.privImage+"/manifests/v1", reader, nil)

	if put.Code != http.StatusUnauthorized || del.Code != http.StatusUnauthorized {
		t.Errorf("reader push %d, delete %d, want 401 for both", put.Code, del.Code)
	}
	if rr := e.req(http.MethodGet, "/v2/"+e.privImage+"/manifests/v1", reader, nil); rr.Code != http.StatusOK {
		t.Errorf("reader pull: got %d, want 200", rr.Code)
	}
	if rr := e.req(http.MethodGet, "/v2/"+e.privImage+"/manifests/v2", reader, nil); rr.Code != http.StatusNotFound {
		t.Errorf("tag a reader pushed exists: %d", rr.Code)
	}
}

func TestTagsList_PaginatesLexicallyWithNLastAndALink(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	for _, tag := range []string{"v2", "latest", "v10", "Alpha", "v1"} {
		e.putManifest(auth, e.pubImage, tag, mtOCIManifest, img.manifest)
	}
	list := func(target string) (struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}, *httptest.ResponseRecorder) {
		rr := e.req(http.MethodGet, target, auth, nil)
		var body struct {
			Name string   `json:"name"`
			Tags []string `json:"tags"`
		}
		if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &body) != nil {
			t.Fatalf("GET %s: got %d %s", target, rr.Code, rr.Body)
		}
		return body, rr
	}
	base := "/v2/" + e.pubImage + "/tags/list"

	all, rr := list(base)
	if got := strings.Join(all.Tags, ","); got != "Alpha,latest,v1,v10,v2" || all.Name != e.pubImage {
		t.Errorf("all tags = %q name %q", got, all.Name)
	}
	if rr.Header().Get("Link") != "" {
		t.Errorf("unpaginated list has Link %q", rr.Header().Get("Link"))
	}
	page, rr := list(base + "?n=2")
	if got := strings.Join(page.Tags, ","); got != "Alpha,latest" {
		t.Errorf("first page = %q", got)
	}
	if want := "<" + base + "?last=latest&n=2>; rel=\"next\""; rr.Header().Get("Link") != want {
		t.Errorf("Link = %q, want %q", rr.Header().Get("Link"), want)
	}
	next, rr := list(base + "?n=2&last=latest")
	if got := strings.Join(next.Tags, ","); got != "v1,v10" || rr.Header().Get("Link") == "" {
		t.Errorf("second page = %q Link %q", got, rr.Header().Get("Link"))
	}
	end, rr := list(base + "?n=2&last=v10")
	if got := strings.Join(end.Tags, ","); got != "v2" || rr.Header().Get("Link") != "" {
		t.Errorf("last page = %q Link %q", got, rr.Header().Get("Link"))
	}
	if rr := e.req(http.MethodGet, base+"?n=x", auth, nil); rr.Code != http.StatusBadRequest {
		t.Errorf("bad n: got %d, want 400", rr.Code)
	}
}

func TestTagsList_AnUnreadablePackageIsTheMissingNameAnswer(t *testing.T) {
	e := newRegistryEnv(t, nil)
	stranger := e.pushAuth(t, e.strangerPAT, e.pubImage)
	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.pubRepoID)

	rr := e.req(http.MethodGet, "/v2/"+e.pubImage+"/tags/list", stranger, nil)

	if rr.Code != http.StatusNotFound || ociErrorCode(t, rr) != "NAME_UNKNOWN" {
		t.Errorf("got %d %s, want 404 NAME_UNKNOWN", rr.Code, rr.Body)
	}
}

func TestManifest_ARenameOfTheLinkedRepositoryLeavesThePathWorking(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)
	repo, err := e.svc.Repo.GetByID(context.Background(), e.pubRepoID)
	if err != nil {
		t.Fatal(err)
	}

	if err := e.svc.Repo.RenameRepo(context.Background(), repo, e.ownerID, "renamed"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if rr := e.req(http.MethodGet, "/v2/"+e.pubImage+"/manifests/v1", auth, nil); rr.Code != http.StatusOK || !bytes.Equal(rr.Body.Bytes(), img.manifest) {
		t.Errorf("pull after rename: got %d", rr.Code)
	}
	if rr := e.putManifest(auth, e.pubImage, "v2", mtOCIManifest, img.manifest); rr.Code != http.StatusCreated {
		t.Errorf("push after rename: got %d %s", rr.Code, rr.Body)
	}
}

const sourceLabel = "org.opencontainers.image.source"

// linkedRepo is the Repository the container package at name is linked to, 0 while it is pending or absent.
func (e *registryEnv) linkedRepo(t *testing.T, name string) int64 {
	t.Helper()
	image := strings.TrimPrefix(name, e.owner+"/")
	var id int64
	err := e.db.QueryRow(`SELECT COALESCE(MAX(repo_id), 0) FROM packages WHERE owner_user_id = $1 AND type = 'container' AND name = $2`, e.ownerID, image).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *registryEnv) repoID(t *testing.T, name string) int64 {
	t.Helper()
	repo, err := e.svc.Repo.Get(context.Background(), e.owner, name)
	if err != nil {
		t.Fatalf("repo %s: %v", name, err)
	}
	return repo.ID
}

// firstPush pushes an image to a name with no package yet and returns the manifest response.
func (e *registryEnv) firstPush(t *testing.T, pat, name string, labels map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	auth := e.pushAuth(t, pat, name)
	img := e.pushBlobs(t, auth, name, labels)
	return e.putManifest(auth, name, "v1", mtOCIManifest, img.manifest)
}

func TestLinking_ANameMatchingARepositoryLinksThereWhateverTheLabelSays(t *testing.T) {
	e := newRegistryEnv(t, nil)

	rr := e.firstPush(t, e.ownerPAT, e.owner+"/pub/server", map[string]string{sourceLabel: e.owner + "/priv"})

	if rr.Code != http.StatusCreated {
		t.Fatalf("push: got %d %s", rr.Code, rr.Body)
	}
	if got, want := e.linkedRepo(t, e.owner+"/pub/server"), e.repoID(t, "pub"); got != want {
		t.Errorf("linked to repo %d, want pub (%d)", got, want)
	}
}

func TestLinking_ANameMatchNeedsCanWriteOnThatRepository(t *testing.T) {
	e := newRegistryEnv(t, nil)
	_, writerPAT := e.writer(t)
	name := e.owner + "/priv"

	rr := e.firstPush(t, writerPAT, name, map[string]string{sourceLabel: e.owner + "/pub"})

	if rr.Code != http.StatusForbidden || ociErrorCode(t, rr) != "DENIED" {
		t.Fatalf("got %d %s, want 403 DENIED", rr.Code, rr.Body)
	}
	if e.linkedRepo(t, name) != 0 {
		t.Errorf("the image linked despite the refusal")
	}
}

func TestLinking_TheLabelNamesTheRepositoryInEveryAcceptedForm(t *testing.T) {
	forms := map[string]string{
		"owner/repo":                 "{owner}/pub",
		"url":                        "http://localhost/{owner}/pub",
		"url with .git":              "http://localhost/{owner}/pub.git",
		"url with trailing slash":    "http://localhost/{owner}/pub/",
		"url with .git and slash":    "http://localhost/{owner}/pub.git/",
		"url with another host case": "http://LocalHost/{owner}/pub",
		"url with a path below":      "http://localhost/{owner}/pub/tree/main",
		"owner/repo with .git":       "{owner}/pub.git",
		"owner/repo with padding":    "  {owner}/pub  ",
		"owner/repo in another case": "{OWNER}/PUB",
	}
	for form, label := range forms {
		t.Run(form, func(t *testing.T) {
			e := newRegistryEnv(t, nil)
			label = strings.NewReplacer("{owner}", e.owner, "{OWNER}", strings.ToUpper(e.owner)).Replace(label)

			rr := e.firstPush(t, e.ownerPAT, e.owner+"/webapp", map[string]string{sourceLabel: label})

			if rr.Code != http.StatusCreated {
				t.Fatalf("push with %q: got %d %s", label, rr.Code, rr.Body)
			}
			if got, want := e.linkedRepo(t, e.owner+"/webapp"), e.repoID(t, "pub"); got != want {
				t.Errorf("linked to repo %d, want pub (%d)", got, want)
			}
		})
	}
}

func TestLinking_ARefusalNamesBothRoutes(t *testing.T) {
	e := newRegistryEnv(t, nil)
	other, err := e.svc.Repo.Create(context.Background(), e.strangerID, e.stranger, "theirs", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_ = other
	cases := map[string]map[string]string{
		"no label":                 nil,
		"a label on another host":  {sourceLabel: "https://github.com/" + e.owner + "/pub"},
		"another owner's repo":     {sourceLabel: e.stranger + "/theirs"},
		"a repository that is not": {sourceLabel: e.owner + "/missing"},
		"a label that is nonsense": {sourceLabel: "not a repo"},
	}
	for name, labels := range cases {
		image := e.owner + "/app" + strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, " ", ""), "'", ""))
		rr := e.firstPush(t, e.ownerPAT, image, labels)
		msg := rr.Body.String()
		if rr.Code != http.StatusForbidden || ociErrorCode(t, rr) != "DENIED" || !strings.Contains(msg, sourceLabel) || !strings.Contains(msg, "named like") {
			t.Errorf("%s: got %d %s, want 403 DENIED naming the label and the name route", name, rr.Code, msg)
		}
		if e.linkedRepo(t, image) != 0 {
			t.Errorf("%s: the image linked despite the refusal", name)
		}
	}
}

func TestLinking_ALabelNeedsCanManageOnTheRepository(t *testing.T) {
	e := newRegistryEnv(t, nil)
	_, writerPAT := e.writer(t)

	rr := e.firstPush(t, writerPAT, e.owner+"/webapp", map[string]string{sourceLabel: e.owner + "/pub"})

	if rr.Code != http.StatusForbidden || ociErrorCode(t, rr) != "DENIED" {
		t.Errorf("a writer's label: got %d %s, want 403 DENIED", rr.Code, rr.Body)
	}
}

func TestLinking_ALaterPushNeedsCanWriteOnTheLinkedRepository(t *testing.T) {
	e := newRegistryEnv(t, nil)
	if rr := e.firstPush(t, e.ownerPAT, e.owner+"/webapp", map[string]string{sourceLabel: e.owner + "/priv"}); rr.Code != http.StatusCreated {
		t.Fatalf("first push: got %d %s", rr.Code, rr.Body)
	}

	rr := e.req(http.MethodPost, "/v2/"+e.owner+"/webapp/blobs/uploads/", e.pushAuth(t, e.readerPAT, e.owner+"/webapp"), nil)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("a reader of the linked repo pushing: got %d, want 401", rr.Code)
	}
}

func TestLinking_ALargeConfigIsRefusedWhenTheLabelMustBeRead(t *testing.T) {
	e := newRegistryEnv(t, nil)
	name := e.owner + "/webapp"
	auth := e.pushAuth(t, e.ownerPAT, name)
	config := []byte(`{"config":{"Labels":{"` + sourceLabel + `":"` + e.owner + `/pub"}},"pad":"` + strings.Repeat("x", 1<<20) + `"}`)
	layer := payload(t, "layer")
	for _, b := range [][]byte{config, layer} {
		if rr := e.pushMonolithic(t, auth, name, b); rr.Code != http.StatusCreated {
			t.Fatalf("push blob: got %d %s", rr.Code, rr.Body)
		}
	}
	m, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": mtOCIManifest,
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digestOf(config), "size": len(config)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": digestOf(layer), "size": len(layer)}}})

	rr := e.putManifest(auth, name, "v1", mtOCIManifest, m)

	if rr.Code != http.StatusForbidden || ociErrorCode(t, rr) != "DENIED" || e.linkedRepo(t, name) != 0 {
		t.Errorf("got %d %s linked=%d, want 403 DENIED and no link", rr.Code, rr.Body, e.linkedRepo(t, name))
	}
}
