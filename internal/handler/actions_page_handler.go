package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/codeurl"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/timefmt"
)

const (
	actionsPageSize = 20
)

// runFilterStatuses are the statuses the list can filter by, as the status select offers them.
var runFilterStatuses = []model.CIStatus{
	model.CIStatusWaiting, model.CIStatusQueued, model.CIStatusRunning,
	model.CIStatusSuccess, model.CIStatusFailure, model.CIStatusCancelled,
}

var runFilterEvents = []string{model.RunEventPush, model.RunEventPull, "manual"}

func shortSHA(sha string) string { return sha[:min(7, len(sha))] }

func actionsBase(repo *model.Repository) string {
	return "/" + repo.OwnerName + "/" + repo.Name + "/actions"
}

func runRefName(ref string) string {
	return strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/tags/")
}

func runDuration(run *model.WorkflowRun) string {
	if run.StartedAt == nil {
		return "—"
	}
	return spanText(*run.StartedAt, run.FinishedAt)
}

func spanText(start time.Time, end *time.Time) string {
	to := time.Now()
	if end != nil {
		to = *end
	}
	return formatDuration(to.Sub(start))
}

func formatDuration(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	switch {
	case s < 0:
		s = 0
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh %02dm", s/3600, s%3600/60)
}

// runTrigger describes who or what started a Run, without markup; the page links the pull request itself.
func runTrigger(run *model.WorkflowRun, refs service.RunRefs) (text string, pull int, by string) {
	who := "a user"
	if run.TriggerUserID != nil {
		if n, ok := refs.Users[*run.TriggerUserID]; ok {
			who = n
		}
	}
	switch {
	case run.PullRequestID != nil && refs.PullNumbers[*run.PullRequestID] > 0:
		return "", refs.PullNumbers[*run.PullRequestID], who
	case run.Event == model.RunEventDispatch:
		return "Manual run by " + who, 0, ""
	}
	return "Push by " + who, 0, ""
}

// onlineRunnerLabels returns the label sets of the runners that can serve the repo and are online now.
func (h *Handler) onlineRunnerLabels(ctx context.Context, repoID int64) (sets [][]string, registered bool) {
	var all []model.CIRunner
	if inst, err := h.Services.CIRunner.ListInstanceRunners(ctx); err == nil {
		all = append(all, inst...)
	}
	if own, err := h.Services.CIRunner.ListRepoRunners(ctx, repoID); err == nil {
		all = append(all, own...)
	}
	now := time.Now()
	for _, r := range all {
		if r.RevokedAt != nil {
			continue
		}
		registered = true
		if runnerStatus(r, now) == "online" {
			sets = append(sets, r.Labels)
		}
	}
	return sets, registered
}

// missingLabel names the runs-on labels of a queued Job that no online runner can serve, or "" when one can.
func missingLabel(job model.WorkflowJob, online [][]string) string {
	for _, labels := range online {
		if len(job.RunsOn) > 0 && isSubset(job.RunsOn, labels) {
			return ""
		}
	}
	var missing []string
	for _, l := range job.RunsOn {
		if !slices.ContainsFunc(online, func(set []string) bool { return slices.Contains(set, l) }) {
			missing = append(missing, l)
		}
	}
	if len(missing) == 0 {
		missing = job.RunsOn
	}
	return strings.Join(missing, ", ")
}

func isSubset(want, have []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

func statusLabel(s model.CIStatus) string {
	switch s {
	case model.CIStatusWaiting:
		return "Approval required"
	case model.CIStatusQueued:
		return "Queued"
	case model.CIStatusRunning:
		return "Running"
	case model.CIStatusSuccess:
		return "Succeeded"
	case model.CIStatusFailure:
		return "Failed"
	case model.CIStatusCancelled:
		return "Cancelled"
	}
	return "Skipped"
}

// PageActions handles GET /{owner}/{repo}/actions: the Runs, or with source=statuses the commit statuses external CI
// posts.
func (h *Handler) PageActions(w http.ResponseWriter, r *http.Request) { h.pageActions(w, r, false) }

// PageChecks handles GET /{owner}/{repo}/checks. With CI on it is the old address of Actions; with ci.enabled off the
// Actions routes do not exist, so it keeps showing the commit statuses external CI posts.
func (h *Handler) PageChecks(w http.ResponseWriter, r *http.Request) {
	if h.Cfg.CI.Enabled {
		target := "/" + chi.URLParam(r, "owner") + "/" + chi.URLParam(r, "repo") + "/actions"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}
	h.pageActions(w, r, true)
}

func (h *Handler) pageActions(w http.ResponseWriter, r *http.Request, legacy bool) {
	owner, repoName := chi.URLParam(r, "owner"), chi.URLParam(r, "repo")
	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}
	ctx := r.Context()
	viewer := viewerOf(r)
	canWrite := viewer != nil && h.Services.Repo.CanWrite(ctx, repo, *viewer)
	canManage := viewer != nil && h.Services.Repo.CanManage(ctx, repo, *viewer)
	q := r.URL.Query()

	filter := view.ActionsFilter{Source: view.SourceWorkflows, Workflow: q.Get("workflow"), Branch: q.Get("branch")}
	if q.Get("source") == view.SourceStatuses || legacy {
		filter.Source = view.SourceStatuses
	}
	if s := model.CIStatus(q.Get("status")); slices.Contains(runFilterStatuses, s) {
		filter.Status = string(s)
	}
	if e := q.Get("event"); slices.Contains(runFilterEvents, e) {
		filter.Event = e
	}
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}

	data := view.ActionsData{
		BasePage: h.withRepoSubnav(ctx, basePage(r, h.Services), repo, "actions", canManage),
		Owner:    owner, RepoName: repoName, Filter: filter, CanWrite: canWrite, CanManage: canManage, Legacy: legacy,
		Flash:        actionsErrorText(r),
		StatusAPIURL: h.Cfg.Server.BaseURL + "/api/repos/" + owner + "/" + repoName + "/statuses/<sha>",
	}
	listBase := actionsBase(repo)
	if legacy {
		listBase = "/" + owner + "/" + repoName + "/checks"
	}
	data.StatusesURL = actionsBase(repo) + "?source=" + view.SourceStatuses
	if on, err := h.Services.WorkflowRun.RepoCIEnabled(ctx, repo.ID); err == nil {
		data.RepoCIOff = !on
	}

	pageURL := func(n int) string {
		v := url.Values{}
		for k, val := range map[string]string{"source": q.Get("source"), "workflow": filter.Workflow, "branch": filter.Branch,
			"status": filter.Status, "event": filter.Event} {
			if val != "" && (k != "source" || val != view.SourceWorkflows) {
				v.Set(k, val)
			}
		}
		if n > 1 {
			v.Set("page", strconv.Itoa(n))
		}
		if len(v) == 0 {
			return listBase
		}
		return listBase + "?" + v.Encode()
	}

	if filter.Source == view.SourceStatuses {
		commits, more, err := h.Services.CommitStatus.ListRecentCommits(ctx, repo.ID, owner, repoName, page, actionsPageSize)
		if err != nil {
			slog.Error("PageActions: ListRecentCommits failed", "owner", owner, "repo", repoName, "error", err)
			h.ServerError(w, r)
			return
		}
		data.Commits = commits
		if page > 1 {
			data.NewerURL = pageURL(page - 1)
		}
		if more {
			data.OlderURL = pageURL(page + 1)
		}
		h.render(w, r, pages.Actions(data))
		return
	}

	runs, more, err := h.Services.WorkflowRun.ListRunsPage(ctx, repo.ID, model.RunFilter{
		WorkflowPath: filter.Workflow, Branch: filter.Branch, Status: model.CIStatus(filter.Status), Event: filter.Event,
	}, page, actionsPageSize)
	if err != nil {
		slog.Error("PageActions: list runs failed", "owner", owner, "repo", repoName, "error", err)
		h.ServerError(w, r)
		return
	}
	if page > 1 {
		data.NewerURL = pageURL(page - 1)
	}
	if more {
		data.OlderURL = pageURL(page + 1)
	}
	if facets, err := h.Services.WorkflowRun.RunFacets(ctx, repo.ID); err == nil {
		data.Facets = facets
		data.HasRuns = len(facets.Workflows) > 0
	}
	h.fillRunRows(ctx, repo, runs, &data)
	if canManage {
		_, registered := h.onlineRunnerLabels(ctx, repo.ID)
		data.NoRunner = !registered
	}
	if canWrite && !data.RepoCIOff {
		d := &view.DispatchData{Default: repo.DefaultBranch, Action: actionsBase(repo) + "/dispatch"}
		d.Workflows = h.Services.WorkflowRun.DispatchableWorkflows(ctx, repo)
		d.Branches, d.Tags = h.Services.WorkflowRun.DispatchRefs(repo)
		data.Dispatch = d
	}
	data.Failure = h.parseFailure(repo, runs)
	h.render(w, r, pages.Actions(data))
}

