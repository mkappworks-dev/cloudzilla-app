package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// runnerOnlineWindow is how recently a runner must have contacted the server to show as online: a polling runner
// calls at least every claim wait (about 25 s) plus its backoff.
const runnerOnlineWindow = 2 * time.Minute

func runnerStatus(r model.CIRunner, now time.Time) string {
	switch {
	case r.RevokedAt != nil:
		return "revoked"
	case r.LastContactAt != nil && now.Sub(*r.LastContactAt) < runnerOnlineWindow:
		return "online"
	}
	return "offline"
}

// runnersFrag builds the runners card. Fleet adds the version and current Job columns.
func (h *Handler) runnersFrag(ctx context.Context, runners []model.CIRunner, base, desc string, fleet bool) view.RunnersFragData {
	jobs, err := h.Services.CIRunner.CurrentJobs(ctx, runners)
	if err != nil {
		slog.Warn("runners: current jobs unavailable", "error", err)
	}
	now := time.Now()
	rows := make([]view.RunnerRow, 0, len(runners))
	for _, r := range runners {
		row := view.RunnerRow{ID: r.ID, Name: r.Name, Labels: r.Labels, Version: r.Version, Status: runnerStatus(r, now), LastContact: r.LastContactAt}
		if j, ok := jobs[r.ID]; ok {
			row.CurrentJob = j.Owner + "/" + j.Repo + " · CI #" + strconv.Itoa(j.RunNumber) + " · " + j.JobName
			row.CurrentJobURL = "/" + j.Owner + "/" + j.Repo + "/actions/runs/" + strconv.Itoa(j.RunNumber)
		}
		rows = append(rows, row)
	}
	return view.RunnersFragData{Rows: rows, Base: base, Description: desc, Fleet: fleet}
}

const (
	instanceRunnersDesc = "Claim Jobs from every repository that has CI on. A repository's own runners are managed in its Settings."
	repoRunnersDesc     = "Machines that run this repository's Jobs. Instance runners registered by an administrator also pick up its Jobs."
)

func (h *Handler) adminRunnersFrag(ctx context.Context) (view.RunnersFragData, error) {
	runners, err := h.Services.CIRunner.ListInstanceRunners(ctx)
	if err != nil {
		return view.RunnersFragData{}, err
	}
	return h.runnersFrag(ctx, runners, "/admin/runners", instanceRunnersDesc, true), nil
}

func (h *Handler) repoRunnersFrag(ctx context.Context, repo *model.Repository) (view.RunnersFragData, error) {
	runners, err := h.Services.CIRunner.ListRepoRunners(ctx, repo.ID)
	if err != nil {
		return view.RunnersFragData{}, err
	}
	return h.runnersFrag(ctx, runners, "/"+repo.OwnerName+"/"+repo.Name+"/settings/runners", repoRunnersDesc, false), nil
}

// PageAdminRunners renders GET /admin/runners; the route is superadmin-only.
func (h *Handler) PageAdminRunners(w http.ResponseWriter, r *http.Request) {
	frag, err := h.adminRunnersFrag(r.Context())
	if err != nil {
		slog.Error("admin runners: list failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	ci := h.Cfg.CI
	h.render(w, r, pages.AdminRunners(view.AdminRunnersData{
		BasePage: basePage(r, h.Services), Runners: frag, ServerURL: h.Cfg.Server.BaseURL,
		Limits: []view.CILimit{
			{Key: "ci.max_jobs_per_run", Value: strconv.Itoa(ci.MaxJobsPerRun)},
			{Key: "ci.max_steps_per_run", Value: strconv.Itoa(ci.MaxStepsPerRun)},
			{Key: "ci.max_queued_runs_per_repo", Value: strconv.Itoa(ci.MaxQueuedRunsPerRepo)},
			{Key: "ci.max_concurrent_runs_per_repo", Value: strconv.Itoa(ci.MaxConcurrentRunsPerRepo)},
			{Key: "ci.max_job_minutes", Value: strconv.Itoa(ci.MaxJobMinutes)},
		},
	}))
}

// settingsRunners is nil when CI is off, which hides the section.
func (h *Handler) settingsRunners(ctx context.Context, repo *model.Repository) *view.RunnersFragData {
	if !h.Cfg.CI.Enabled {
		return nil
	}
	frag, err := h.repoRunnersFrag(ctx, repo)
	if err != nil {
		slog.Warn("settings: runners unavailable", "repo_id", repo.ID, "error", err)
		return nil
	}
	return &frag
}
