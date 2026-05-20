package view

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

type MilestonesData struct {
	BasePage
	Repo             model.Repository
	Owner            string
	RepoName         string
	OpenMilestones   []model.Milestone
	ClosedMilestones []model.Milestone
	CanWrite         bool
}

type MilestoneNewData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	CanWrite bool
	// Error and the field values below are populated when a submission fails
	// validation so the form re-renders with the user's input intact.
	Error       string
	Title       string
	Description string
	DueDate     string
}

type MilestoneSidebarFragData struct {
	Owner         string
	RepoName      string
	ItemNumber    int
	IsPull        bool
	Current       *model.Milestone
	AllMilestones []model.Milestone
	CanWrite      bool
}

// MilestoneDetailData backs the milestone detail page. The Tab/State/Page
// fields come from the query string and select which slice of items renders.
type MilestoneDetailData struct {
	BasePage
	Repo      model.Repository
	Owner     string
	RepoName  string
	Milestone model.Milestone
	CanWrite  bool

	Tab   string // "issues" or "pulls"
	State string // "open" or "closed"
	Page  int

	Issues []model.Issue       // populated when Tab == "issues"
	Pulls  []model.PullRequest // populated when Tab == "pulls"

	IssueOpenCount   int
	IssueClosedCount int
	PullOpenCount    int
	PullClosedCount  int

	TotalCount int // items matching the current Tab+State
	TotalPages int
	PerPage    int

	DescriptionHTML string
}

type MilestoneBodyCardData struct {
	Owner           string
	RepoName        string
	Number          int
	Description     string
	DescriptionHTML string
	CanWrite        bool
	Editing         bool
}
