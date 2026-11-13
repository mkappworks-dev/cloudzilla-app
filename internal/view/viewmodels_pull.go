package view

import (
	"strings"

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
	OpenURL     string
	DraftURL    string
	MergedURL   string
	ClosedURL   string
	Filter      components.ListFilterData
	ClearURL    string

	Page       int
	TotalPages int
	PrevURL    string
	NextURL    string

	RepoEmpty   bool // no item in any state: the toolbar is hidden
	OpenCount   int
	DraftCount  int
	MergedCount int
	ClosedCount int
	Rows        []components.PRListRowData
}

// OtherRepo is "owner/repo" when the item is in a repo other than the page's
// own, where an item from a query that doesn't name its repo is assumed to be.
func OtherRepo(pageOwner, pageRepo, owner, repo string) string {
	if owner == "" || strings.EqualFold(owner, pageOwner) && strings.EqualFold(repo, pageRepo) {
		return ""
	}
	return owner + "/" + repo
}

type LinkedIssue struct {
	Number    int
	Title     string
	State     string
	OtherRepo string
}

type LinkedIssuesSidebarData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Linked     []LinkedIssue
	AllIssues  []LinkedIssue
	CanWrite   bool
}

type PullChromeCounts struct {
	ConvCount    int
	CommitsCount int
	ChecksTotal  int
	ChecksPassed int
	FilesCount   int
	Added        int
	Deleted      int
	// MergedBy and MergedCommits describe a merged PR as it merged: afterwards its commit range is empty, and the
	// author may not be who merged it. MergedCommits is 0 for a PR merged before the count was recorded.
	MergedBy      string
	MergedCommits int
	// HeadCI is the combined state of the head commit, shown beside the title.
	HeadCI components.CIBadge
}

// PullDetailData holds template data for the pull request detail page.
type PullDetailData struct {
	BasePage
	PullChromeCounts
	Repo               model.Repository
	Pull               model.PullRequest
	Owner              string
	RepoName           string
	AuthorUsername     string // resolved by handler; empty -> chrome falls back to Pull.AuthorName
	BodyHTML           string
	Labels             []model.Label
	Assignees          []model.User
	AllLabels          []model.Label
	Milestone          *model.Milestone
	AllMilestones      []model.Milestone
	CanWrite           bool
	Collaborators      []string
	HeadStatuses       []model.CommitStatus
	Reviews            []model.PullReview
	Comments           []RenderedComment
	Participants       []string
	LinkedIssues       []LinkedIssue
	LinkableIssues     []LinkedIssue
	ThreadSubscription ThreadSubscriptionData
	Events             []model.PullEvent
	CanMerge           bool
	MergeBlockReason   string
	AutoMergeEnabled   bool
	AutoMergeStrategy  string
	Mergeability       components.MergeabilityBoxData
}

type PullCommitsData struct {
	BasePage
	PullChromeCounts
	OwnerName      string
	Repo           *model.Repository
	Pull           *model.PullRequest
	AuthorUsername string
	Commits        []service.CommitSummary
	CanWrite       bool
	LoadError      bool
	// Unavailable marks a merged PR whose commits were never recorded.
	Unavailable bool
}

// CheckRun is one workflow Run on a pull request's head commit with its Jobs.
type CheckRun struct {
	Title      string
	Status     string
	URL        string
	Meta       string
	Waiting    bool
	CanApprove bool
	Jobs       []components.CheckRow
}

type PullChecksData struct {
	BasePage
	PullChromeCounts
	OwnerName      string
	Repo           *model.Repository
	Pull           *model.PullRequest
	AuthorUsername string
	Runs           []CheckRun
	// Rows are the statuses no Run owns, posted by external CI.
	Rows []components.CheckRow
	// States are the states of the commit's statuses, so the banner counts what the tab badge counts even when two Runs
	// share a Job context.
	States    []string
	HeadSHA   string
	CanWrite  bool
	LoadError bool
}

type PullFilesData struct {
	BasePage
	PullChromeCounts
	OwnerName      string
	Repo           *model.Repository
	Pull           *model.PullRequest
	AuthorUsername string
	Tree           []components.DiffFileTreeItem
	Diff           *service.PRDiffResult
	CanWrite       bool
	LineComments   map[LineCommentKey][]RenderedLineComment
	LoadError      bool
	// Unavailable marks a merged PR whose diff was never recorded.
	Unavailable bool
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
	CanWrite     bool
	AllLabels    []model.Label
	Reviewer     components.ReviewerPickerData
	Error        string

	// HeadRepo is the "owner/name" the head branch is taken from, empty for the repository itself. HeadRepoChoices
	// are the other repositories of the fork network the viewer can write to; HeadBranches are HeadRepo's branches.
	HeadRepo        string
	HeadRepoChoices []string
	HeadBranches    []service.BranchInfo
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
	Owner         string
	RepoName      string
	PullNumber    int
	Assignees     []model.User
	Collaborators []string
	CanWrite      bool
}

// LineCommentKey identifies a thread of line comments. Line is a base-file line
// number when Side is "left" and a head-file one when it is "right", so the same
// Path and Line on the two sides can name different lines.
type LineCommentKey struct {
	Side string
	Path string
	Line int
}

func LineCommentKeyOf(c model.PullLineComment) LineCommentKey {
	return LineCommentKey{Side: c.DiffSide, Path: c.Path, Line: c.Line}
}

// LineCommentsFragData holds template data for the inline line comments HTMX fragment.
type LineCommentsFragData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Key        LineCommentKey
	Comments   []RenderedLineComment
	CanWrite   bool
	// PullOpen gates Apply suggestion: a suggestion can only be committed to an open PR's head branch.
	PullOpen bool
}

// LineCommentFormFragData holds template data for the inline comment form HTMX fragment.
type LineCommentFormFragData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Path       string
	Line       int
}

// PullHeadLabel is the head as a reader names it: the branch for a same-repository PR, owner/name:branch for a fork's,
// and "a fork" when the viewer may not know which.
func PullHeadLabel(p *model.PullRequest) string {
	switch {
	case p.HeadRepoHidden:
		return "a fork"
	case p.CrossRepo && p.HeadRepo != "":
		return p.HeadRepo + ":" + p.HeadBranch
	default:
		return p.HeadBranch
	}
}