// parseFailure points at a workflow file whose newest Run failed to parse, on the first page only.
func (h *Handler) parseFailure(repo *model.Repository, runs []model.WorkflowRun) *view.ParseFailureBanner {
	seen := map[string]bool{}
	for _, run := range runs {
		if seen[run.WorkflowPath] {
			continue
		}
		seen[run.WorkflowPath] = true
		if run.Error != "" && run.Status == model.CIStatusFailure {
			return &view.ParseFailureBanner{File: path.Base(run.WorkflowPath), URL: actionsBase(repo) + "/runs/" + strconv.Itoa(run.Number),
				SHA: run.SHA, Ref: runRefName(run.Ref)}
		}
	}
	return nil
}

func (h *Handler) fillRunRows(ctx context.Context, repo *model.Repository, runs []model.WorkflowRun, data *view.ActionsData) {
	refs := h.Services.WorkflowRun.LoadRunRefs(ctx, runs)
	shas := make([]string, 0, len(runs))
	for _, r := range runs {
		shas = append(shas, r.SHA)
	}
	subjects := h.Services.Code.CommitSubjects(repo.OwnerName, repo.Name, shas)
	online, _ := h.onlineRunnerLabels(ctx, repo.ID)
	for i := range runs {
		run := &runs[i]
		row := view.RunRow{
			Number: run.Number, URL: actionsBase(repo) + "/runs/" + strconv.Itoa(run.Number), Title: subjects[run.SHA],
			WorkflowName: run.WorkflowName, Status: string(run.Status), Ref: runRefName(run.Ref), SHA: shortSHA(run.SHA),
			SHAURL: codeurl.Path(repo.OwnerName, repo.Name, "commit", run.SHA, ""), Duration: runDuration(run),
			CanApprove: data.CanWrite && run.Status == model.CIStatusWaiting,
		}
		if row.Title == "" {
			row.Title = run.WorkflowName
		}
		row.Trigger, row.PullNumber, row.PullBy = runTrigger(run, refs)
		if row.PullNumber > 0 {
			row.PullURL = "/" + repo.OwnerName + "/" + repo.Name + "/pulls/" + strconv.Itoa(row.PullNumber)
		}
		switch {
		case run.Error != "":
			row.Badge = "Invalid workflow"
			row.Duration = "—"
		case run.Status == model.CIStatusWaiting:
			row.Badge, row.BadgeWarning = "Approval required", true
		}
		switch run.Status {
		case model.CIStatusWaiting:
			row.When = "Waiting for approval"
		case model.CIStatusRunning:
			row.When = "Running"
		case model.CIStatusQueued:
			row.When = "Queued " + timefmt.Relative(run.CreatedAt)
			if jobs, err := h.Services.WorkflowRun.ListJobs(ctx, run.ID); err == nil {
				for _, j := range jobs {
					if j.Status == model.CIStatusQueued {
						if m := missingLabel(j, online); m != "" {
							row.Note = "No runner with label " + m
							break
						}
					}
				}
			}
		default:
			row.When = timefmt.Relative(runEnd(run))
		}
		data.Rows = append(data.Rows, row)
	}
}

