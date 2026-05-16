package view

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// PullsData holds template data for the pull request list page.
type PullsData struct {
	BasePage
	Repo        model.Repository
	Owner       string
	RepoName    string
	StateFilter string
	OpenCount   int
	DraftCount  int
	MergedCount int
	ClosedCount int
	Rows        []components.PRListRowData
}

// PullDetailData holds template data for the pull request detail page.
type PullDetailData struct {
	BasePage
	Repo              model.Repository
	Pull              model.PullRequest
	Owner             string
	RepoName          string
	AuthorUsername    string // resolved by handler; empty -> chrome falls back to Pull.AuthorName
	Diff              *service.PRDiffResult
	BodyHTML          string
	Labels            []model.Label
	Assignees         []model.User
	AllLabels         []model.Label
	Milestone         *model.Milestone
	AllMilestones     []model.Milestone
	CanWrite          bool
	HeadStatuses      []model.CommitStatus
	Reviews           []model.PullReview
	CanMerge          bool
	MergeBlockReason  string
	AutoMergeEnabled  bool
	AutoMergeStrategy string
	// LineComments keyed by "path:line" (e.g. "src/main.go:42")
	LineComments map[string][]RenderedLineComment
	Mergeability components.MergeabilityBoxData
}

// PullCommitsData holds template data for the PR commits sub-view.
type PullCommitsData struct {
	BasePage
	OwnerName      string
	Repo           *model.Repository
	Pull           *model.PullRequest
	AuthorUsername string
	Commits        []service.CommitSummary
}

type PullNewData struct {
	BasePage
	Repo         model.Repository
	Owner        string
	RepoName     string
	TemplateBody string
	Branches     []service.BranchInfo
	Base         string
	Head         string
	AllLabels    []model.Label
	Reviewer     components.ReviewerPickerData
	Error        string
}

// PRReviewsFragData holds template data for the PR reviews HTMX fragment.
type PRReviewsFragData struct {
	Owner            string
	RepoName         string
	PullNumber       int
	Reviews          []model.PullReview
	CanWrite         bool
	PullOpen         bool
	CanMerge         bool
	MergeBlockReason string
}

// PullDetailFragData holds template data for the pull request detail HTMX fragment.
type PullDetailFragData struct {
	Pull              model.PullRequest
	Owner             string
	Repo              string
	BodyHTML          string
	CanWrite          bool
	AutoMergeEnabled  bool
	AutoMergeStrategy string
}

// PullLabelSidebarData holds template data for the PR label sidebar HTMX fragment.
type PullLabelSidebarData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Labels     []model.Label
	AllLabels  []model.Label
	CanWrite   bool
}

// PullAssigneeSidebarData holds template data for the PR assignee sidebar HTMX fragment.
type PullAssigneeSidebarData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Assignees  []model.User
	CanWrite   bool
}

// Line comment fragments
// LineCommentsFragData holds template data for the inline line comments HTMX fragment.
type LineCommentsFragData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Path       string
	Line       int
	Comments   []RenderedLineComment
	CanWrite   bool
}

// LineCommentFormFragData holds template data for the inline comment form HTMX fragment.
type LineCommentFormFragData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Path       string
	Line       int
}
