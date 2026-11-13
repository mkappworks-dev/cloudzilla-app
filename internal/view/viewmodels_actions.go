package view

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// Statuses the Actions pages draw. A commit status of an external CI adds "error" and "pending" on top.
const (
	SourceWorkflows = "workflows"
	SourceStatuses  = "statuses"
)

// RunRow is one Run in the list.
type RunRow struct {
	Number       int
	URL          string
	Title        string
	WorkflowName string
	Status       string
	// Badge is a pill next to the title: "Approval required" or "Invalid workflow".
	Badge        string
	BadgeWarning bool
	// Note is a warning under the title, such as the label no runner has.
	Note string
	// Trigger is "Push by daisy"; PullNumber and PullURL replace its first words when the Run came from a pull request.
	Trigger    string
	PullNumber int
	PullURL    string
	PullBy     string
	Ref        string
	SHA        string
	SHAURL     string
	Duration   string
	When       string
	CanApprove bool
}

// ActionsFilter is the list's current filter values.
type ActionsFilter struct {
	Source   string
	Workflow string
	Branch   string
	Status   string
	Event    string
}

func (f ActionsFilter) Active() bool {
	return f.Workflow != "" || f.Branch != "" || f.Status != "" || f.Event != ""
}

// DispatchData is the Run workflow popover.
type DispatchData struct {
	Workflows []service.DispatchWorkflow
	Branches  []string
	Tags      []string
	Default   string
	Action    string
}

// ParseFailureBanner warns that a workflow file did not parse on its latest Run.
type ParseFailureBanner struct {
	File string
	URL  string
	SHA  string
	Ref  string
}

// ActionsData is the view model for GET /{owner}/{repo}/actions.
type ActionsData struct {
	BasePage
	// Flash is the sentence for a refused control, from a fixed set keyed by ?err=.
	Flash           string
	Owner, RepoName string
	Filter          ActionsFilter
	Facets          model.RunFacets
	Rows            []RunRow
	Commits         []service.CommitChecks
	NewerURL        string
	OlderURL        string
	StatusesURL     string
	StatusAPIURL    string
	// HasRuns is true once the repo has any Run, so an empty page with filters means "no match".
	HasRuns bool
	// Legacy is the /checks page of an Instance with CI off: statuses only, no filters or controls.
	Legacy bool
	// RepoCIOff says the repo's CI switch is off.
	RepoCIOff bool
	CanWrite  bool
	CanManage bool
	// NoRunner is true when no runner can serve the repo; only a manager sees the hint.
	NoRunner bool
	Dispatch *DispatchData
	Failure  *ParseFailureBanner
}

// JobView is one Job in the graph, the nav and the selected-job header.
type JobView struct {
	ID       int64
	Name     string
	Status   string
	Duration string
	URL      string
	Selected bool
	Stage    int
}

// LogLine is one numbered line of a Job's log.
type LogLine struct {
	No    int
	Spans []LogSpan
	Error bool
}

// StepView is a Step with the log lines in its range.
type StepView struct {
	Position int
	Name     string
	Status   string
	Duration string
	Started  string
	Open     bool
	Lines    []LogLine
}

// ArtifactRow is one downloadable Artifact.
type ArtifactRow struct {
	Name, Size, SHA, SHAShort, Expires, URL, Job string
}

// FileLine is a line of the workflow file, with the parse error that points at it.
type FileLine struct {
	No    int
	Text  string
	Error string
}

// ActionsRunData is the view model for GET /{owner}/{repo}/actions/runs/{n}.
type ActionsRunData struct {
	BasePage
	Flash           string
	Owner, RepoName string

	Number       int
	Title        string
	WorkflowName string
	WorkflowPath string
	WorkflowURL  string
	Status       string
	StatusLabel  string
	Trigger      string
	PullNumber   int
	PullURL      string
	PullBy       string
	Ref          string
	SHA          string
	SHAURL       string
	Timing       string
	Attempt      int
	On           string

	CanWrite  bool
	ListURL   string
	CancelURL string
	RerunURL  string
	// RerunFailed is the default re-run: only Jobs that failed or were cancelled.
	RerunFailed bool
	ApproveURL  string
	CanCancel   bool
	CanRerun    bool
	CanApprove  bool

	CancelledNote string
	ParseError    bool
	ErrorSummary  string
	File          []FileLine
	FileURL       string

	Stages    [][]JobView
	Jobs      []JobView
	Selected  *JobDetail
	Runner    string
	Artifacts []ArtifactRow
}

// JobDetail is the selected Job: its header, the reason it has no log, or its steps with their lines.
type JobDetail struct {
	ID          int64
	Name        string
	Status      string
	StatusLabel string
	Subtitle    string
	Live        bool
	Message     string
	// Placeholder is the text shown instead of a log: why nothing ran, or that it is waiting.
	Placeholder       string
	PlaceholderDetail string
	// NoRunner names the label no online runner has, when that is why the Job is queued.
	NoRunner     string
	SettingsURL  string
	Steps        []StepView
	DownloadURL  string
	TailURL      string
	TruncatedMsg string
	// More is set when the log has lines past what this page renders.
	More bool
}

// TailData is one poll's answer: every Step with its status and the lines past the last offset, and the next poll.
type TailData struct {
	Job     int64
	NextURL string
	Steps   []StepView
}
