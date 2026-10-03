package service

import (
	"context"
	"errors"
	"testing"
	"time"
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
