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
