package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// faultBackend wraps a real backend and tracks the keys it holds, so a test can fail a write or run code between a
// write and the row that follows it.
type faultBackend struct {
	storage.Backend
	mu       sync.Mutex
	keys     map[string]bool
	failPut  bool
	afterPut func()
}

func (f *faultBackend) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if f.failPut {
		return errors.New("disk full")
	}
	if err := f.Backend.Put(ctx, key, r, size); err != nil {
		return err
	}
	f.mu.Lock()
	f.keys[key] = true
	f.mu.Unlock()
	if f.afterPut != nil {
		f.afterPut()
	}
	return nil
}

func (f *faultBackend) Delete(ctx context.Context, key string) error {
	f.mu.Lock()
	delete(f.keys, key)
	f.mu.Unlock()
	return f.Backend.Delete(ctx, key)
}

func (f *faultBackend) held() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.keys)
}

type logEnv struct {
	*ciEnv
	backend *faultBackend
	logs    *store.WorkflowLogStore
	tok     *service.JobToken
	runRow  model.WorkflowRun
}

// newLogEnv pushes a workflow and gives back a token for its "test" Job.
func newLogEnv(t *testing.T, tune func(*config.CIConfig)) *logEnv {
	t.Helper()
	e := newCIEnv(t, tune)
	local, err := storage.NewLocal(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatal(err)
	}
	fb := &faultBackend{Backend: local, keys: map[string]bool{}}
	e.svcs.WithStorage(fb)
	sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})
	e.pushBranch("main", sha, e.owner.id)
	run := e.onlyRun()
	job := e.job(run, "test")
	return &logEnv{ciEnv: e, backend: fb, logs: store.NewWorkflowLogStore(e.db), runRow: run,
		tok: &service.JobToken{JobID: job.ID, RunID: run.ID, RepoID: e.repo.ID}}
}

func (e *logEnv) append(seq, step int, text string) bool {
	e.t.Helper()
	trunc, err := e.svcs.WorkflowLog.AppendLog(testCtx, e.tok, step, seq, []byte(text))
	if err != nil {
		e.t.Fatalf("append seq %d: %v", seq, err)
	}
	return trunc
}

func (e *logEnv) full() string {
	e.t.Helper()
	var b bytes.Buffer
	if err := e.svcs.WorkflowLog.WriteLog(testCtx, e.tok.JobID, &b); err != nil {
		e.t.Fatalf("write log: %v", err)
	}
	return b.String()
}

func lineBlock(first, n, width int) string {
	var b strings.Builder
	for i := first; i < first+n; i++ {
		fmt.Fprintf(&b, "%07d %s\n", i, strings.Repeat("x", width-9))
	}
	return b.String()
}

func TestWorkflowLog_ThirtyMiBIsCutAtTwentyWithOneTruncationLine(t *testing.T) {
	e := newLogEnv(t, nil)
	const lineWidth, perChunk = 100, 10000 // 1 MB chunks of 100-byte lines
	line := 0
	for seq := 0; seq < 31; seq++ {
		e.append(seq, 0, lineBlock(line, perChunk, lineWidth))
		line += perChunk
	}

	full := e.full()
	if n := int64(len(full)); n > 20<<20+200 || n < 20<<20-lineWidth-200 {
		t.Errorf("log is %d bytes, want about 20 MiB", n)
	}
	if got := strings.Count(full, "[log truncated"); got != 1 {
		t.Fatalf("truncation lines = %d, want 1", got)
	}
	if !strings.HasSuffix(full, "(ci.max_log_bytes)]\n") || !strings.Contains(full, "20 MiB limit") {
		t.Errorf("log ends %q", full[len(full)-120:])
	}

	// Offsets read back contiguous lines, across chunk boundaries and page boundaries.
	lines := strings.Split(strings.TrimSuffix(full, "\n"), "\n")
	var got []string
	for offset := 0; ; {
		page, err := e.svcs.WorkflowLog.ReadLog(testCtx, e.tok.JobID, offset, 3333)
		if err != nil {
			t.Fatal(err)
		}
		if page.TotalLines != len(lines) {
			t.Fatalf("total lines = %d, want %d", page.TotalLines, len(lines))
		}
		if page.Offset != offset || page.NextOffset != offset+len(page.Lines) {
			t.Fatalf("page = offset %d next %d lines %d", page.Offset, page.NextOffset, len(page.Lines))
		}
		if len(page.Lines) == 0 {
			break
		}
		got = append(got, page.Lines...)
		offset = page.NextOffset
	}
	if len(got) != len(lines) {
		t.Fatalf("read %d lines, want %d", len(got), len(lines))
	}
	for i := range lines {
		if got[i] != lines[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], lines[i])
		}
	}
	if !strings.HasPrefix(lines[1234], "0001234 ") {
		t.Errorf("line 1234 = %q", lines[1234])
	}
	page, _ := e.svcs.WorkflowLog.ReadLog(testCtx, e.tok.JobID, 25000, 5)
	if len(page.Lines) != 5 || !strings.HasPrefix(page.Lines[0], "0025000 ") || !page.Truncated {
		t.Errorf("mid-chunk page = %+v", page)
	}

	// Chunks after the cap are dropped, and the answer says so.
	if !e.append(40, 0, "late\n") || strings.Contains(e.full(), "late") {
		t.Error("a chunk after the cap was stored")
	}
}

