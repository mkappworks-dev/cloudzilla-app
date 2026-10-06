package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	// ErrAdminSelf refuses an admin action on the acting admin's own account,
	// which Settings handles with the account's own confirmation.
	ErrAdminSelf             = errors.New("use your own account settings for this")
	ErrUserSuspended         = store.ErrUserSuspended
	ErrDeleteConfirmMismatch = errors.New("the confirmation doesn't match the username")
)

// AdminUserService is what a superadmin can see of and do to other accounts.
// Callers have already checked that the actor is a superadmin and confirmed
// the action with their password.
type AdminUserService struct {
	users   *store.UserStore
	user    *UserService
	audit   *AuditService
	notices *EmailService
	resets  *PasswordResetService
}

func NewAdminUserService(users *store.UserStore, user *UserService, audit *AuditService) *AdminUserService {
	return &AdminUserService{users: users, user: user, audit: audit}
}

// Without it, 2FA resets and credential revocations mail no notice.
func (s *AdminUserService) WithSecurityNotices(e *EmailService) *AdminUserService {
	s.notices = e
	return s
}

func (s *AdminUserService) WithPasswordResets(r *PasswordResetService) *AdminUserService {
	s.resets = r
	return s
}

// AdminUserDetail is one account as /admin/users/{username} shows it.
type AdminUserDetail struct {
	model.AdminUserRow
	SoleOwnedOrgs []string
	// LastSignIn is the latest web sign-in, nil if none was recorded.
	LastSignIn *time.Time
}

func (s *AdminUserService) List(ctx context.Context, f model.AdminUserFilter, page, perPage int) ([]model.AdminUserRow, int, error) {
	return s.users.ListForAdmin(ctx, f, page, perPage)
}

// Get returns an error wrapping sql.ErrNoRows for an unknown username or the ghost.
func (s *AdminUserService) Get(ctx context.Context, username string) (*AdminUserDetail, error) {
	row, err := s.users.GetForAdmin(ctx, username)
	if err != nil {
		return nil, err
	}
	d := &AdminUserDetail{AdminUserRow: *row}
	if d.SoleOwnedOrgs, err = s.users.SoleOwnedOrgNames(ctx, row.User.ID); err != nil {
		return nil, err
	}
	id := row.User.ID
	entries, _, err := s.audit.List(ctx, model.AuditFilter{ActorID: &id, Action: model.AuditActionLogin}, 1, 1)
	if err != nil {
		return nil, fmt.Errorf("admin user last sign-in: %w", err)
	}
	if len(entries) > 0 {
		d.LastSignIn = &entries[0].CreatedAt
	}
	return d, nil
}

// target loads username for an action by actorID, refusing the actor's own account.
func (s *AdminUserService) target(ctx context.Context, actorID int64, username string) (*model.User, error) {
	u, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	if u.ID == actorID {
		return nil, ErrAdminSelf
	}
	return u, nil
}

// Suspend is idempotent. It returns ErrLastSuperadmin rather than leave the
// instance with no active superadmin.
func (s *AdminUserService) Suspend(ctx context.Context, actorID int64, username string) (*model.User, error) {
	u, err := s.target(ctx, actorID, username)
	if err != nil {
		return nil, err
	}
	if _, err := s.users.Suspend(ctx, u.ID); err != nil {
		return nil, err
	}
	return u, nil
}

// Unsuspend lets the account's tokens and keys work again; its sessions stay ended.
func (s *AdminUserService) Unsuspend(ctx context.Context, actorID int64, username string) (*model.User, error) {
	u, err := s.target(ctx, actorID, username)
	if err != nil {
		return nil, err
	}
	if _, err := s.users.Unsuspend(ctx, u.ID); err != nil {
		return nil, err
	}
	return u, nil
}

// Promote returns ErrUserSuspended for a suspended account.
func (s *AdminUserService) Promote(ctx context.Context, actorID int64, username string) (*model.User, error) {
	u, err := s.target(ctx, actorID, username)
	if err != nil {
		return nil, err
	}
	if err := s.users.Promote(ctx, u.ID); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *AdminUserService) Demote(ctx context.Context, actorID int64, username string) (*model.User, error) {
	u, err := s.target(ctx, actorID, username)
	if err != nil {
		return nil, err
	}
	if err := s.users.Demote(ctx, u.ID); err != nil {
		return nil, err
	}
	return u, nil
}

// ResetTOTP turns two-factor sign-in off for someone who lost their
// authenticator and backup codes, and tells them.
func (s *AdminUserService) ResetTOTP(ctx context.Context, actorID int64, username string) (*model.User, error) {
	u, err := s.target(ctx, actorID, username)
	if err != nil {
		return nil, err
	}
	if err := s.resetTOTP(ctx, u); err != nil {
		return nil, err
	}
	notifySecurityChange(s.notices, s.users, u.ID, "admin_totp_reset", adminTOTPResetNotice)
	return u, nil
}

