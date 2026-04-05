package view

import (
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
)

type PullsData struct {
	BasePage
	Repo            model.Repository
	Pulls           []model.PullRequest
	Owner           string
	RepoName        string
	PullLabels      map[int64][]model.Label
	AllMilestones   []model.Milestone
	ActiveMilestone *model.Milestone
	StateFilter     string
}

type PullDetailData struct {
	BasePage
	Repo              model.Repository
	Pull              model.PullRequest
	Owner             string
	RepoName          string
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
}

// PullNewData is used by the new-pull-request page.
type PullNewData struct {
	BasePage
	Repo         model.Repository
	Owner        string
	RepoName     string
	TemplateBody string
	Branches     []service.BranchInfo
	Error        string
}

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

type PullDetailFragData struct {
	Pull              model.PullRequest
	Owner             string
	Repo              string
	BodyHTML          string
	CanWrite          bool
	AutoMergeEnabled  bool
	AutoMergeStrategy string
}

type PullLabelSidebarData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Labels     []model.Label
	AllLabels  []model.Label
	CanWrite   bool
}

type PullAssigneeSidebarData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Assignees  []model.User
	CanWrite   bool
}

// Line comment fragments
type LineCommentsFragData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Path       string
	Line       int
	Comments   []RenderedLineComment
	CanWrite   bool
}

type LineCommentFormFragData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Path       string
	Line       int
}
