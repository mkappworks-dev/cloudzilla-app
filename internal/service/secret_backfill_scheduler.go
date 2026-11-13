package service

import (
	"context"
	"log/slog"
	"time"
)

const (
	backfillPollInterval = 30 * time.Second
	// The lease outlives the scan's own timeout by enough to record its result.
	backfillLeaseSlack = time.Minute
	failedScanRetry    = time.Hour
	blockedAlertTTL    = 7 * 24 * time.Hour
)

// RunBackfill scans repositories whose history scan is due, one at a time on this instance, and sweeps blocked-only
// alerts, until ctx ends. A scan cut short by ctx keeps its lease and is retaken when it expires.
func (s *SecretScanService) RunBackfill(ctx context.Context) {
	ticker := time.NewTicker(backfillPollInterval)
	defer ticker.Stop()
	for {
		s.sweepBlocked(ctx)
		s.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}

func (s *SecretScanService) drain(ctx context.Context) {
	for ctx.Err() == nil {
		repoID, ok, err := s.scans.Claim(ctx, s.cfg.BackfillTimeout+backfillLeaseSlack)
		if err != nil {
			slog.Error("secret scan: claim failed", "error", err)
			return
		}
		if !ok {
			return
		}
		s.scanClaimed(ctx, repoID)
	}
}

func (s *SecretScanService) sweepBlocked(ctx context.Context) {
	n, err := s.alerts.SweepBlocked(ctx, time.Now().Add(-blockedAlertTTL))
	if err != nil {
		slog.Error("secret scan: sweep blocked alerts failed", "error", err)
		return
	}
	if n > 0 {
		slog.Info("secret scan: swept blocked alerts", "count", n)
	}
}
