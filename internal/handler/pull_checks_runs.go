package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/timefmt"
)

// checkRuns groups the Runs on a pull request's head commit with their Jobs, and returns the statuses no Run owns.
// A failure leaves the Runs out and every status in the flat list, so the tab still shows the results.
func (h *Handler) checkRuns(ctx context.Context, repo *model.Repository, statuses []model.CommitStatus, canWrite bool) ([]view.CheckRun, []components.CheckRow) {
	flat := func() []components.CheckRow {
		rows := make([]components.CheckRow, 0, len(statuses))
		for _, s := range statuses {
			rows = append(rows, checkRowOf(s))
		}
		return rows
	}
	if !h.Cfg.CI.Enabled {
		return nil, flat()
	}
	var ids []int64
	for _, s := range statuses {
		if s.RunID != nil {
			ids = append(ids, *s.RunID)
		}
	}
	runs, err := h.Services.WorkflowRun.RunsByIDs(ctx, repo.ID, ids)
	if err != nil {
		slog.Warn("pull checks: run lookup failed", "repo_id", repo.ID, "error", err)
		return nil, flat()
	}
	owned := map[int64]bool{}
	for _, run := range runs {
		owned[run.ID] = true
	}
	var other []components.CheckRow
	for _, s := range statuses {
		if s.RunID == nil || !owned[*s.RunID] {
			other = append(other, checkRowOf(s))
		}
	}
	cards := make([]view.CheckRun, 0, len(runs))
	for _, run := range runs {
		jobs, err := h.Services.WorkflowRun.ListJobs(ctx, run.ID)
		if err != nil {
			slog.Warn("pull checks: job lookup failed", "run_id", run.ID, "error", err)
			return nil, flat()
		}
		cards = append(cards, checkRunCard(repo, run, jobs, canWrite))
	}
	return cards, other
}

func checkRowOf(s model.CommitStatus) components.CheckRow {
	return components.CheckRow{Context: s.Context, State: string(s.State), Description: s.Description, URL: s.TargetURL}
}

func checkRunCard(repo *model.Repository, run model.WorkflowRun, jobs []model.WorkflowJob, canWrite bool) view.CheckRun {
	url := actionsBase(repo) + "/runs/" + strconv.Itoa(run.Number)
	card := view.CheckRun{
		Title:      run.WorkflowName + " #" + strconv.Itoa(run.Number),
		Status:     string(run.Status),
		URL:        url,
		Waiting:    run.Status == model.CIStatusWaiting,
		CanApprove: canWrite && run.Status == model.CIStatusWaiting,
	}
	passed, started := 0, 0
	for _, j := range jobs {
		switch j.Status {
		case model.CIStatusSuccess, model.CIStatusSkipped:
			passed++
			started++
		case model.CIStatusFailure, model.CIStatusCancelled, model.CIStatusRunning:
			started++
		}
		card.Jobs = append(card.Jobs, components.CheckRow{
			Context: j.StatusContext, State: string(j.Status), Description: jobSummary(j, run.Status == model.CIStatusWaiting), URL: url,
		})
	}
	meta := []string{"Workflow " + run.WorkflowName}
	switch {
	case card.Waiting:
		meta = append(meta, fmt.Sprintf("%d of %d jobs started", started, len(jobs)))
	case run.Status.Finished():
		meta = append(meta, fmt.Sprintf("%d of %d jobs passed", passed, len(jobs)), runDuration(&run), timefmt.Relative(run.CreatedAt))
	default:
		meta = append(meta, fmt.Sprintf("%d of %d jobs passed", passed, len(jobs)))
		if run.StartedAt != nil {
			meta = append(meta, "running for "+runDuration(&run))
		}
	}
	card.Meta = strings.Join(meta, " · ")
	return card
}

// jobSummary is the line under a Job's name, with its duration where it has one.
func jobSummary(j model.WorkflowJob, runWaiting bool) string {
	span := ""
	if j.StartedAt != nil {
		span = spanText(*j.StartedAt, j.FinishedAt)
	}
	var s string
	switch j.Status {
	case model.CIStatusSuccess:
		s = "Succeeded in " + span
	case model.CIStatusFailure:
		s = "Failed after " + span
	case model.CIStatusRunning:
		s = "Running for " + span
	case model.CIStatusSkipped:
		s = "Skipped"
	case model.CIStatusCancelled:
		s = "Cancelled"
	case model.CIStatusQueued:
		s = "Queued"
	default:
		s = "Waiting for " + strings.Join(j.Needs, ", ")
		if runWaiting || len(j.Needs) == 0 {
			s = "Not started"
		}
	}
	if span == "" {
		s = strings.TrimSuffix(strings.TrimSuffix(s, " in "), " after ")
	}
	if j.Message != "" {
		s += ": " + j.Message
	}
	return s
}
