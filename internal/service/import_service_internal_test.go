package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

func TestImportService_StartRejectsBadInputBeforeAnyLookup(t *testing.T) {
	s := &ImportService{jobs: map[string]*importJob{}} // nil repo: a lookup would panic
	for _, tc := range []struct {
		req ImportRequest
		err error
	}{
		{ImportRequest{CloneURL: "/srv/repos/alice/secret.git", Name: "x"}, ErrImportURL},
		{ImportRequest{CloneURL: "https://user:tok@example.com/a.git", Name: "x"}, ErrImportURLUserinfo},
		{ImportRequest{CloneURL: "https://example.com/a.git", AuthToken: "tok", Name: "x"}, ErrImportCredentials},
		{ImportRequest{CloneURL: "https://example.com/a.git", AuthUsername: "me", Name: "x"}, ErrImportCredentials},
	} {
		if _, err := s.Start(context.Background(), 1, "me", tc.req); !errors.Is(err, tc.err) {
			t.Errorf("Start(%+v) = %v, want %v", tc.req, err, tc.err)
		}
	}
}

func TestImportService_SweepDropsOldFinishedJobs(t *testing.T) {
	now := time.Now()
	s := &ImportService{jobs: map[string]*importJob{
		"old":     {ImportJob: ImportJob{ID: "old", Status: ImportDone, FinishedAt: now.Add(-2 * time.Hour)}},
		"recent":  {ImportJob: ImportJob{ID: "recent", Status: ImportFailed, FinishedAt: now.Add(-time.Minute)}},
		"running": {ImportJob: ImportJob{ID: "running", Status: ImportRunning}},
	}}
	s.sweepLocked(now)
	if _, ok := s.jobs["old"]; ok {
		t.Error("old finished job kept")
	}
	for _, id := range []string{"recent", "running"} {
		if _, ok := s.jobs[id]; !ok {
			t.Errorf("%s job dropped", id)
		}
	}
}

func TestImportService_EvictsAUsersOldestFinishedJobs(t *testing.T) {
	now := time.Now()
	s := &ImportService{jobs: map[string]*importJob{}}
	add := func(id string, user int64, status ImportStatus, finishedAgo time.Duration) {
		job := &importJob{ImportJob: ImportJob{ID: id, UserID: user, Status: status}}
		if job.Finished() {
			job.FinishedAt = now.Add(-finishedAgo)
		}
		s.jobs[id] = job
	}
	const total = importFinishedPerUser + 5
	for i := 1; i <= total; i++ { // job i finished i minutes ago
		add(fmt.Sprintf("mine-%d", i), 1, ImportFailed, time.Duration(i)*time.Minute)
	}
	add("running", 1, ImportRunning, 0)
	add("queued", 1, ImportQueued, 0)
	add("theirs-old", 2, ImportDone, 3*time.Hour)

	s.evictFinishedLocked(1)

	for i := 1; i <= total; i++ {
		id := fmt.Sprintf("mine-%d", i)
		if _, kept := s.jobs[id]; kept != (i <= importFinishedPerUser) {
			t.Errorf("%s kept = %v, want %v", id, kept, i <= importFinishedPerUser)
		}
	}
	for _, id := range []string{"running", "queued", "theirs-old"} {
		if _, ok := s.jobs[id]; !ok {
			t.Errorf("%s was evicted", id)
		}
	}
}

func TestImportService_PanicFailsTheJob(t *testing.T) {
	root := t.TempDir()
	s := &ImportService{root: root, slots: make(chan struct{}, 1), jobs: map[string]*importJob{}}
	job := &importJob{
		ImportJob: ImportJob{ID: "boom", UserID: 1, SourceURL: "https://example.com/a.git", Owner: "me", Name: "a", Status: ImportQueued},
		progress:  &importProgress{},
	}
	s.jobs[job.ID] = job
	dir := filepath.Join(root, importTmpDirName, job.ID)

	orig := cloneImport
	t.Cleanup(func() { cloneImport = orig })
	cloneImport = func(_ context.Context, dir, _ string, _ transport.AuthMethod, _ io.Writer) (string, error) {
		if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o755); err != nil {
			t.Errorf("seed clone dir: %v", err)
		}
		panic("hostile reply")
	}

	s.run(job, RepoTarget{}, "", false, nil)

	got, err := s.Get(1, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != ImportFailed || got.Error != "The import failed." {
		t.Errorf("job = %s %q, want failed %q", got.Status, got.Error, "The import failed.")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp clone left behind: %v", err)
	}
	if len(s.slots) != 0 {
		t.Error("import slot not released")
	}
}

func TestImportService_FailureNamesTheCapCrossed(t *testing.T) {
	s := &ImportService{}
	job := &importJob{ImportJob: ImportJob{ID: "j", SourceURL: "https://example.com/a.git", Owner: "me", Name: "a"}}
	for _, tc := range []struct {
		crossed *importSizeError
		want    string
	}{
		{&importSizeError{limit: 2 << 30}, "The repository is larger than this instance's limit of 2 GiB."},
		{&importSizeError{limit: 512 << 20}, "The repository is larger than this instance's limit of 512 MiB."},
		{&importSizeError{limit: importMaxRefsBytes, refs: true}, "The source advertised more refs than this instance accepts (64 MiB)."},
	} {
		got := s.failureMessage(context.Background(), job, &importGuard{tooLarge: tc.crossed}, errors.New("read failed"))
		if got != tc.want {
			t.Errorf("failureMessage = %q, want %q", got, tc.want)
		}
	}
}

func TestFormatImportLimits(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Minute: "30m", time.Hour: "1h", 90 * time.Minute: "1h30m", 45 * time.Second: "45s",
	} {
		if got := formatImportTimeout(d); got != want {
			t.Errorf("formatImportTimeout(%v) = %q, want %q", d, got, want)
		}
	}
	for n, want := range map[int64]string{2 << 30: "2 GiB", 100 << 20: "100 MiB", 1: "1 MiB"} {
		if got := formatImportBytes(n); got != want {
			t.Errorf("formatImportBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
