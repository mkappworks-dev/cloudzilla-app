package router_test

// The referrers API over the /v2 wire protocol. Requires TEST_DATABASE_DSN and skips otherwise.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
)

const sigArtifactType = "application/vnd.dev.cosign.artifact.sig.v1+json"

type referrerIndex struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Manifests     []struct {
		MediaType    string `json:"mediaType"`
		Digest       string `json:"digest"`
		Size         int64  `json:"size"`
		ArtifactType string `json:"artifactType"`
	} `json:"manifests"`
}

// pushReferrer pushes a manifest whose subject is the given digest, with artifactType set, under tag ("" for none).
func (e *registryEnv) pushReferrer(t *testing.T, auth, name, subject, artifactType, tag string) (digest string, put *httptest.ResponseRecorder) {
	t.Helper()
	layer := payload(t, "sig")
	if rr := e.pushMonolithic(t, auth, name, layer); rr.Code != http.StatusCreated {
		t.Fatalf("push signature blob: %d %s", rr.Code, rr.Body)
	}
	empty := []byte("{}")
	if rr := e.pushMonolithic(t, auth, name, empty); rr.Code != http.StatusCreated {
		t.Fatalf("push empty config: %d %s", rr.Code, rr.Body)
	}
	doc := map[string]any{
		"schemaVersion": 2, "mediaType": mtOCIManifest, "artifactType": artifactType,
		"config":  map[string]any{"mediaType": "application/vnd.oci.empty.v1+json", "digest": digestOf(empty), "size": len(empty)},
		"layers":  []any{map[string]any{"mediaType": "application/octet-stream", "digest": digestOf(layer), "size": len(layer)}},
		"subject": map[string]any{"mediaType": mtOCIManifest, "digest": subject, "size": 1},
	}
	body, _ := json.Marshal(doc)
	ref := tag
	if ref == "" {
		ref = digestOf(body)
	}
	return digestOf(body), e.putManifest(auth, name, ref, mtOCIManifest, body)
}

func (e *registryEnv) referrers(t *testing.T, auth, name, subject, filter string) (*httptest.ResponseRecorder, referrerIndex) {
	t.Helper()
	target := "/v2/" + name + "/referrers/" + subject
	if filter != "" {
		target += "?artifactType=" + url.QueryEscape(filter)
	}
	rr := e.req(http.MethodGet, target, auth, nil)
	var idx referrerIndex
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &idx); err != nil {
			t.Fatalf("decode referrers %s: %v", rr.Body, err)
		}
	}
	return rr, idx
}

func TestReferrers_ASignatureManifestAppearsUnderItsSubject(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)

	sig, put := e.pushReferrer(t, auth, e.pubImage, img.digest, sigArtifactType, "")

	if put.Code != http.StatusCreated {
		t.Fatalf("push referrer: %d %s", put.Code, put.Body)
	}
	if got := put.Header().Get("OCI-Subject"); got != img.digest {
		t.Errorf("OCI-Subject = %q, want %q", got, img.digest)
	}
	rr, idx := e.referrers(t, auth, e.pubImage, img.digest, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("referrers: %d %s", rr.Code, rr.Body)
	}
	if got := rr.Header().Get("Content-Type"); got != mtOCIIndex {
		t.Errorf("Content-Type = %q, want %q", got, mtOCIIndex)
	}
	if idx.SchemaVersion != 2 || idx.MediaType != mtOCIIndex || len(idx.Manifests) != 1 {
		t.Fatalf("index = %+v, want one manifest of an OCI index", idx)
	}
	m := idx.Manifests[0]
	if m.Digest != sig || m.ArtifactType != sigArtifactType || m.MediaType != mtOCIManifest || m.Size == 0 {
		t.Errorf("descriptor = %+v, want the signature's digest, artifactType, media type and size", m)
	}
	if rr.Header().Get("OCI-Filters-Applied") != "" {
		t.Errorf("OCI-Filters-Applied set without a filter")
	}
}

func TestReferrers_AnUnknownSubjectIsAnEmptyIndexNotA404(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)

	rr, idx := e.referrers(t, auth, e.pubImage, testDigest, "")

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200", rr.Code, rr.Body)
	}
	if idx.SchemaVersion != 2 || idx.MediaType != mtOCIIndex || idx.Manifests == nil || len(idx.Manifests) != 0 {
		t.Errorf("index = %+v (%s), want an empty manifests array", idx, rr.Body)
	}
}

