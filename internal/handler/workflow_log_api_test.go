package handler_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// logEnv is a runner env with storage, a claimed Job and its per-job token.
type logAPIEnv struct {
	*runnerEnv
	spec *runnerapi.JobSpec
	run  int
}

func newLogAPIEnv(t *testing.T) *logAPIEnv {
	t.Helper()
	e := newRunnerEnv(t)
	backend, err := storage.NewLocal(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatal(err)
	}
	e.svcs.WithStorage(backend)
	spec := e.claim(t, e.registerRunner(t, nil))
	return &logAPIEnv{runnerEnv: e, spec: spec, run: spec.RunNumber}
}

func (e *logAPIEnv) raw(t *testing.T, method, path, token, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	return rr
}

func (e *logAPIEnv) postLog(t *testing.T, token string, seq, step int, text string) *httptest.ResponseRecorder {
	return e.raw(t, http.MethodPost, fmt.Sprintf("%s?seq=%d&step=%d", jobPath(e.spec.JobID, "log"), seq, step), token, "text/plain", []byte(text))
}

func (e *logAPIEnv) base() string {
	return fmt.Sprintf("/%s/%s/actions/runs/%d", e.r.owner.name, e.r.name, e.run)
}

func (e *logAPIEnv) apiBase() string {
	return fmt.Sprintf("/api/repos/%s/%s/actions/runs/%d", e.r.owner.name, e.r.name, e.run)
}

func testZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("out/hello.txt")
	_, _ = f.Write([]byte("hello"))
	_ = zw.Close()
	return buf.Bytes()
}

func TestRunnerLog_PerJobTokenPostsChunksAndReadersPageByOffset(t *testing.T) {
	e := newLogAPIEnv(t)
	tok := e.spec.Token

	wantCode(t, e.postLog(t, tok, 0, 0, "one\ntwo\n"), http.StatusOK)
	wantCode(t, e.postLog(t, tok, 1, 1, "three\nfour\n"), http.StatusOK)

	logs := fmt.Sprintf("%s/jobs/%d/logs", e.apiBase(), e.spec.JobID)
	rr := e.raw(t, http.MethodGet, logs+"?offset=1&limit=2", e.r.owner.token, "", nil)
	wantCode(t, rr, http.StatusOK)
	page := decodeBody[struct {
		Offset     int      `json:"offset"`
		NextOffset int      `json:"next_offset"`
		TotalLines int      `json:"total_lines"`
		Lines      []string `json:"lines"`
		Complete   bool     `json:"complete"`
	}](t, rr)
	if page.Offset != 1 || page.NextOffset != 3 || page.TotalLines != 4 || strings.Join(page.Lines, "|") != "two|three" || page.Complete {
		t.Errorf("page = %+v", page)
	}

	dl := e.raw(t, http.MethodGet, fmt.Sprintf("%s/jobs/%d/log", e.base(), e.spec.JobID), e.r.owner.token, "", nil)
	wantCode(t, dl, http.StatusOK)
	if dl.Body.String() != "one\ntwo\nthree\nfour\n" || !strings.HasPrefix(dl.Header().Get("Content-Disposition"), "attachment") ||
		dl.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(dl.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("download = %q %v", dl.Body.String(), dl.Header())
	}
}

func TestRunnerLog_TokenRules(t *testing.T) {
	e := newLogAPIEnv(t)
	other := newLogAPIEnv(t)

	wantCode(t, e.postLog(t, "", 0, 0, "x\n"), http.StatusUnauthorized)
	wantCode(t, e.postLog(t, "czj_nope", 0, 0, "x\n"), http.StatusUnauthorized)
	wantCode(t, e.postLog(t, other.spec.Token, 0, 0, "x\n"), http.StatusForbidden)
	wantCode(t, e.raw(t, http.MethodPost, jobPath(e.spec.JobID, "log")+"?step=0", e.spec.Token, "text/plain", []byte("x\n")), http.StatusBadRequest)

	// The token dies with the Job.
	wantCode(t, e.postLog(t, e.spec.Token, 0, 0, "x\n"), http.StatusOK)
	if err := e.svcs.WorkflowRun.FinishJob(context.Background(), e.spec.JobID, "success", ""); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.postLog(t, e.spec.Token, 1, 0, "late\n"), http.StatusUnauthorized)
	wantCode(t, e.raw(t, http.MethodPost, jobPath(e.spec.JobID, "artifacts")+"?name=a", e.spec.Token, "application/zip", testZip(t)), http.StatusUnauthorized)
}