// ResetTOTPOffline is ResetTOTP for cloudzilla-cli, which has no acting admin; the caller mails SendTOTPResetNotice.
func (s *AdminUserService) ResetTOTPOffline(ctx context.Context, username string) (*model.User, bool, error) {
	u, err := s.users.GetByUsernameWithTOTP(ctx, username)
	if err != nil {
		return nil, false, err
	}
	if !u.TOTPEnabled {
		return u, false, nil
	}
	if err := s.resetTOTP(ctx, u); err != nil {
		return nil, false, err
	}
	return u, true, nil
}

// SendTOTPResetNotice mails ResetTOTP's notice and waits for the send.
func (s *AdminUserService) SendTOTPResetNotice(ctx context.Context, userID int64) error {
	return sendSecurityNotice(ctx, s.notices, s.users, userID, adminTOTPResetNotice)
}

func (s *AdminUserService) resetTOTP(ctx context.Context, u *model.User) error {
	if err := s.users.SetTOTPEnabled(ctx, u.ID, false, ""); err != nil {
		return fmt.Errorf("admin reset totp: %w", err)
	}
	return nil
}

// RevokeCredentials deletes the account's tokens, SSH keys and app grants and
// ends its sessions, and tells the user.
func (s *AdminUserService) RevokeCredentials(ctx context.Context, actorID int64, username string) (*model.User, model.RevokedCredentials, error) {
	u, err := s.target(ctx, actorID, username)
	if err != nil {
		return nil, model.RevokedCredentials{}, err
	}
	revoked, err := s.users.RevokeCredentials(ctx, u.ID)
	if err != nil {
		return nil, model.RevokedCredentials{}, err
	}
	notifySecurityChange(s.notices, s.users, u.ID, "admin_credentials_revoke", adminCredentialsRevokedNotice)
	return u, revoked, nil
}

// IssuePasswordResetLink returns a link for the admin to hand over. The caller
// sends NotifyPasswordResetLink once the link is recorded.
func (s *AdminUserService) IssuePasswordResetLink(ctx context.Context, actorID int64, username string) (*model.User, string, error) {
	u, err := s.target(ctx, actorID, username)
	if err != nil {
		return nil, "", err
	}
	link, err := s.resets.IssueLink(ctx, u.ID, model.PasswordResetByAdmin)
	if err != nil {
		return nil, "", err
	}
	return u, link, nil
}

// NotifyPasswordResetLink tells the user a link was issued, without the link.
func (s *AdminUserService) NotifyPasswordResetLink(userID int64) {
	notifySecurityChange(s.notices, s.users, userID, "admin_password_reset_link", adminPasswordResetLinkNotice)
}

// Delete removes the account as self-service deletion would. confirm must be
// the username typed out.
func (s *AdminUserService) Delete(ctx context.Context, actorID int64, username, confirm string) (*model.User, error) {
	if strings.TrimSpace(confirm) != username {
		return nil, ErrDeleteConfirmMismatch
	}
	u, err := s.target(ctx, actorID, username)
	if err != nil {
		return nil, err
	}
	if err := s.user.DeleteUser(ctx, u.ID); err != nil {
		return nil, err
	}
	return u, nil
}

func adminTOTPResetNotice(username string) (subject, body string) {
	return "An administrator turned off two-factor authentication for your Cloudzilla account",
		fmt.Sprintf("<p>An administrator turned off two-factor authentication for <strong>@%s</strong>. Signing in now needs only your password or sign-in provider.</p>"+
			"<p>Turn it back on under Account settings → Security. If you didn't ask for this, tell your administrator.</p>", html.EscapeString(username))
}

func adminCredentialsRevokedNotice(username string) (subject, body string) {
	return "An administrator revoked your Cloudzilla tokens and keys",
		fmt.Sprintf("<p>An administrator deleted the personal access tokens, SSH keys and app authorizations of <strong>@%s</strong>, and signed it out everywhere.</p>"+
			"<p>Sign in again and create new ones as needed.</p>", html.EscapeString(username))
}

func adminPasswordResetLinkNotice(username string) (subject, body string) {
	return "An administrator issued a password reset link for your Cloudzilla account",
		fmt.Sprintf("<p>An administrator issued a link that sets a new password for <strong>@%s</strong>. They will pass it on to you. It works once, within 24 hours, and your password stays the same until it is used.</p>"+
			"<p>If you didn't ask for this, tell your administrator.</p>", html.EscapeString(username))
}