func TestReferrers_ArtifactTypeFilterIsAnnounced(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)
	sig, _ := e.pushReferrer(t, auth, e.pubImage, img.digest, sigArtifactType, "")
	e.pushReferrer(t, auth, e.pubImage, img.digest, "application/spdx+json", "")

	rr, idx := e.referrers(t, auth, e.pubImage, img.digest, sigArtifactType)

	if rr.Code != http.StatusOK || len(idx.Manifests) != 1 || idx.Manifests[0].Digest != sig {
		t.Fatalf("filtered: %d %s, want only the signature", rr.Code, rr.Body)
	}
	if got := rr.Header().Get("OCI-Filters-Applied"); got != "artifactType" {
		t.Errorf("OCI-Filters-Applied = %q, want artifactType", got)
	}
}

func TestReferrers_AReferrerFallsBackToTheConfigMediaTypeAsItsArtifactType(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)

	e.pushReferrer(t, auth, e.pubImage, img.digest, "", "")

	_, idx := e.referrers(t, auth, e.pubImage, img.digest, "application/vnd.oci.empty.v1+json")
	if len(idx.Manifests) != 1 {
		t.Errorf("referrers by config media type = %d, want 1", len(idx.Manifests))
	}
}

func TestReferrers_TaggedReferrersStayInTheTagList(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)
	e.pushReferrer(t, auth, e.pubImage, img.digest, sigArtifactType, "sha256-sig")

	rr := e.req(http.MethodGet, "/v2/"+e.pubImage+"/tags/list", auth, nil)

	var body struct{ Tags []string }
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if len(body.Tags) != 2 || body.Tags[0] != "sha256-sig" || body.Tags[1] != "v1" {
		t.Errorf("tags = %v, want [sha256-sig v1]", body.Tags)
	}
}

func TestReferrers_APrivateNameAnswersLikeAMissingOne(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.bearer(t, e.strangerPAT, "repository:"+e.privImage+":pull")

	rr := e.req(http.MethodGet, "/v2/"+e.privImage+"/referrers/"+testDigest, auth, nil)

	if rr.Code != http.StatusNotFound && rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want the refusal a missing name gets", rr.Code)
	}
}

func TestManifest_AnIndexCarriesAPlatformSummaryOfItsChildren(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	amd, arm, attest := e.pushBlobs(t, auth, e.pubImage, nil), e.pushBlobs(t, auth, e.pubImage, nil), e.pushBlobs(t, auth, e.pubImage, nil)
	for _, c := range []image{amd, arm, attest} {
		if rr := e.putManifest(auth, e.pubImage, c.digest, mtOCIManifest, c.manifest); rr.Code != http.StatusCreated {
			t.Fatalf("push child: %d %s", rr.Code, rr.Body)
		}
	}
	child := func(c image, platform map[string]any) map[string]any {
		return map[string]any{"mediaType": mtOCIManifest, "digest": c.digest, "size": len(c.manifest), "platform": platform}
	}
	index, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": mtOCIIndex,
		"manifests": []any{
			child(amd, map[string]any{"os": "linux", "architecture": "amd64"}),
			child(arm, map[string]any{"os": "linux", "architecture": "arm64", "variant": "v8"}),
			child(attest, map[string]any{"os": "unknown", "architecture": "unknown"}),
		},
	})
	if rr := e.putManifest(auth, e.pubImage, "multi", mtOCIIndex, index); rr.Code != http.StatusCreated {
		t.Fatalf("push index: %d %s", rr.Code, rr.Body)
	}

	m, err := e.svc.Registry.GetManifest(context.Background(), e.ownerID, e.pubImage, "multi", nil)

	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"linux/amd64", "linux/arm64/v8"}; !slices.Equal(m.Platforms, want) {
		t.Errorf("Platforms = %v, want %v", m.Platforms, want)
	}
	single, err := e.svc.Registry.GetManifest(context.Background(), e.ownerID, e.pubImage, amd.digest, nil)
	if err != nil || len(single.Platforms) != 0 {
		t.Errorf("an image manifest has Platforms %v (err %v), want none", single.Platforms, err)
	}
}