func TestRunnerArtifact_UploadDownloadAndTheRules(t *testing.T) {
	e := newLogAPIEnv(t)
	z := testZip(t)

	rr := e.raw(t, http.MethodPost, jobPath(e.spec.JobID, "artifacts")+"?name=dist", e.spec.Token, "application/zip", z)
	wantCode(t, rr, http.StatusCreated)
	art := decodeBody[runnerapi.ArtifactResponse](t, rr)
	if art.Name != "dist" || art.Size != int64(len(z)) {
		t.Errorf("artifact = %+v", art)
	}
	wantCode(t, e.raw(t, http.MethodPost, jobPath(e.spec.JobID, "artifacts")+"?name=dist", e.spec.Token, "application/zip", z), http.StatusConflict)

	list := e.raw(t, http.MethodGet, e.apiBase()+"/artifacts", e.r.owner.token, "", nil)
	wantCode(t, list, http.StatusOK)
	if !strings.Contains(list.Body.String(), fmt.Sprintf(`"download_url":"%s/artifacts/%d"`, e.base(), art.ID)) || strings.Contains(list.Body.String(), "storage") {
		t.Errorf("list = %s", list.Body.String())
	}

	url := fmt.Sprintf("%s/artifacts/%d", e.base(), art.ID)
	dl := e.raw(t, http.MethodGet, url, e.r.owner.token, "", nil)
	wantCode(t, dl, http.StatusOK)
	h := dl.Header()
	if !bytes.Equal(dl.Body.Bytes(), z) || h.Get("Content-Disposition") != `attachment; filename="dist.zip"` || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Content-Security-Policy") != "default-src 'none'; sandbox" || h.Get("Content-Type") != "application/zip" || !strings.Contains(h.Get("Cache-Control"), "private") {
		t.Errorf("headers = %v", h)
	}
}

func TestWorkflowArtifact_PrivateRepoGivesANonReaderTheSame404AsAMissingOne(t *testing.T) {
	e := newLogAPIEnv(t)
	rr := e.raw(t, http.MethodPost, jobPath(e.spec.JobID, "artifacts")+"?name=dist", e.spec.Token, "application/zip", testZip(t))
	wantCode(t, rr, http.StatusCreated)
	id := decodeBody[runnerapi.ArtifactResponse](t, rr).ID
	if _, err := e.svcs.WorkflowLog.GetArtifact(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.r.id)
	stranger := seedSignedInUser(t, e.db)

	existing := fmt.Sprintf("%s/artifacts/%d", e.base(), id)
	missing := fmt.Sprintf("%s/artifacts/%d", e.base(), id+9999)
	noRun := fmt.Sprintf("/%s/%s/actions/runs/%d/artifacts/%d", e.r.owner.name, e.r.name, e.run+50, id)
	jobLog := fmt.Sprintf("%s/jobs/%d/log", e.base(), e.spec.JobID)
	for name, tok := range map[string]string{"signed out": "", "stranger": stranger.token} {
		want := e.raw(t, http.MethodGet, missing, tok, "", nil)
		wantCode(t, want, http.StatusNotFound)
		for _, path := range []string{existing, noRun, jobLog} {
			got := e.raw(t, http.MethodGet, path, tok, "", nil)
			if got.Code != http.StatusNotFound || got.Body.String() != want.Body.String() {
				t.Errorf("%s GET %s = %d %q, want the missing artifact's 404 %q", name, path, got.Code, got.Body.String(), want.Body.String())
			}
		}
		api := e.raw(t, http.MethodGet, fmt.Sprintf("%s/jobs/%d/logs", e.apiBase(), e.spec.JobID), tok, "", nil)
		apiMissing := e.raw(t, http.MethodGet, fmt.Sprintf("/api/repos/%s/nosuchrepo/actions/runs/1/jobs/1/logs", e.r.owner.name), tok, "", nil)
		if api.Code != http.StatusNotFound || api.Body.String() != apiMissing.Body.String() {
			t.Errorf("%s log API = %d %q, want a missing repo's %d %q", name, api.Code, api.Body.String(), apiMissing.Code, apiMissing.Body.String())
		}
	}
	wantCode(t, e.raw(t, http.MethodGet, existing, e.r.owner.token, "", nil), http.StatusOK)
}

func TestRunnerArtifact_RefusalsNameTheLimit(t *testing.T) {
	e := newLogAPIEnv(t)
	big := bytes.Repeat([]byte{'z'}, 100<<20+1)
	rr := e.raw(t, http.MethodPost, jobPath(e.spec.JobID, "artifacts")+"?name=big", e.spec.Token, "application/zip", big)
	wantCode(t, rr, http.StatusRequestEntityTooLarge)
	if !strings.Contains(rr.Body.String(), "100 MiB") || !strings.Contains(rr.Body.String(), "ci.max_artifact_bytes") {
		t.Errorf("body = %s", rr.Body.String())
	}
	for i := range 10 {
		wantCode(t, e.raw(t, http.MethodPost, jobPath(e.spec.JobID, "artifacts")+fmt.Sprintf("?name=a%d", i), e.spec.Token, "application/zip", testZip(t)), http.StatusCreated)
	}
	rr = e.raw(t, http.MethodPost, jobPath(e.spec.JobID, "artifacts")+"?name=eleven", e.spec.Token, "application/zip", testZip(t))
	wantCode(t, rr, http.StatusUnprocessableEntity)
	var body map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if !strings.Contains(body["error"], "at most 10 artifacts") || !strings.Contains(body["error"], "ci.max_artifacts_per_run") {
		t.Errorf("error = %q", body["error"])
	}
}