func TestWorkflowLog_FullDownloadEqualsTheChunksInOrder(t *testing.T) {
	e := newLogEnv(t, nil)
	e.append(0, 0, "one\ntwo\n")
	e.append(1, 1, "three") // no trailing newline: the server ends the line
	e.append(2, 1, "four\nfive\n")

	if got, want := e.full(), "one\ntwo\nthree\nfour\nfive\n"; got != want {
		t.Errorf("full = %q, want %q", got, want)
	}
	page, _ := e.svcs.WorkflowLog.ReadLog(testCtx, e.tok.JobID, 2, 10)
	if strings.Join(page.Lines, "|") != "three|four|five" || page.NextOffset != 5 {
		t.Errorf("page = %+v", page)
	}
	steps, _ := e.svcs.WorkflowRun.ListSteps(testCtx, e.tok.JobID)
	if steps[0].FirstLine != 0 || steps[0].LastLine != 2 {
		t.Errorf("step 0 lines = [%d,%d), want [0,2)", steps[0].FirstLine, steps[0].LastLine)
	}
}

func TestWorkflowLog_ARetriedChunkIsStoredOnce(t *testing.T) {
	e := newLogEnv(t, nil)
	e.append(0, 0, "a\nb\n")
	e.append(0, 0, "a\nb\n")
	if got := e.full(); got != "a\nb\n" {
		t.Errorf("full = %q", got)
	}
	if e.backend.held() != 1 {
		t.Errorf("objects held = %d, want 1", e.backend.held())
	}
}

func TestWorkflowLog_FailedObjectWriteLeavesNoRow(t *testing.T) {
	e := newLogEnv(t, nil)
	e.backend.failPut = true
	if _, err := e.svcs.WorkflowLog.AppendLog(testCtx, e.tok, 0, 0, []byte("x\n")); err == nil {
		t.Fatal("append succeeded with a failing backend")
	}
	e.backend.failPut = false
	chunks, _ := e.logs.ChunksBetween(testCtx, e.tok.JobID, 0, 100)
	st, _ := e.logs.State(testCtx, e.tok.JobID)
	if len(chunks) != 0 || st.Lines != 0 || st.Bytes != 0 {
		t.Errorf("rows after a failed write: %d chunks, state %+v", len(chunks), st)
	}
}

func TestWorkflowLog_FailedRowAfterTheWriteDeletesTheObject(t *testing.T) {
	e := newLogEnv(t, nil)
	// A concurrent writer commits between this chunk's object and its row, every time.
	e.backend.afterPut = func() {
		if _, err := e.db.Exec(`UPDATE workflow_jobs SET log_lines = log_lines + 1 WHERE id = $1`, e.tok.JobID); err != nil {
			t.Error(err)
		}
	}
	_, err := e.svcs.WorkflowLog.AppendLog(testCtx, e.tok, 0, 0, []byte("x\n"))
	if !errors.Is(err, store.ErrLogConflict) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	if e.backend.held() != 0 {
		t.Errorf("%d objects left after the row failed", e.backend.held())
	}
	if chunks, _ := e.logs.ChunksBetween(testCtx, e.tok.JobID, 0, 1000); len(chunks) != 0 {
		t.Errorf("%d chunk rows", len(chunks))
	}
}

type secrets []string

func (s secrets) SecretValues(context.Context, int64) ([]string, error) { return s, nil }

func TestWorkflowLog_ServerMasksTheReposSecretsAsABackstop(t *testing.T) {
	e := newLogEnv(t, nil)
	e.svcs.WorkflowLog.WithSecrets(secrets{"hunter2-value"})
	e.append(0, 0, "password is hunter2-value\nencoded aHVudGVyMi12YWx1ZQ==\n")
	got := e.full()
	if strings.Contains(got, "hunter2") || strings.Contains(got, "aHVudGVyMi12YWx1ZQ") || strings.Count(got, "***") != 2 {
		t.Errorf("log = %q", got)
	}
}

