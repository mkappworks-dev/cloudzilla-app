package view

import "time"

// RunnerRow is one runner in a runners list.
type RunnerRow struct {
	ID          int64
	Name        string
	Labels      []string
	Version     string
	Status      string // online, offline or revoked
	LastContact *time.Time
	// CurrentJob is empty when the runner is idle; CurrentJobURL is the Run's page.
	CurrentJob    string
	CurrentJobURL string
}

// RunnersFragData is the runners card, for the admin page and a repo's settings.
type RunnersFragData struct {
	Rows []RunnerRow
	// Base is the path the card's forms post to: /admin/runners or /{owner}/{repo}/settings/runners.
	Base        string
	Description string
	// Fleet adds the version and current Job columns, which the repo view leaves out.
	Fleet bool
}

// RunnerTokenFragData is the one-time registration token with the command that uses it.
type RunnerTokenFragData struct {
	Token   string
	Command string
	// Instance says the runner will serve every repository.
	Instance bool
}

// CILimit is one read-only ci.* setting.
type CILimit struct {
	Key   string
	Value string
}

// AdminRunnersData is the view model for GET /admin/runners.
type AdminRunnersData struct {
	BasePage
	Runners RunnersFragData
	Limits  []CILimit
	// ServerURL is shown in the register command.
	ServerURL string
}

// RunnerDialogData is what the "New runner" dialog needs.
type RunnerDialogData struct {
	Base     string
	Title    string
	Instance bool
}
