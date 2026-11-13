package runner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
)

// Poller claims Jobs one at a time and hands each to its Executor. A runner takes one Job at a time; run several
// runners for parallelism.
type Poller struct {
	Client *Client
	Labels []string
	Exec   *Executor
	// Once makes Loop return after the first Job.
	Once bool
	// Wait is the claim long-poll length.
	Wait time.Duration
	// SweepEvery is how often leftover Docker resources are removed while polling; defaultSweepEvery when zero.
	SweepEvery time.Duration
}

const defaultSweepEvery = 10 * time.Minute

// Loop polls until ctx ends, the token is revoked, or (with Once) a Job has run.
func (p *Poller) Loop(ctx context.Context) error {
	wait := p.Wait
	if wait == 0 {
		wait = runnerapi.MaxClaimWaitSeconds * time.Second * 5 / 6
	}
	// A kill -9 skips the Job's own cleanup, so the start of the next run is the first chance to remove it.
	p.Exec.Sweep(ctx)
	every := p.SweepEvery
	if every == 0 {
		every = defaultSweepEvery
	}
	sweepCtx, stopSweep := context.WithCancel(ctx)
	defer stopSweep()
	go p.Exec.SweepEvery(sweepCtx, every)
	backoff := time.Second
	for ctx.Err() == nil {
		spec, err := p.Client.Claim(ctx, p.Labels, wait)
		switch {
		case errors.Is(err, ErrUnauthorized):
			return err
		case err != nil:
			if ctx.Err() != nil {
				return nil
			}
			slog.Warn("claim failed", "error", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		if spec == nil {
			continue
		}
		slog.Info("claimed job", "job", spec.JobID, "run", spec.RunNumber, "name", spec.Name, "repo", spec.Repository)
		p.Exec.Execute(ctx, spec)
		if p.Once {
			return nil
		}
	}
	return nil
}
