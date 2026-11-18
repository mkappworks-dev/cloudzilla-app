package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const pullFlushInterval = time.Minute

type tagKey struct {
	packageID int64
	name      string
}

// pullCounter batches manifest downloads in memory; a crash loses the batch, which decision 18 accepts.
type pullCounter struct {
	mu        sync.Mutex
	manifests map[int64]int64
	tags      map[tagKey]int64
}

func (c *pullCounter) add(manifests map[int64]int64, tags map[tagKey]int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.manifests == nil {
		c.manifests, c.tags = map[int64]int64{}, map[tagKey]int64{}
	}
	for k, n := range manifests {
		c.manifests[k] += n
	}
	for k, n := range tags {
		c.tags[k] += n
	}
}

func (c *pullCounter) take() (map[int64]int64, map[tagKey]int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	manifests, tags := c.manifests, c.tags
	c.manifests, c.tags = nil, nil
	return manifests, tags
}

// CountPull records one download of m. Callers pass only a GET: a HEAD, a blob read or a miss is not a pull.
func (s *RegistryService) CountPull(m *Manifest) {
	tags := map[tagKey]int64{}
	if m.Tag != "" {
		tags[tagKey{m.PackageID, m.Tag}] = 1
	}
	s.pulls.add(map[int64]int64{m.ID: 1}, tags)
}

// FlushPulls writes the batch to the counters. A failed write puts the batch back for the next flush.
func (s *RegistryService) FlushPulls(ctx context.Context) error {
	manifests, tags := s.pulls.take()
	if len(manifests) == 0 {
		return nil
	}
	batch := make([]store.TagPulls, 0, len(tags))
	for k, n := range tags {
		batch = append(batch, store.TagPulls{PackageID: k.packageID, Name: k.name, Count: n})
	}
	if err := s.registry.AddPullCounts(ctx, manifests, batch); err != nil {
		s.pulls.add(manifests, tags)
		return err
	}
	return nil
}

// RunPullCounts flushes every minute until ctx ends. The final flush is the caller's, after the HTTP server has
// stopped taking requests.
func (s *RegistryService) RunPullCounts(ctx context.Context) {
	ticker := time.NewTicker(pullFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.FlushPulls(ctx); err != nil {
				slog.Warn("registry pull counts flush failed; keeping the batch", "error", err)
			}
		}
	}
}
