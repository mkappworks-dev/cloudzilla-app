package view

import (
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// AdminUsersData is the view model for GET /admin/users.
type AdminUsersData struct {
	BasePage
	Users      []model.AdminUserRow
	Filter     model.AdminUserFilter
	TotalCount int
	Page       int
	PerPage    int
}

// AdminUserData is the view model for GET /admin/users/{username}.
type AdminUserData struct {
	BasePage
	Detail service.AdminUserDetail
	// Self is set when the admin is looking at their own account, which offers no actions.
	Self    bool
	Confirm components.ConfirmFactors
}

// AdminResetLinkFragData is the response to POST
// /api/admin/users/{username}/password-reset-link.
type AdminResetLinkFragData struct {
	Username  string
	Link      string
	ExpiresAt time.Time
	// Notified is set when SMTP is on, so the user was emailed a notice.
	Notified bool
}
