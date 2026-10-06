package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

type stubProbe struct {
	pingErr     error
	pending     []string
	pendingErr  error
	pendingRuns int
}

func (p *stubProbe) Ping(context.Context) error { return p.pingErr }

func (p *stubProbe) PendingMigrations(context.Context) ([]string, error) {
	p.pendingRuns++
	return p.pending, p.pendingErr
}

func wantChecks(t *testing.T, got service.Readiness, status, database, migrations, storage string) {
	t.Helper()
	want := map[string]string{"database": database, "migrations": migrations, "storage": storage}
	if got.Status != status {
		t.Errorf("status: want %q, got %q", status, got.Status)
	}
	for k, v := range want {
		if got.Checks[k] != v {
			t.Errorf("%s: want %q, got %q", k, v, got.Checks[k])
		}
	}
}

func TestHealthReadiness_AllPass(t *testing.T) {
	root := t.TempDir()
	svc := service.NewHealthService(&stubProbe{}, root)

	got := svc.Readiness(context.Background())

	wantChecks(t, got, "ok", "ok", "ok", "ok")
	if leftovers, _ := filepath.Glob(filepath.Join(root, ".readyz-*")); len(leftovers) != 0 {
		t.Errorf("storage check left files behind: %v", leftovers)
	}
}

func TestHealthReadiness_DatabaseDown_SkipsMigrations(t *testing.T) {
	probe := &stubProbe{pingErr: errors.New("dial tcp: connection refused")}
	svc := service.NewHealthService(probe, t.TempDir())

	got := svc.Readiness(context.Background())

	wantChecks(t, got, "fail", "fail", "skipped", "ok")
	if probe.pendingRuns != 0 {
		t.Error("migrations must not be queried while the database is down")
	}
}

func TestHealthReadiness_PendingMigration_FailsUntilApplied(t *testing.T) {
	probe := &stubProbe{pending: []string{"101_user_code_themes"}}
	svc := service.NewHealthService(probe, t.TempDir())

	wantChecks(t, svc.Readiness(context.Background()), "fail", "ok", "fail", "ok")

	probe.pending = nil
	wantChecks(t, svc.Readiness(context.Background()), "ok", "ok", "ok", "ok")
}

func TestHealthReadiness_MigrationsPassOnce_NotQueriedAgain(t *testing.T) {
	probe := &stubProbe{}
	svc := service.NewHealthService(probe, t.TempDir())

	svc.Readiness(context.Background())
	svc.Readiness(context.Background())

	if probe.pendingRuns != 1 {
		t.Errorf("want 1 migrations query, got %d", probe.pendingRuns)
	}
}

func TestHealthReadiness_MigrationsQueryError_Fails(t *testing.T) {
	svc := service.NewHealthService(&stubProbe{pendingErr: errors.New("boom")}, t.TempDir())

	wantChecks(t, svc.Readiness(context.Background()), "fail", "ok", "fail", "ok")
}

func TestHealthReadiness_ReposRootMissing_FailsStorage(t *testing.T) {
	svc := service.NewHealthService(&stubProbe{}, filepath.Join(t.TempDir(), "missing"))

	wantChecks(t, svc.Readiness(context.Background()), "fail", "ok", "ok", "fail")
}

func TestHealthReadiness_ReposRootReadOnly_FailsStorage(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	svc := service.NewHealthService(&stubProbe{}, root)

	wantChecks(t, svc.Readiness(context.Background()), "fail", "ok", "ok", "fail")
}

type hangingProbe struct{}

func (hangingProbe) Ping(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (hangingProbe) PendingMigrations(context.Context) ([]string, error) { return nil, nil }

// A storage check that finished before the deadline must report its own result,
// not the deadline the database check ran into.
func TestHealthReadiness_DatabaseHangs_StorageStillReportsOK(t *testing.T) {
	svc := service.NewHealthService(hangingProbe{}, t.TempDir())
	for range 30 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		got := svc.Readiness(ctx)
		cancel()
		wantChecks(t, got, "fail", "fail", "skipped", "ok")
		if t.Failed() {
			return
		}
	}
}
