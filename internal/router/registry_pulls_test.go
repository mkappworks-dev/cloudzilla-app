package router_test

// Download counts of manifests and tags. Requires TEST_DATABASE_DSN and skips otherwise.

import (
	"context"
	"net/http"
	"testing"
)

func (e *registryEnv) pulls(t *testing.T, digest string) (manifest int, tags map[string]int) {
	t.Helper()
	if err := e.db.QueryRow(`SELECT pull_count FROM package_manifests WHERE digest = $1`, digest).Scan(&manifest); err != nil {
		t.Fatalf("manifest pulls: %v", err)
	}
	rows, err := e.db.Query(`SELECT t.name, t.pull_count FROM package_tags t JOIN package_manifests m ON m.id = t.manifest_id WHERE m.digest = $1`, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	tags = map[string]int{}
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			t.Fatal(err)
		}
		tags[name] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return manifest, tags
}

func TestPullCounts_ThreeByTagAndOneByDigestAfterAFlush(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)
	e.putManifest(auth, e.pubImage, "stable", mtOCIManifest, img.manifest)
	manifestURL := "/v2/" + e.pubImage + "/manifests/"

	for range 3 {
		e.req(http.MethodGet, manifestURL+"v1", auth, nil)
	}
	e.req(http.MethodGet, manifestURL+img.digest, auth, nil)
	e.req(http.MethodHead, manifestURL+"v1", auth, nil)
	e.req(http.MethodGet, "/v2/"+e.pubImage+"/blobs/"+digestOf(img.layer), auth, nil)
	e.req(http.MethodGet, manifestURL+"nope", auth, nil)

	if m, _ := e.pulls(t, img.digest); m != 0 {
		t.Fatalf("pulls reached the database before a flush: %d", m)
	}
	if err := e.svc.Registry.FlushPulls(t.Context()); err != nil {
		t.Fatal(err)
	}
	m, tags := e.pulls(t, img.digest)
	if m != 4 || tags["v1"] != 3 || tags["stable"] != 0 {
		t.Errorf("manifest = %d, tags = %v; want 4, v1 3, stable 0 (HEAD, blob reads and a miss do not count)", m, tags)
	}

	if err := e.svc.Registry.FlushPulls(t.Context()); err != nil {
		t.Fatal(err)
	}
	if again, _ := e.pulls(t, img.digest); again != 4 {
		t.Errorf("a second flush counted %d, want it to add nothing", again)
	}
}

func TestPullCounts_AFlushThatFailsKeepsTheBatchForTheNextOne(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)
	e.req(http.MethodGet, "/v2/"+e.pubImage+"/manifests/v1", auth, nil)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	if err := e.svc.Registry.FlushPulls(cancelled); err == nil {
		t.Fatal("flush with a cancelled context succeeded")
	}
	if err := e.svc.Registry.FlushPulls(t.Context()); err != nil {
		t.Fatal(err)
	}

	if m, tags := e.pulls(t, img.digest); m != 1 || tags["v1"] != 1 {
		t.Errorf("manifest = %d, tags = %v; want the failed batch counted once", m, tags)
	}
}
