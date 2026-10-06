package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const readinessTimeout = 3 * time.Second

// Readiness check outcomes.
const (
	CheckOK      = "ok"
	CheckFail    = "fail"
	CheckSkipped = "skipped"
)

// HealthProbe is the database side of a readiness check.
type HealthProbe interface {
	Ping(ctx context.Context) error
	PendingMigrations(ctx context.Context) ([]string, error)
}

// Readiness is the /readyz body. It never carries error details: the
// endpoint is unauthenticated and driver errors can name hosts and paths.
type Readiness struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func (r Readiness) Ready() bool { return r.Status == CheckOK }

// HealthService runs the readiness checks.
type HealthService struct {
	probe     HealthProbe
	reposRoot string
	// Once nothing is pending it stays so: the embedded set is fixed and versions are never unapplied.
	migrated atomic.Bool
}

func NewHealthService(probe HealthProbe, reposRoot string) *HealthService {
	return &HealthService{probe: probe, reposRoot: reposRoot}
}

// Readiness checks the database, the schema and the repos volume under one
// shared deadline, logging the cause of each failure.
func (s *HealthService) Readiness(ctx context.Context) Readiness {
	ctx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()

	checks := map[string]string{
		"database":   CheckOK,
		"migrations": CheckOK,
		"storage":    CheckOK,
	}
	fail := func(check string, err error) {
		checks[check] = CheckFail
		slog.Warn("readiness check failed", "check", check, "error", err)
	}
	// A hung mount must not hold the probe past the deadline.
	storage := make(chan error, 1)
	go func() { storage <- checkWritable(s.reposRoot) }()

	if err := s.probe.Ping(ctx); err != nil {
		fail("database", err)
		checks["migrations"] = CheckSkipped
	} else if !s.migrated.Load() {
		pending, err := s.probe.PendingMigrations(ctx)
		switch {
		case err != nil:
			fail("migrations", err)
		case len(pending) > 0:
			fail("migrations", fmt.Errorf("%d pending, first %s; run cloudzilla-cli migrate", len(pending), pending[0]))
		default:
			s.migrated.Store(true)
		}
	}
	select {
	case err := <-storage:
		if err != nil {
			fail("storage", err)
		}
	case <-ctx.Done():
		fail("storage", ctx.Err())
	}

	status := CheckOK
	for _, v := range checks {
		if v == CheckFail {
			status = CheckFail
		}
	}
	return Readiness{Status: status, Checks: checks}
}

// checkWritable catches a missing volume, a read-only mount and wrong
// ownership; it doesn't measure free space.
func checkWritable(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("git.repos_root is not set")
	}
	f, err := os.CreateTemp(dir, ".readyz-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_, werr := f.WriteString("ok")
	cerr := f.Close()
	rerr := os.Remove(name)
	return errors.Join(werr, cerr, rerr)
}
