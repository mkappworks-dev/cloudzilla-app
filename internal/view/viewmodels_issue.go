package view

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// IssuesData holds template data for the issue list page.
type IssuesData struct {
	BasePage
	Repo            model.Repository
	Issues          []model.Issue
	PinnedIssues    []model.Issue
	Owner           string
	RepoName        string
	IssueLabels     map[int64][]model.Label
	AllMilestones   []model.Milestone
	ActiveMilestone *model.Milestone

	StateFilter     string
	SearchQuery     string
	LabelFilter     string
	MilestoneFilter string
	Sort            string

	Labels []model.Label

	OpenCount   int
	ClosedCount int
}

// IssueDetailData holds template data for the issue detail page.
type IssueDetailData struct {
	BasePage
	Repo          model.Repository
	Issue         model.Issue
	Comments      []RenderedComment
	Owner         string
	RepoName      string
	BodyHTML      string
	Labels        []model.Label
	Assignees     []model.User
	AllLabels     []model.Label
	Milestone     *model.Milestone
	AllMilestones []model.Milestone
	LinkedPRs     []model.PullRequest
	Collaborators []string
	CanWrite      bool
	CanManage     bool
}

type IssueNewData struct {
	BasePage
	Repo      model.Repository
	Owner     string
	RepoName  string
	Templates []service.IssueTemplate
	// Selected is the pre-filled body when a specific template was chosen.
	Selected string
	// ShowForm is true when the form should be shown (vs. the template chooser).
	ShowForm bool
	CanWrite bool
	Error    string
}

// Fragment data structs
// IssueDetailFragData holds template data for the issue detail HTMX fragment.
type IssueDetailFragData struct {
	Issue    model.Issue
	Owner    string
	Repo     string
	BodyHTML string
}

// Label sidebar fragments
// IssueLabelSidebarData holds template data for the issue label sidebar HTMX fragment.
type IssueLabelSidebarData struct {
	Owner       string
	RepoName    string
	IssueNumber int
	Labels      []model.Label
	AllLabels   []model.Label
	CanWrite    bool
}

// Assignee sidebar fragments
// IssueAssigneeSidebarData holds template data for the issue assignee sidebar HTMX fragment.
type IssueAssigneeSidebarData struct {
	Owner         string
	RepoName      string
	IssueNumber   int
	Assignees     []model.User
	Collaborators []string
	CanWrite      bool
}

// Milestones page
// MilestonesData holds template data for the milestones list page.
type MilestonesData struct {
	BasePage
	Repo             model.Repository
	Owner            string
	RepoName         string
	OpenMilestones   []model.Milestone
	ClosedMilestones []model.Milestone
	CanWrite         bool
}

// Milestones list fragment (HTMX swap)
// MilestonesListFragData holds template data for the milestones list HTMX fragment.
type MilestonesListFragData struct {
	Owner            string
	RepoName         string
	OpenMilestones   []model.Milestone
	ClosedMilestones []model.Milestone
	CanWrite         bool
}

// Milestone sidebar fragment for issue/PR detail pages
// MilestoneSidebarFragData holds template data for the milestone sidebar HTMX fragment.
type MilestoneSidebarFragData struct {
	Owner         string
	RepoName      string
	ItemNumber    int
	IsPull        bool
	Current       *model.Milestone
	AllMilestones []model.Milestone
	CanWrite      bool
}
