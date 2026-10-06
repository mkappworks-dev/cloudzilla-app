package storage_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage/storagetest"
)

// fakeS3 serves path-style PUT/GET/HEAD/DELETE for one bucket. It refuses an
// unsigned request and one carrying a flexible checksum, which several
// S3-compatible servers reject.
type fakeS3 struct {
	t       *testing.T
	bucket  string
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeS3(t *testing.T, bucket string) *httptest.Server {
	f := &fakeS3{t: t, bucket: bucket, objects: map[string][]byte{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		f.t.Errorf("%s %s: missing SigV4 Authorization", r.Method, r.URL.Path)
		http.Error(w, "unsigned", http.StatusForbidden)
		return
	}
	for name := range r.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-amz-checksum-") || lower == "x-amz-sdk-checksum-algorithm" || lower == "x-amz-trailer" {
			f.t.Errorf("%s %s: sent %s", r.Method, r.URL.Path, name)
			http.Error(w, "checksum header", http.StatusBadRequest)
			return
		}
	}
	key, ok := strings.CutPrefix(r.URL.Path, "/"+f.bucket+"/")
	if !ok {
		f.t.Errorf("%s %s: not a path-style request for bucket %s", r.Method, r.URL.Path, f.bucket)
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.objects[key] = body
		w.Header().Set("ETag", `"x"`)
	case http.MethodGet:
		data, ok := f.objects[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message><Key>%s</Key></Error>`, key)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
	case http.MethodHead:
		data, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	case http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

func fakeBackend(t *testing.T, endpoint, prefix string) storage.Backend {
	t.Helper()
	b, err := storage.New(context.Background(), config.StorageConfig{Backend: "s3", S3: config.S3StorageConfig{
		Endpoint: endpoint, Region: "us-east-1", Bucket: "avatars",
		AccessKeyID: "test", SecretAccessKey: "test", PathStyle: true, Prefix: prefix,
	}})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return b
}

func TestS3Backend_Conformance_FakeServer(t *testing.T) {
	srv := newFakeS3(t, "avatars")
	storagetest.Run(t, fakeBackend(t, srv.URL, "instance-a"), "suite")
}

func TestS3Backend_PrefixIsolation(t *testing.T) {
	srv := newFakeS3(t, "avatars")
	a := fakeBackend(t, srv.URL, "instance-a")
	b := fakeBackend(t, srv.URL, "instance-b")
	ctx := context.Background()
	if err := a.Put(ctx, "avatars/user/1/x.png", bytes.NewReader([]byte("a")), 1); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if ok, err := b.Exists(ctx, "avatars/user/1/x.png"); err != nil || ok {
		t.Errorf("other prefix sees the object: %v, %v", ok, err)
	}
	if ok, err := a.Exists(ctx, "avatars/user/1/x.png"); err != nil || !ok {
		t.Errorf("own prefix misses the object: %v, %v", ok, err)
	}
}

// Runs against a real S3-compatible server, such as the compose file's
// versitygw service, when TEST_S3_* is set.
func TestS3Backend_Conformance_RealServer(t *testing.T) {
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT not set; skipping real S3 test")
	}
	b, err := storage.New(context.Background(), config.StorageConfig{Backend: "s3", S3: config.S3StorageConfig{
		Endpoint:        endpoint,
		Region:          envOr("TEST_S3_REGION", "us-east-1"),
		Bucket:          os.Getenv("TEST_S3_BUCKET"),
		AccessKeyID:     os.Getenv("TEST_S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("TEST_S3_SECRET_ACCESS_KEY"),
		PathStyle:       os.Getenv("TEST_S3_PATH_STYLE") != "false",
	}})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	storagetest.Run(t, b, fmt.Sprintf("storagetest-%d", time.Now().UnixNano()))
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