func TestWorkflowLog_AMultiLineSecretSplitAcrossChunksIsMasked(t *testing.T) {
	e := newLogEnv(t, nil)
	e.svcs.WorkflowLog.WithSecrets(secrets{"first-line-secret\nsecond-line-secret\n"})
	e.append(0, 0, "first-line-secret\n")
	e.append(1, 0, "second-line-secret\n")
	if got := e.full(); strings.Contains(got, "line-secret") || strings.Count(got, "***") != 2 {
		t.Errorf("log = %q", got)
	}
}

type countingSecrets struct {
	mu sync.Mutex
	n  int
}

func (c *countingSecrets) SecretValues(context.Context, int64) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return []string{"hunter2-value"}, nil
}

func TestWorkflowLog_MaskerIsLoadedOncePerJobAndSkippedForNoSecretRuns(t *testing.T) {
	e := newLogEnv(t, nil)
	src := &countingSecrets{}
	e.svcs.WorkflowLog.WithSecrets(src)
	for i := range 3 {
		e.append(0, i, "line hunter2-value\n")
	}
	if src.n != 1 {
		t.Errorf("secrets loaded %d times for 3 chunks, want 1", src.n)
	}
	if strings.Contains(e.full(), "hunter2") {
		t.Errorf("log = %q", e.full())
	}

	noSecrets := &service.JobToken{JobID: e.tok.JobID + 1000, RunID: e.tok.RunID, RepoID: e.tok.RepoID, NoSecrets: true}
	_, _ = e.svcs.WorkflowLog.AppendLog(testCtx, noSecrets, 0, 0, []byte("x\n"))
	if src.n != 1 {
		t.Errorf("a no-secrets run loaded the repo's secrets (%d loads)", src.n)
	}
}

func TestWorkflowLog_ReadingAfterTheObjectsExpiredStopsAtTheGap(t *testing.T) {
	e := newLogEnv(t, nil)
	e.append(0, 0, "a\n")
	for k := range e.backend.keys {
		_ = e.backend.Delete(testCtx, k)
	}
	page, err := e.svcs.WorkflowLog.ReadLog(testCtx, e.tok.JobID, 0, 10)
	if err != nil || len(page.Lines) != 0 {
		t.Errorf("page = %+v, %v", page, err)
	}
	if e.full() != "" {
		t.Error("download of a log with a missing object was not empty")
	}
}

func zipOf(n int) io.Reader { return bytes.NewReader(bytes.Repeat([]byte{'z'}, n)) }

func TestWorkflowArtifact_UploadStoresObjectAndRow(t *testing.T) {
	e := newLogEnv(t, nil)
	a, err := e.svcs.WorkflowLog.UploadArtifact(testCtx, e.tok, "build out", zipOf(1000))
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "build-out" || a.Size != 1000 || len(a.SHA256) != 64 || !a.ExpiresAt.After(time.Now().Add(29*24*time.Hour)) {
		t.Errorf("artifact = %+v", a)
	}
	if !strings.HasPrefix(a.StorageKey, fmt.Sprintf("ci/artifacts/repo/%d/run/%d/", e.repo.ID, e.runRow.ID)) || e.backend.held() != 1 {
		t.Errorf("key = %q, held = %d", a.StorageKey, e.backend.held())
	}
	rc, err := e.svcs.WorkflowLog.OpenArtifact(testCtx, a)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if len(b) != 1000 {
		t.Errorf("object = %d bytes", len(b))
	}
}

func TestWorkflowArtifact_OversizeAndOverCountAreRefusedWithTheLimitNamed(t *testing.T) {
	e := newLogEnv(t, func(c *config.CIConfig) { c.MaxArtifactBytes = 10 << 20; c.MaxArtifactsPerRun = 2 })
	_, err := e.svcs.WorkflowLog.UploadArtifact(testCtx, e.tok, "big", zipOf(10<<20+1))
	if !errors.Is(err, service.ErrArtifactTooLarge) || !strings.Contains(err.Error(), "10 MiB") || !strings.Contains(err.Error(), "ci.max_artifact_bytes") {
		t.Errorf("oversize err = %v", err)
	}
	for _, n := range []string{"a", "b"} {
		if _, err := e.svcs.WorkflowLog.UploadArtifact(testCtx, e.tok, n, zipOf(5)); err != nil {
			t.Fatal(err)
		}
	}
	_, err = e.svcs.WorkflowLog.UploadArtifact(testCtx, e.tok, "c", zipOf(5))
	if !errors.Is(err, service.ErrArtifactLimit) || !strings.Contains(err.Error(), "at most 2 artifacts") || !strings.Contains(err.Error(), "ci.max_artifacts_per_run") {
		t.Errorf("count err = %v", err)
	}
	if e.backend.held() != 2 {
		t.Errorf("objects = %d, want only the two accepted", e.backend.held())
	}
}

