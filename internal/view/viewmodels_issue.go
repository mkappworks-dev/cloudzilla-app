package view

import (
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
)

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
}

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
	CanWrite      bool
	CanManage     bool
}

// IssueNewData is used by the new-issue page (template chooser + form).
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
type IssueDetailFragData struct {
	Issue    model.Issue
	Owner    string
	Repo     string
	BodyHTML string
}

// Label sidebar fragments
type IssueLabelSidebarData struct {
	Owner       string
	RepoName    string
	IssueNumber int
	Labels      []model.Label
	AllLabels   []model.Label
	CanWrite    bool
}

// Assignee sidebar fragments
type IssueAssigneeSidebarData struct {
	Owner       string
	RepoName    string
	IssueNumber int
	Assignees   []model.User
	CanWrite    bool
}

// Milestones page
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
type MilestonesListFragData struct {
	Owner            string
	RepoName         string
	OpenMilestones   []model.Milestone
	ClosedMilestones []model.Milestone
	CanWrite         bool
}

// Milestone sidebar fragment for issue/PR detail pages
type MilestoneSidebarFragData struct {
	Owner         string
	RepoName      string
	ItemNumber    int
	IsPull        bool
	Current       *model.Milestone
	AllMilestones []model.Milestone
	CanWrite      bool
}
