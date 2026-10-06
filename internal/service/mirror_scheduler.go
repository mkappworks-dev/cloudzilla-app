package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

const mirrorPollInterval = 30 * time.Second

// The lease outlives the sync's own timeout by enough to record its result,
// so another instance never claims a sync that is still finishing.
const mirrorLeaseSlack = time.Minute

// Run claims due mirrors and syncs them, at most mirror.max_concurrent at a
// time on this instance, until ctx ends. A sync already running finishes on
// its own timeout: shutdown doesn't cut it off half-fetched.
func (s *MirrorService) Run(ctx context.Context) {
	ticker := time.NewTicker(mirrorPollInterval)
	defer ticker.Stop()
	slots := make(chan struct{}, max(s.cfg.MaxConcurrent, 1))
	for {
		s.claimAndSync(ctx, slots)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}

// Wake makes Run look for due mirrors now rather than at its next poll.
func (s *MirrorService) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// SyncNow makes a mirror due and wakes the loop. A sync already holding the
// mirror's lease is left to finish.
func (s *MirrorService) SyncNow(ctx context.Context, repoID int64) error {
	if err := s.mirrors.MarkDue(ctx, repoID); err != nil {
		return err
	}
	s.Wake()
	return nil
}

func (s *MirrorService) claimAndSync(ctx context.Context, slots chan struct{}) {
	free := cap(slots) - len(slots)
	if free == 0 || ctx.Err() != nil {
		return
	}
	claimed, err := s.mirrors.ClaimDue(ctx, free, s.cfg.Timeout+mirrorLeaseSlack)
	if err != nil {
		slog.Error("mirror: claim due mirrors failed", "error", err)
		return
	}
	for _, m := range claimed {
		slots <- struct{}{}
		concurrency.Go("mirror.sync", func() {
			defer func() {
				<-slots
				s.Wake() // a freed slot may take a mirror that was due but didn't fit
			}()
			s.syncAndRecord(m)
		})
	}
}

func (s *MirrorService) syncAndRecord(m model.RepoMirror) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout)
	defer cancel()
	err := s.Sync(ctx, &m)
	record := context.Background()
	if err == nil {
		if err := s.mirrors.RecordSuccess(record, m.RepoID); err != nil {
			slog.Error("mirror: record success failed", "repo_id", m.RepoID, "error", err)
		}
		return
	}
	msg := "The sync failed."
	var syncErr *MirrorSyncError
	if errors.As(err, &syncErr) {
		msg = syncErr.Msg
	} else {
		slog.Error("mirror sync failed", "repo_id", m.RepoID, "error", err)
	}
	if err := s.mirrors.RecordFailure(record, m.RepoID, msg); err != nil {
		slog.Error("mirror: record failure failed", "repo_id", m.RepoID, "error", err)
	}
}
