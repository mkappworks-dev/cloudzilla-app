package runner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	labelRunner = "cz.runner"
	labelJob    = "cz.job"
	jobPrefix   = "cz-job-"
)

// labels tag everything the runner creates so Sweep can find what a dead runner left. rest is appended after them.
func (e *Executor) labels(jobID int64, rest ...string) []string {
	return append([]string{fmt.Sprintf("--label=%s=%s", labelRunner, e.Config.Owner()), fmt.Sprintf("--label=%s=%d", labelJob, jobID)}, rest...)
}

// Sweep removes the containers, volumes and networks this runner labelled and no longer holds a Job for: what a
// kill -9 or a crash left. Two runners sharing one config on one host would sweep each other's Jobs, so don't.
func (e *Executor) Sweep(ctx context.Context) {
	d := docker{bin: e.Docker}
	filter := "--filter=label=" + labelRunner + "=" + e.Config.Owner()
	held := fmt.Sprintf("%s%d", jobPrefix, e.held.Load())
	for _, k := range []struct {
		list   []string
		remove []string
	}{
		{[]string{"ps", "-a", filter, "--format={{.Names}}"}, []string{"rm", "-f"}},
		{[]string{"volume", "ls", filter, "--format={{.Name}}"}, []string{"volume", "rm", "-f"}},
		{[]string{"network", "ls", filter, "--format={{.Name}}"}, []string{"network", "rm"}},
	} {
		out, err := d.output(ctx, k.list...)
		if err != nil {
			slog.Warn("sweep: list failed", "error", err)
			continue
		}
		for name := range strings.FieldsSeq(out) {
			// The prefix check keeps a stray label on something else from being removed, or read as a flag.
			if !strings.HasPrefix(name, jobPrefix) || name == held {
				continue
			}
			if err := d.run(ctx, nil, append(k.remove[:len(k.remove):len(k.remove)], name)...); err != nil {
				slog.Warn("sweep: remove failed", "name", name, "error", err)
				continue
			}
			slog.Info("removed a leftover job resource", "name", name, "kind", k.remove[0])
		}
	}
}

// SweepEvery runs Sweep on a timer until ctx ends.
func (e *Executor) SweepEvery(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.Sweep(ctx)
		}
	}
}