func TestWorkflowArtifact_FailedWriteOrRowLeavesNothing(t *testing.T) {
	e := newLogEnv(t, nil)
	e.backend.failPut = true
	if _, err := e.svcs.WorkflowLog.UploadArtifact(testCtx, e.tok, "a", zipOf(5)); err == nil {
		t.Fatal("upload succeeded with a failing backend")
	}
	e.backend.failPut = false
	if list, _ := e.svcs.WorkflowLog.ListArtifacts(testCtx, e.runRow.ID); len(list) != 0 {
		t.Errorf("rows after a failed write: %v", list)
	}

	if _, err := e.svcs.WorkflowLog.UploadArtifact(testCtx, e.tok, "dup", zipOf(5)); err != nil {
		t.Fatal(err)
	}
	_, err := e.svcs.WorkflowLog.UploadArtifact(testCtx, e.tok, "dup", zipOf(5))
	if !errors.Is(err, service.ErrArtifactNameTaken) {
		t.Errorf("duplicate err = %v", err)
	}
	if e.backend.held() != 1 {
		t.Errorf("objects = %d, want the failed row's object deleted", e.backend.held())
	}
}

func TestWorkflowLog_RetentionDeletesExpiredObjectsAndKeepsRunsAndStatuses(t *testing.T) {
	e := newLogEnv(t, func(c *config.CIConfig) { c.LogRetention = 24 * time.Hour })
	e.append(0, 0, "old\n")
	if _, err := e.svcs.WorkflowLog.UploadArtifact(testCtx, e.tok, "old", zipOf(5)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svcs.WorkflowLog.UploadArtifact(testCtx, e.tok, "young", zipOf(5)); err != nil {
		t.Fatal(err)
	}
	e.append(1, 0, "recent\n")
	// Age the first chunk and the "old" artifact past their retention; the others stay.
	e.exec(`UPDATE workflow_log_chunks SET created_at = NOW() - INTERVAL '2 days' WHERE seq = 0`)
	e.exec(`UPDATE workflow_artifacts SET expires_at = NOW() - INTERVAL '1 minute' WHERE name = 'old'`)
	statusBefore, _ := e.status(e.runRow.SHA, "ci/test")

	// The database is shared with other tests whose repos are gone, so the count is a floor.
	n, err := e.svcs.WorkflowLog.Sweep(testCtx, time.Now())
	if err != nil || n < 2 {
		t.Fatalf("sweep = %d, %v", n, err)
	}
	if e.backend.held() != 2 {
		t.Errorf("objects held = %d, want the recent chunk and the young artifact", e.backend.held())
	}
	list, _ := e.svcs.WorkflowLog.ListArtifacts(testCtx, e.runRow.ID)
	if len(list) != 1 || list[0].Name != "young" {
		t.Errorf("artifacts = %+v", list)
	}
	if e.run(1).ID != e.runRow.ID || e.job(e.runRow, "test").Status != model.CIStatusQueued {
		t.Error("the sweep touched the Run or its Jobs")
	}
	if statusAfter, _ := e.status(e.runRow.SHA, "ci/test"); statusAfter.State != statusBefore.State {
		t.Errorf("commit status changed: %v -> %v", statusBefore.State, statusAfter.State)
	}
}

func TestWorkflowLog_RepoPurgeAndUserDeleteRemoveTheObjects(t *testing.T) {
	e := newLogEnv(t, nil)
	e.append(0, 0, "x\n")
	if _, err := e.svcs.WorkflowLog.UploadArtifact(testCtx, e.tok, "a", zipOf(5)); err != nil {
		t.Fatal(err)
	}
	if e.backend.held() != 2 {
		t.Fatalf("held = %d", e.backend.held())
	}

	if err := e.svcs.Repo.Delete(testCtx, e.repo.ID, e.owner.id); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if e.backend.held() != 2 {
		t.Fatalf("a soft delete removed objects: held = %d", e.backend.held())
	}
	e.exec(`UPDATE repositories SET deleted_at = NOW() - INTERVAL '40 days' WHERE id = $1`, e.repo.ID)
	if err := e.svcs.Repo.PurgeExpired(testCtx); err != nil {
		t.Fatal(err)
	}
	if e.backend.held() != 0 {
		t.Errorf("after purge: %d objects left", e.backend.held())
	}

	// A user delete cascades the repos' rows away; the orphan pass finds what they left.
	e2 := newLogEnv(t, nil)
	e2.append(0, 0, "x\n")
	if _, err := e2.svcs.WorkflowLog.UploadArtifact(testCtx, e2.tok, "a", zipOf(5)); err != nil {
		t.Fatal(err)
	}
	if err := e2.svcs.User.DeleteUser(testCtx, e2.owner.id); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if e2.backend.held() != 0 {
		t.Errorf("after user delete: %d objects left", e2.backend.held())
	}
}

func (e *ciEnv) exec(query string, args ...any) {
	e.t.Helper()
	if _, err := e.db.Exec(query, args...); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
}