func runEnd(run *model.WorkflowRun) time.Time {
	if run.FinishedAt != nil {
		return *run.FinishedAt
	}
	return run.CreatedAt
}

var parseErrLine = regexp.MustCompile(`^(.*?):(\d+):\d+: (.*)$`)

// PageActionsRun handles GET /{owner}/{repo}/actions/runs/{number}.
func (h *Handler) PageActionsRun(w http.ResponseWriter, r *http.Request) {
	repo, run, _, ok := h.readableRun(r)
	if !ok {
		h.NotFound(w, r)
		return
	}
	ctx := r.Context()
	viewer := viewerOf(r)
	canWrite := viewer != nil && h.Services.Repo.CanWrite(ctx, repo, *viewer)
	canManage := viewer != nil && h.Services.Repo.CanManage(ctx, repo, *viewer)
	base := actionsBase(repo)
	runURL := base + "/runs/" + strconv.Itoa(run.Number)
	refs := h.Services.WorkflowRun.LoadRunRefs(ctx, []model.WorkflowRun{*run})
	subjects := h.Services.Code.CommitSubjects(repo.OwnerName, repo.Name, []string{run.SHA})

	d := view.ActionsRunData{
		BasePage: h.withRepoSubnav(ctx, basePage(r, h.Services), repo, "actions", canManage),
		Owner:    repo.OwnerName, RepoName: repo.Name, Number: run.Number, Title: subjects[run.SHA], WorkflowName: run.WorkflowName,
		WorkflowPath: run.WorkflowPath, Status: string(run.Status), StatusLabel: statusLabel(run.Status), Attempt: run.Attempt,
		Ref: runRefName(run.Ref), SHA: shortSHA(run.SHA), SHAURL: codeurl.Path(repo.OwnerName, repo.Name, "commit", run.SHA, ""),
		CanWrite: canWrite, ListURL: base, ParseError: run.Error != "", Flash: actionsErrorText(r),
		WorkflowURL: codeurl.Path(repo.OwnerName, repo.Name, "blob", run.SHA, run.WorkflowPath),
		CancelURL:   runURL + "/cancel", RerunURL: runURL + "/rerun", ApproveURL: runURL + "/approve",
	}
	if d.Title == "" {
		d.Title = run.WorkflowName
	}
	d.Trigger, d.PullNumber, d.PullBy = runTrigger(run, refs)
	if d.PullNumber > 0 {
		d.PullURL = "/" + repo.OwnerName + "/" + repo.Name + "/pulls/" + strconv.Itoa(d.PullNumber)
	}
	d.CanCancel = canWrite && !run.Status.Finished()
	d.CanApprove = canWrite && run.Status == model.CIStatusWaiting
	d.CanRerun = canWrite && run.Status.Finished()
	d.Timing = runTiming(run)
	if run.Status == model.CIStatusCancelled {
		d.CancelledNote = cancelledNote(run, refs)
	}
	if d.ParseError {
		d.StatusLabel = "Invalid workflow"
		h.fillParseError(&d, run)
	}

	jobs, err := h.Services.WorkflowRun.ListJobs(ctx, run.ID)
	if err != nil {
		slog.Error("PageActionsRun: list jobs failed", "run", run.ID, "error", err)
		h.ServerError(w, r)
		return
	}
	d.RerunFailed = slices.ContainsFunc(jobs, func(j model.WorkflowJob) bool {
		return j.Status == model.CIStatusFailure || j.Status == model.CIStatusCancelled
	})
	d.On = ""
	selected := selectJob(jobs, r.URL.Query().Get("job"))
	d.Stages, d.Jobs = jobGraph(jobs, runURL, selected)
	if selected != nil {
		d.Selected, d.Runner = h.jobDetail(ctx, repo, run, selected, jobs, runURL)
	}
	if arts, err := h.Services.WorkflowLog.ListArtifacts(ctx, run.ID); err == nil {
		jobNames := map[int64]string{}
		for _, j := range jobs {
			jobNames[j.ID] = j.Name
		}
		for _, a := range arts {
			d.Artifacts = append(d.Artifacts, view.ArtifactRow{
				Name: a.Name, Size: humanSize(a.Size), SHA: a.SHA256, SHAShort: a.SHA256[:min(8, len(a.SHA256))],
				Expires: timefmt.Date(a.ExpiresAt), URL: workflowArtifactURL(repo.OwnerName, repo.Name, run.Number, a.ID), Job: jobNames[a.JobID],
			})
		}
	}
	w.Header().Set("Cache-Control", "private, no-store")
	h.render(w, r, pages.ActionsRun(d))
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

func runTiming(run *model.WorkflowRun) string {
	switch run.Status {
	case model.CIStatusWaiting:
		return "waiting for approval since " + timefmt.Relative(run.CreatedAt)
	case model.CIStatusQueued:
		return "queued " + timefmt.Relative(run.CreatedAt)
	case model.CIStatusRunning:
		return "running for " + runDuration(run)
	}
	if run.Error != "" {
		return timefmt.Relative(run.CreatedAt)
	}
	verb := "finished "
	if run.Status == model.CIStatusCancelled {
		verb = "cancelled "
	}
	return runDuration(run) + " · " + verb + timefmt.Relative(runEnd(run))
}

func cancelledNote(run *model.WorkflowRun, refs service.RunRefs) string {
	who := "the system"
	if run.CancelledBy != nil {
		if n, ok := refs.Users[*run.CancelledBy]; ok {
			who = n
		}
	}
	return "Cancelled by " + who + " after " + runDuration(run)
}

// fillParseError lists the workflow file with each parse error under the line it names.
func (h *Handler) fillParseError(d *view.ActionsRunData, run *model.WorkflowRun) {
	byLine := map[int]string{}
	var first string
	for _, l := range strings.Split(run.Error, "\n") {
		m := parseErrLine.FindStringSubmatch(l)
		if m == nil {
			if first == "" {
				first = l
			}
			continue
		}
		n, _ := strconv.Atoi(m[2])
		if _, dup := byLine[n]; !dup {
			byLine[n] = m[3]
		}
		if first == "" {
			first = path.Base(m[1]) + " line " + m[2] + ": " + m[3]
		}
	}
	d.ErrorSummary = first
	for i, text := range strings.Split(strings.TrimRight(run.WorkflowContent, "\n"), "\n") {
		d.File = append(d.File, view.FileLine{No: i + 1, Text: text, Error: byLine[i+1]})
	}
	d.FileURL = d.WorkflowURL
}

// selectJob picks the Job whose log shows: the one in ?job=, else a running one, else the first failure, else the first.
func selectJob(jobs []model.WorkflowJob, param string) *model.WorkflowJob {
	if len(jobs) == 0 {
		return nil
	}
	if id, err := strconv.ParseInt(param, 10, 64); err == nil {
		for i := range jobs {
			if jobs[i].ID == id {
				return &jobs[i]
			}
		}
	}
	for _, want := range []model.CIStatus{model.CIStatusRunning, model.CIStatusFailure} {
		for i := range jobs {
			if jobs[i].Status == want {
				return &jobs[i]
			}
		}
	}
	return &jobs[0]
}

// jobGraph groups Jobs into stages: a Job sits one stage after the deepest Job it needs.
func jobGraph(jobs []model.WorkflowJob, runURL string, selected *model.WorkflowJob) ([][]view.JobView, []view.JobView) {
	depth := map[string]int{}
	var resolve func(key string, seen map[string]bool) int
	byKey := map[string][]model.WorkflowJob{}
	for _, j := range jobs {
		byKey[j.JobKey] = append(byKey[j.JobKey], j)
	}
	resolve = func(key string, seen map[string]bool) int {
		if d, ok := depth[key]; ok {
			return d
		}
		if seen[key] {
			return 0
		}
		seen[key] = true
		d := 0
		for _, j := range byKey[key] {
			for _, n := range j.Needs {
				d = max(d, resolve(n, seen)+1)
			}
		}
		depth[key] = d
		return d
	}
	var stages [][]view.JobView
	var flat []view.JobView
	for _, j := range jobs {
		st := resolve(j.JobKey, map[string]bool{})
		jv := view.JobView{ID: j.ID, Name: j.Name, Status: string(j.Status), Stage: st, Selected: selected != nil && j.ID == selected.ID,
			URL: runURL + "?job=" + strconv.FormatInt(j.ID, 10) + "#job"}
		if j.StartedAt != nil {
			jv.Duration = spanText(*j.StartedAt, j.FinishedAt)
		}
		for len(stages) <= st {
			stages = append(stages, nil)
		}
		stages[st] = append(stages[st], jv)
		flat = append(flat, jv)
	}
	return stages, flat
}

func (h *Handler) jobDetail(ctx context.Context, repo *model.Repository, run *model.WorkflowRun, job *model.WorkflowJob,
	all []model.WorkflowJob, runURL string) (*view.JobDetail, string) {
	jd := &view.JobDetail{ID: job.ID, Name: job.Name, Status: string(job.Status), StatusLabel: statusLabel(job.Status), Message: job.Message,
		DownloadURL: runURL + "/jobs/" + strconv.FormatInt(job.ID, 10) + "/log"}
	runner := "Not assigned"
	if job.RunnerID != nil {
		if rn, err := h.Services.CIRunner.GetRunner(ctx, *job.RunnerID); err == nil {
			runner = rn.Name
		}
	}
	onRunner := ""
	if runner != "Not assigned" {
		onRunner = " on " + runner
	}
	dur := ""
	if job.StartedAt != nil {
		dur = spanText(*job.StartedAt, job.FinishedAt)
	}
	switch job.Status {
	case model.CIStatusWaiting:
		jd.Subtitle = "Waiting for approval of the run"
		jd.Placeholder, jd.PlaceholderDetail = "Nothing has run.", "A writer has to approve this fork pull request first."
	case model.CIStatusQueued:
		online, _ := h.onlineRunnerLabels(ctx, repo.ID)
		if m := missingLabel(*job, online); m != "" {
			jd.Subtitle, jd.NoRunner = "No runner with label "+m, m
			jd.SettingsURL = "/" + repo.OwnerName + "/" + repo.Name + "/settings#runners"
		} else {
			jd.Subtitle = "Waiting for a runner"
			jd.Placeholder, jd.PlaceholderDetail = "Waiting for a runner.", "The page updates when it starts."
		}
	case model.CIStatusSkipped:
		jd.Subtitle = "Skipped: a job it needs didn't succeed"
		jd.Placeholder, jd.PlaceholderDetail = "This job didn't run.", needsText(job.Needs)
	case model.CIStatusRunning:
		jd.Subtitle = "Running for " + dur + onRunner
		jd.Live = true
	default:
		jd.Subtitle = statusLabel(job.Status) + " after " + dur + onRunner
		if job.Message != "" {
			jd.Subtitle += " · " + job.Message
		}
	}
	if job.Status == model.CIStatusQueued || job.Status == model.CIStatusWaiting || job.Status == model.CIStatusSkipped {
		return jd, runner
	}
	steps, err := h.Services.WorkflowRun.ListSteps(ctx, job.ID)
	if err != nil {
		slog.Warn("job detail: list steps failed", "job", job.ID, "error", err)
		return jd, runner
	}
	page, err := h.Services.WorkflowLog.ReadLog(ctx, job.ID, 0, service.MaxLogLineLimit)
	if err != nil {
		slog.Warn("job detail: read log failed", "job", job.ID, "error", err)
		page = &model.WorkflowLogPage{}
	}
	jd.Steps = stepViews(steps, page.Lines, 0)
	jd.More = page.TotalLines > page.NextOffset
	if page.Truncated {
		jd.TruncatedMsg = "The log hit its size limit and the rest of this job's output was dropped."
	}
	if jd.Live {
		jd.TailURL = tailURL(runURL, job.ID, page.NextOffset)
	}
	_ = all
	_ = run
	return jd, runner
}

func needsText(needs []string) string {
	if len(needs) == 0 {
		return ""
	}
	return "It needs " + strings.Join(needs, ", ") + "."
}

func tailURL(runURL string, jobID int64, offset int) string {
	return fmt.Sprintf("%s/jobs/%d/tail?offset=%d", runURL, jobID, offset)
}

// stepViews puts each line of a log window (starting at line index first) under the Step whose line range holds it.
// Lines in no Step's range, such as the truncation notice, go under a trailing "Job output" group.
func stepViews(steps []model.WorkflowStep, lines []string, first int) []view.StepView {
	out := make([]view.StepView, 0, len(steps)+1)
	claimed := make([]bool, len(lines))
	for _, st := range steps {
		sv := view.StepView{Position: st.Position, Name: st.Name, Status: string(st.Status), Open: st.Status == model.CIStatusFailure || st.Status == model.CIStatusRunning}
		if st.StartedAt != nil {
			sv.Duration = spanText(*st.StartedAt, st.FinishedAt)
			if st.FinishedAt == nil && st.Status != model.CIStatusRunning {
				sv.Duration = ""
			}
			sv.Started = st.StartedAt.Local().Format("15:04:05")
		}
		for i := max(st.FirstLine, first); i < st.LastLine && i-first < len(lines); i++ {
			sv.Lines = append(sv.Lines, logLine(i, lines[i-first]))
			claimed[i-first] = true
		}
		out = append(out, sv)
	}
	var rest view.StepView
	for i, l := range lines {
		if !claimed[i] {
			rest.Lines = append(rest.Lines, logLine(first+i, l))
		}
	}
	if len(rest.Lines) > 0 {
		rest.Position, rest.Name, rest.Status, rest.Open = -1, "Job output", string(model.CIStatusSuccess), true
		out = append(out, rest)
	}
	return out
}

func logLine(index int, text string) view.LogLine {
	ll := view.LogLine{No: index + 1, Spans: view.ParseANSI(text)}
	for _, sp := range ll.Spans {
		if strings.Contains(sp.Class, "text-destructive") {
			ll.Error = true
		}
	}
	return ll
}

// PageActionsJobTail handles GET .../jobs/{jobID}/tail?offset=: the lines past offset, as out-of-band appends
// to their Step, and the next poll. When the Job has ended it asks the page to reload so every status settles.
func (h *Handler) PageActionsJobTail(w http.ResponseWriter, r *http.Request) {
	repo, run, job, _, ok := h.readableJob(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	offset, err := queryInt(r, "offset", 0)
	if err != nil || offset < 0 {
		http.Error(w, "bad offset", http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	fresh, err := h.Services.WorkflowRun.GetJob(ctx, job.ID)
	if err != nil {
		h.ServerError(w, r)
		return
	}
	if fresh.Status != model.CIStatusRunning && fresh.Status != model.CIStatusQueued {
		w.Header().Set("HX-Refresh", "true")
		return
	}
	steps, err := h.Services.WorkflowRun.ListSteps(ctx, job.ID)
	page, err2 := h.Services.WorkflowLog.ReadLog(ctx, job.ID, offset, service.MaxLogLineLimit)
	if err != nil || err2 != nil {
		slog.Warn("tail: read failed", "job", job.ID, "steps_error", err, "log_error", err2)
		h.ServerError(w, r)
		return
	}
	runURL := actionsBase(repo) + "/runs/" + strconv.Itoa(run.Number)
	all := stepViews(steps, page.Lines, offset)
	data := view.TailData{Job: job.ID, NextURL: tailURL(runURL, job.ID, page.NextOffset)}
	for _, sv := range all {
		// Lines outside every Step have no container to append to; the reload at the Job's end shows them.
		if sv.Position >= 0 {
			data.Steps = append(data.Steps, sv)
		}
	}
	h.render(w, r, pages.ActionsTail(data))
}
