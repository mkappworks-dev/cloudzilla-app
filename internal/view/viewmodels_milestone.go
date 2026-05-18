package view

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

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

// MilestoneNewData holds template data for the dedicated new-milestone page.
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

// MilestoneSidebarFragData holds template data for the milestone sidebar HTMX
// fragment shown on issue and pull request detail pages.
type MilestoneSidebarFragData struct {
	Owner         string
	RepoName      string
	ItemNumber    int
	IsPull        bool
	Current       *model.Milestone
	AllMilestones []model.Milestone
	CanWrite      bool
}
